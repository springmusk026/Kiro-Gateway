package proxy

import (
	"encoding/json"
	accountpool "kiro-go/pool"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIResponsesCarryRequestIDs(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/nope", nil))
	id := w.Header().Get("request-id")
	if id == "" || w.Header().Get("X-Request-Id") != id {
		t.Fatalf("request-id=%q x-request-id=%q", id, w.Header().Get("X-Request-Id"))
	}
}

func TestCORSPreflightEchoesRequestedHeaders(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	r := httptest.NewRequest(http.MethodOptions, "/v1/messages", nil)
	r.Header.Set("Access-Control-Request-Headers", "x-custom-header, anthropic-version")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "x-custom-header, anthropic-version" {
		t.Fatalf("allow headers = %q", got)
	}
	if w.Header().Get("Access-Control-Max-Age") != "86400" {
		t.Fatalf("max-age = %q", w.Header().Get("Access-Control-Max-Age"))
	}
	expose := w.Header().Get("Access-Control-Expose-Headers")
	for _, h := range []string{"request-id", "x-request-id", "retry-after"} {
		if !strings.Contains(expose, h) {
			t.Fatalf("expose headers %q missing %s", expose, h)
		}
	}
}

func TestOversizedBodyIs413(t *testing.T) {
	mustInitConfig(t)
	t.Setenv("KIRO_MAX_BODY_MB", "1")
	h := &Handler{}
	body := `{"model":"x","messages":[{"role":"user","content":"` + strings.Repeat("a", 2<<20) + `"}]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"request_too_large"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestNoAccountsIsOverloaded(t *testing.T) {
	mustInitConfig(t)
	p := accountpool.GetPool()
	p.Reload()
	h := &Handler{pool: p, promptCache: newPromptCacheTracker(defaultPromptCacheTTL)}

	w := httptest.NewRecorder()
	h.handleClaudeMessages(w, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
		`{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)))
	if w.Code != 529 || !strings.Contains(w.Body.String(), `"overloaded_error"`) {
		t.Fatalf("claude: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.handleOpenAIChat(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"claude-sonnet-4.5","messages":[{"role":"user","content":"hi"}]}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("openai: %d %s", w.Code, w.Body.String())
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	for _, header := range []string{"Bearer sk-1", "bearer sk-1", "BEARER  sk-1"} {
		r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		r.Header.Set("Authorization", header)
		if got := extractProvidedKey(r); got != "sk-1" {
			t.Fatalf("%q -> %q", header, got)
		}
	}
}

func TestUnknownAPIPathsReturnJSONErrors(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/unknown", nil))
	var openai struct {
		Error map[string]interface{} `json:"error"`
	}
	if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &openai) != nil || openai.Error["type"] != "invalid_request_error" {
		t.Fatalf("openai 404: %d %s", w.Code, w.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/unknown", nil)
	r.Header.Set("anthropic-version", "2023-06-01")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var anthropic struct {
		Type  string            `json:"type"`
		Error map[string]string `json:"error"`
	}
	if w.Code != 404 || json.Unmarshal(w.Body.Bytes(), &anthropic) != nil || anthropic.Type != "error" || anthropic.Error["type"] != "not_found_error" {
		t.Fatalf("anthropic 404: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/messages", nil))
	if w.Code != http.StatusMethodNotAllowed || !strings.Contains(w.Body.String(), `"type":"error"`) {
		t.Fatalf("405: %d %s", w.Code, w.Body.String())
	}
}
