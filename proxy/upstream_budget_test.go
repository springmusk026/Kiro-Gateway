package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"kiro-go/config"
)

func TestUpstreamBudgetCounts(t *testing.T) {
	ctx := withUpstreamBudget(context.Background(), 2)
	if takeUpstreamAttempt(ctx) != nil || takeUpstreamAttempt(ctx) != nil {
		t.Fatal("first two attempts must pass")
	}
	if !errors.Is(takeUpstreamAttempt(ctx), errUpstreamBudgetExhausted) {
		t.Fatal("third attempt must be refused")
	}
	if takeUpstreamAttempt(context.Background()) != nil {
		t.Fatal("contexts without a budget are unlimited")
	}
}

// A permanently failing upstream must not turn one client request into a
// burst of calls across all retry layers.
func TestFailingUpstreamCallsAreCappedPerRequest(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	for i := 0; i < 3; i++ { // several accounts → account rotation would multiply calls
		config.AddAccount(config.Account{ID: "extra-" + string(rune('a'+i)), Enabled: true, AccessToken: "t", ProfileArn: "arn:aws:codewhisperer:profile/x"})
	}
	h.pool.Reload()
	t.Setenv("KIRO_MAX_UPSTREAM_ATTEMPTS", "4")

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"boom"}`))
	}))
	defer server.Close()
	defer swapKiroEndpointsForTest(t, server)()

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4.5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got := hits.Load(); got == 0 || got > 4 {
		t.Fatalf("upstream calls for one request = %d, want 1..4", got)
	}
}
