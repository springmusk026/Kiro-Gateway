package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
	accountpool "github.com/springmusk026/Kiro-Gateway/pool"
)

// routeRecorder answers every upstream call with a server error (so the
// executor fails over) and records which account token and model each used.
type routeRecorder struct {
	mu     sync.Mutex
	tokens []string
	models []string
}

func (rr *routeRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	var p struct {
		ConversationState struct {
			CurrentMessage struct {
				UserInputMessage struct {
					ModelID string `json:"modelId"`
				} `json:"userInputMessage"`
			} `json:"currentMessage"`
		} `json:"conversationState"`
	}
	_ = json.Unmarshal(body, &p)
	rr.mu.Lock()
	rr.tokens = append(rr.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	rr.models = append(rr.models, p.ConversationState.CurrentMessage.UserInputMessage.ModelID)
	rr.mu.Unlock()
	return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"message":"boom"}`)), Request: r}, nil
}

func TestApiKeyRoutingPinsAccountsAndMapsModels(t *testing.T) {
	mustInitConfig(t)
	noRetryWait(t)
	t.Setenv("KIRO_MAX_UPSTREAM_ATTEMPTS", "8")
	for _, id := range []string{"acc-a", "acc-b", "acc-c"} {
		if err := config.AddAccount(config.Account{ID: id, Enabled: true, AccessToken: "token-" + id,
			ProfileArn: "arn:aws:codewhisperer:us-east-1:000000000000:profile/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []error{config.UpdateUpstreamProtocol("legacy"), config.UpdatePreferredEndpoint("kiro"), config.UpdateEndpointFallback(false)} {
		if step != nil {
			t.Fatal(step)
		}
	}
	t.Cleanup(func() { _ = config.UpdateUpstreamProtocol("") })
	if _, err := config.AddApiKey(config.ApiKeyEntry{Name: "pinned", Key: "sk-pinned", Enabled: true,
		AccountIDs: []string{" acc-c ", "acc-c"}, ModelMap: map[string]string{"Client-Model": "claude-sonnet-4.5"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.AddApiKey(config.ApiKeyEntry{Name: "free", Key: "sk-free", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	requireAuth(t)

	rr := &routeRecorder{}
	old := kiroHttpStore.Load()
	kiroHttpStore.Store(&http.Client{Transport: rr})
	t.Cleanup(func() { kiroHttpStore.Store(old) })
	p := accountpool.GetPool()
	p.Reload()
	h := &Handler{pool: p, promptCache: newPromptCacheTracker(defaultPromptCacheTTL)}

	send := func(key, model string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
			`{"model":"`+model+`","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", key)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	for i := 0; i < 3; i++ {
		send("sk-pinned", "client-model")
	}
	rr.mu.Lock()
	if len(rr.tokens) == 0 {
		t.Fatal("no upstream call")
	}
	for i, tok := range rr.tokens {
		if tok != "token-acc-c" {
			t.Fatalf("pinned key used %s (calls %v)", tok, rr.tokens)
		}
		if rr.models[i] != "claude-sonnet-4.5" {
			t.Fatalf("model sent = %q, want the mapped model", rr.models[i])
		}
	}
	rr.tokens, rr.models = nil, nil
	rr.mu.Unlock()

	// A key without rules fails over across every account and keeps its model.
	send("sk-free", "claude-sonnet-4.5")
	rr.mu.Lock()
	defer rr.mu.Unlock()
	seen := map[string]bool{}
	for _, tok := range rr.tokens {
		seen[tok] = true
	}
	if len(seen) < 2 {
		t.Fatalf("unpinned key should fail over across accounts, used %v", rr.tokens)
	}
}

func TestApiKeyRoutingAdminValidation(t *testing.T) {
	mustInitConfig(t)
	if err := config.AddAccount(config.Account{ID: "acc-a", Enabled: true, AccessToken: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := validateApiKeyRouting(config.ApiKeyEntry{AccountIDs: []string{"acc-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateApiKeyRouting(config.ApiKeyEntry{AccountIDs: []string{"acc-a", "nope"}}); err == nil {
		t.Fatal("unknown account ids must be rejected")
	}
	got := config.NormalizeModelMap(map[string]string{" A ": " b ", "": "x", "c": ""})
	if len(got) != 1 || got["a"] != "b" {
		t.Fatalf("NormalizeModelMap = %v", got)
	}
	if config.MapApiKeyModel(got, "A") != "b" || config.MapApiKeyModel(got, "z") != "z" {
		t.Fatal("MapApiKeyModel")
	}
}
