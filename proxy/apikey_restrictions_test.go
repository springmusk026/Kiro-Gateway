package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func seedRestrictedKey(t *testing.T, e config.ApiKeyEntry) config.ApiKeyEntry {
	t.Helper()
	mustInitConfig(t)
	e.Enabled = true
	created, err := config.AddApiKey(e)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	requireAuth(t)
	return created
}

func restrictedRequest(path, key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+key)
	return r
}

func TestKeyExpiredIs401(t *testing.T) {
	seedRestrictedKey(t, config.ApiKeyEntry{Key: "sk-exp", ExpiresAt: time.Now().Add(-time.Minute).Unix()})
	h := &Handler{}
	w := httptest.NewRecorder()
	if ar := h.authenticateForClaude(w, restrictedRequest("/v1/messages", "sk-exp", `{"model":"claude-sonnet-4.5"}`)); ar != nil {
		t.Fatal("expired key was accepted")
	}
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "API key expired") ||
		!strings.Contains(w.Body.String(), "authentication_error") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestKeyNotYetExpiredPasses(t *testing.T) {
	seedRestrictedKey(t, config.ApiKeyEntry{Key: "sk-future", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	h := &Handler{}
	w := httptest.NewRecorder()
	if ar := h.authenticateForClaude(w, restrictedRequest("/v1/messages", "sk-future", `{}`)); ar == nil {
		t.Fatalf("valid key rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestKeyModelAllowList(t *testing.T) {
	seedRestrictedKey(t, config.ApiKeyEntry{Key: "sk-models", AllowedModels: []string{"Claude-Sonnet-*", "claude-haiku-4.5"}})
	if err := config.UpdateModelMappings([]config.ModelMapping{{Source: "kiro-fast", Target: "claude-haiku-4.5"}, {Source: "kiro-big", Target: "claude-opus-4.5"}}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{}
	cases := []struct {
		model string
		ok    bool
	}{
		{"claude-sonnet-4.5", true},
		{"CLAUDE-SONNET-4", true},
		{"claude-sonnet-4-5-20250929", true}, // resolves to claude-sonnet-4.5
		{"claude-haiku-4.5", true},
		{"kiro-fast", true}, // alias → claude-haiku-4.5
		{"kiro-big", false}, // alias → claude-opus-4.5
		{"claude-opus-4.5", false},
		{"", false},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		body := `{"model":"` + c.model + `","messages":[]}`
		ar := h.authenticateForClaude(w, restrictedRequest("/v1/messages", "sk-models", body))
		if (ar != nil) != c.ok {
			t.Fatalf("model %q: allowed=%v, want %v (%d %s)", c.model, ar != nil, c.ok, w.Code, w.Body.String())
		}
		if c.ok {
			// The handler still sees the whole body.
			got, _ := io.ReadAll(ar.Body)
			if string(got) != body {
				t.Fatalf("body not restored: %q", got)
			}
			continue
		}
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "permission_error") {
			t.Fatalf("model %q: got %d %s", c.model, w.Code, w.Body.String())
		}
	}

	// OpenAI shape: 403 with code model_not_allowed.
	w := httptest.NewRecorder()
	if ar := h.authenticateForOpenAI(w, restrictedRequest("/v1/chat/completions", "sk-models", `{"model":"claude-opus-4.5"}`)); ar != nil {
		t.Fatal("disallowed model accepted")
	}
	var body struct {
		Error struct{ Type, Code string }
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body.Error.Code != "model_not_allowed" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestKeyRPMLimit(t *testing.T) {
	created := seedRestrictedKey(t, config.ApiKeyEntry{Key: "sk-rpm", RPMLimit: 2})
	apiKeyRPM.mu.Lock()
	delete(apiKeyRPM.buckets, created.ID)
	apiKeyRPM.mu.Unlock()
	h := &Handler{}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		if ar := h.authenticateForOpenAI(w, restrictedRequest("/v1/chat/completions", "sk-rpm", `{}`)); ar == nil {
			t.Fatalf("request %d rejected: %s", i, w.Body.String())
		}
	}
	// count_tokens does not count against the limit.
	w := httptest.NewRecorder()
	if ar := h.authenticateForClaude(w, restrictedRequest("/v1/messages/count_tokens", "sk-rpm", `{}`)); ar == nil {
		t.Fatalf("count_tokens rejected: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	if ar := h.authenticateForClaude(w, restrictedRequest("/v1/messages", "sk-rpm", `{}`)); ar != nil {
		t.Fatal("third request within a minute accepted")
	}
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "rate_limit_error") {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if ra := w.Header().Get("Retry-After"); ra == "" || ra == "0" {
		t.Fatalf("missing retry-after: %q", ra)
	}
}

func TestKeyRateLimiterRefills(t *testing.T) {
	l := &keyRateLimiter{buckets: map[string]*keyBucket{}}
	now := time.Unix(1000, 0)
	for i := 0; i < 60; i++ {
		if ok, _ := l.allow("k", 60, now); !ok {
			t.Fatalf("burst request %d rejected", i)
		}
	}
	ok, wait := l.allow("k", 60, now)
	if ok || wait < time.Second || wait > 2*time.Second {
		t.Fatalf("expected rejection with ~1s wait, got %v %v", ok, wait)
	}
	if ok, _ := l.allow("k", 60, now.Add(1100*time.Millisecond)); !ok {
		t.Fatal("bucket did not refill")
	}
	// Changing the limit resets the bucket.
	if ok, _ := l.allow("k", 61, now.Add(1100*time.Millisecond)); !ok {
		t.Fatal("new limit did not start full")
	}
}

func TestModelsListFilteredForKey(t *testing.T) {
	seedRestrictedKey(t, config.ApiKeyEntry{Key: "sk-list", AllowedModels: []string{"claude-sonnet-*"}})
	models := []map[string]interface{}{
		{"id": "claude-sonnet-4.5"}, {"id": "claude-sonnet-4.5-thinking"}, {"id": "claude-opus-4.5"}, {"id": "auto"},
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if got := filterModelsForKey(r, append([]map[string]interface{}{}, models...)); len(got) != 4 {
		t.Fatalf("without a key every model is listed, got %d", len(got))
	}
	r.Header.Set("x-api-key", "sk-list")
	got := filterModelsForKey(r, models)
	var ids []string
	for _, m := range got {
		ids = append(ids, m["id"].(string))
	}
	if strings.Join(ids, ",") != "claude-sonnet-4.5,claude-sonnet-4.5-thinking" {
		t.Fatalf("got %v", ids)
	}
}

func TestAdminApiKeyRestrictionsRoundTrip(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/api/api-keys",
		strings.NewReader(`{"name":"r","allowedModels":[" Claude-Sonnet-* ","claude-sonnet-*",""],"rpmLimit":30,"expiresAt":4102444800}`))
	h.apiCreateApiKey(w, r)
	var created struct {
		ID     string
		ApiKey apiKeyView
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	v := created.ApiKey
	if len(v.AllowedModels) != 1 || v.AllowedModels[0] != "claude-sonnet-*" || v.RPMLimit != 30 || v.ExpiresAt != 4102444800 {
		t.Fatalf("view: %+v", v)
	}
	// Omitted fields stay; explicit empties clear.
	w = httptest.NewRecorder()
	h.apiUpdateApiKey(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"rpmLimit":0}`)), created.ID)
	e := config.GetApiKeyEntry(created.ID)
	if e.RPMLimit != 0 || len(e.AllowedModels) != 1 || e.ExpiresAt == 0 {
		t.Fatalf("after patch: %+v", e)
	}
	w = httptest.NewRecorder()
	h.apiUpdateApiKey(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"allowedModels":[],"expiresAt":0}`)), created.ID)
	e = config.GetApiKeyEntry(created.ID)
	if len(e.AllowedModels) != 0 || e.ExpiresAt != 0 {
		t.Fatalf("after clear: %+v", e)
	}
	w = httptest.NewRecorder()
	h.apiUpdateApiKey(w, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"rpmLimit":-1}`)), created.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative rpm accepted: %d", w.Code)
	}
}
