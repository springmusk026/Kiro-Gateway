package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	accountpool "github.com/springmusk026/Kiro-Gateway/pool"
)

// ---- Model fallback ----

func TestModelFallbackEligibility(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New(`HTTP 503 from test: {"reason":"MODEL_TEMPORARILY_UNAVAILABLE"}`), true},
		{errors.New(`HTTP 503 from Kiro responses: {"error":{"code":"server_is_overloaded"}}`), true},
		{errors.New(`HTTP 529 from test: busy`), true},
		{errors.New(`HTTP 400 from test: {"reason":"INVALID_MODEL_ID"}`), true},
		{errors.New(`HTTP 400 from test: improperly formed request`), false},
		{errors.New(`HTTP 429 from test: Too many requests`), false},
		{errors.New(`HTTP 429 from test: {"reason":"MONTHLY_REQUEST_COUNT"}`), false},
		{errors.New(`HTTP 403 from test: unauthorized`), false},
		{errors.New(`HTTP 500 from test: boom`), false},
		{errUpstreamBudgetExhausted, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := modelFallbackEligible(c.err); got != c.want {
			t.Errorf("modelFallbackEligible(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRemapReasoningFieldsDropsUnsupportedControls(t *testing.T) {
	fields := map[string]interface{}{"output_config": map[string]interface{}{"effort": "high"}}
	if got := remapReasoningFields(fields, "model-without-controls"); got != nil {
		t.Fatalf("expected controls to be dropped for a model that declares none, got %v", got)
	}
}

// fallbackUpstream answers per model: unavailable for the models in down,
// a short answer otherwise. It counts calls per model.
func fallbackUpstream(t *testing.T, down map[string]string) (*httptest.Server, *sync.Map) {
	t.Helper()
	var hits sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p KiroPayload
		_ = json.Unmarshal(body, &p)
		model := p.ConversationState.CurrentMessage.UserInputMessage.ModelID
		n, _ := hits.LoadOrStore(model, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		if errBody, ok := down[model]; ok {
			status := http.StatusServiceUnavailable
			if strings.Contains(errBody, "Too many") {
				status = http.StatusTooManyRequests
			}
			w.WriteHeader(status)
			w.Write([]byte(errBody))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "served by " + model}))
		w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "end_turn"}))
	}))
	return srv, &hits
}

func hitCount(m *sync.Map, model string) int32 {
	if v, ok := m.Load(model); ok {
		return v.(*atomic.Int32).Load()
	}
	return 0
}

func noRetryWait(t *testing.T) {
	old := streamRetryWait
	streamRetryWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { streamRetryWait = old })
}

func postMessages(h *Handler, model string, stream bool) *httptest.ResponseRecorder {
	body := `{"model":"` + model + `","max_tokens":16,"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestModelFallbackRetriesOnceOnFirstFallback(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	t.Setenv("KIRO_MAX_UPSTREAM_ATTEMPTS", "4")
	if err := config.UpdateModelFallbacks([]config.ModelFallback{{Model: "test-primary", Fallbacks: []string{"test-backup", "test-third"}}}); err != nil {
		t.Fatal(err)
	}
	srv, hits := fallbackUpstream(t, map[string]string{"test-primary": `{"reason":"MODEL_TEMPORARILY_UNAVAILABLE"}`})
	defer srv.Close()
	defer swapKiroEndpointsForTest(t, srv)()

	for _, stream := range []bool{false, true} {
		rec := postMessages(h, "test-primary", stream)
		if rec.Code != http.StatusOK {
			t.Fatalf("stream=%v: status %d body=%s", stream, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get(fallbackModelHeader); got != "test-backup" {
			t.Fatalf("stream=%v: fallback header = %q", stream, got)
		}
		if !strings.Contains(rec.Body.String(), "served by test-backup") {
			t.Fatalf("stream=%v: body does not come from the fallback: %s", stream, rec.Body.String())
		}
	}
	if hitCount(hits, "test-third") != 0 {
		t.Fatal("only the first fallback may be used")
	}
	if n := hitCount(hits, "test-backup"); n != 2 {
		t.Fatalf("fallback calls = %d, want one per request", n)
	}
}

func TestModelFallbackHappensOnlyOnce(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	noRetryWait(t)
	t.Setenv("KIRO_MAX_UPSTREAM_ATTEMPTS", "8")
	if err := config.UpdateModelFallbacks([]config.ModelFallback{
		{Model: "test-primary", Fallbacks: []string{"test-backup"}},
		{Model: "test-backup", Fallbacks: []string{"test-third"}},
	}); err != nil {
		t.Fatal(err)
	}
	down := `{"reason":"MODEL_TEMPORARILY_UNAVAILABLE"}`
	srv, hits := fallbackUpstream(t, map[string]string{"test-primary": down, "test-backup": down})
	defer srv.Close()
	defer swapKiroEndpointsForTest(t, srv)()

	rec := postMessages(h, "test-primary", false)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected failure, got 200: %s", rec.Body.String())
	}
	if hitCount(hits, "test-backup") == 0 || hitCount(hits, "test-third") != 0 {
		t.Fatalf("expected exactly one model switch; backup=%d third=%d", hitCount(hits, "test-backup"), hitCount(hits, "test-third"))
	}
}

func TestNoModelFallbackOnRateLimit(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	noRetryWait(t)
	if err := config.UpdateModelFallbacks([]config.ModelFallback{{Model: "test-primary", Fallbacks: []string{"test-backup"}}}); err != nil {
		t.Fatal(err)
	}
	srv, hits := fallbackUpstream(t, map[string]string{"test-primary": `Too many requests`})
	defer srv.Close()
	defer swapKiroEndpointsForTest(t, srv)()

	rec := postMessages(h, "test-primary", false)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	if hitCount(hits, "test-backup") != 0 || rec.Header().Get(fallbackModelHeader) != "" {
		t.Fatal("a rate limit must not trigger a model fallback")
	}
}

func TestNoModelFallbackWithoutBudget(t *testing.T) {
	ctx := withUpstreamBudget(t.Context(), 1)
	_ = takeUpstreamAttempt(ctx)
	if err := config.InitForTest(t, t.TempDir()+"/config.json"); err != nil {
		t.Fatal(err)
	}
	_ = config.UpdateModelFallbacks([]config.ModelFallback{{Model: "a", Fallbacks: []string{"b"}}})
	if fb := fallbackCandidate(ctx, "a"); fb != "" {
		t.Fatalf("fallback %q offered with an exhausted attempt budget", fb)
	}
}

// ---- Budgets ----

func TestDailyBudgetRejectsInAPIShape(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	if err := config.UpdateBudgetSettings(config.BudgetSettings{DailyCreditLimit: 5}); err != nil {
		t.Fatal(err)
	}
	config.RecordDailyCredits("", 5, time.Now())

	rec := postMessages(h, "anything", false)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"rate_limit_error"`) {
		t.Fatalf("anthropic: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After until the budget day resets")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), `"insufficient_quota"`) {
		t.Fatalf("openai: status %d body %s", rec.Code, rec.Body.String())
	}

	// Raising the cap lets requests through again (no upstream needed to
	// see that the budget check passed: count_tokens is never capped).
	req = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"x","messages":[{"role":"user","content":"hi"}]}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("count_tokens must not be subject to the credit budget")
	}
}

func TestPerKeyDailyBudget(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	key, err := config.AddApiKey(config.ApiKeyEntry{Name: "team", Key: "sk-budget-test", Enabled: true, DailyCreditLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = config.AddApiKey(config.ApiKeyEntry{Name: "other", Key: "sk-budget-other", Enabled: true})
	if err := config.UpdateSettingsPatch(nil, boolPtr(true), ""); err != nil {
		t.Fatal(err)
	}
	config.RecordDailyCredits(key.ID, 2.5, time.Now())

	send := func(k string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"x","messages":[]}`))
		req.Header.Set("Authorization", "Bearer "+k)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := send("sk-budget-test"); rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "API key's daily credit budget") {
		t.Fatalf("capped key: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := send("sk-budget-other"); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("uncapped key must not be rejected: %s", rec.Body.String())
	}
}

// ---- Alerts ----

func TestAlertsPostOncePerThresholdPerDay(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	// A fresh pool: the shared one may carry cooldowns from other tests,
	// which would raise account alerts here.
	h.pool = accountpool.NewTestPool(config.GetEnabledAccounts())
	var mu sync.Mutex
	var got []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]interface{}
		json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}))
	defer srv.Close()
	// A plain transport: the default one would cache the proxy environment
	// for the whole test binary.
	oldClient := alertWebhookClient
	alertWebhookClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer func() { alertWebhookClient = oldClient }()
	if err := config.UpdateBudgetSettings(config.BudgetSettings{DailyCreditLimit: 10, AlertWebhookURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	config.RecordDailyCredits("", 8.5, now)
	if n := h.dispatchAlerts(now); n != 1 {
		t.Fatalf("first evaluation sent %d alerts, want 1 warning", n)
	}
	if n := h.dispatchAlerts(now); n != 0 {
		t.Fatalf("repeat evaluation sent %d alerts, want 0", n)
	}
	config.RecordDailyCredits("", 2, now)
	if n := h.dispatchAlerts(now); n != 1 {
		t.Fatalf("crossing 100%% sent %d alerts, want 1 critical", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("webhook received %d posts, want 2", len(got))
	}
	levels := map[string]bool{}
	for _, p := range got {
		levels[p["level"].(string)] = true
		if p["kind"] != "global_daily" || p["text"] == "" || p["limit"].(float64) != 10 {
			t.Fatalf("unexpected payload %v", p)
		}
	}
	if !levels["warning"] || !levels["critical"] {
		t.Fatalf("levels = %v", levels)
	}
}

func TestAlertsCoverKeyCapsAndPoolUsage(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	if _, err := config.AddApiKey(config.ApiKeyEntry{Name: "k", Key: "sk-alert", Enabled: true, CreditLimit: 10, DailyCreditLimit: 1}); err != nil {
		t.Fatal(err)
	}
	keys := config.ListApiKeys()
	_ = config.RecordApiKeyUsage(keys[0].ID, 0, 9)
	config.RecordDailyCredits(keys[0].ID, 1, time.Now())
	_ = config.UpdateAccountInfo("test-account", config.AccountInfo{UsageCurrent: 95, UsageLimit: 100, LastRefresh: 1})
	h.pool.Reload()

	kinds := map[string]string{}
	for _, a := range h.computeAlerts(time.Now()) {
		kinds[a.Kind] = a.Level
	}
	want := map[string]string{"key_daily": "critical", "key_credits": "warning", "pool_usage": "warning"}
	for k, lvl := range want {
		if kinds[k] != lvl {
			t.Fatalf("alerts = %v, want %s=%s", kinds, k, lvl)
		}
	}
}

// ---- Usage refresh ----

func TestUsageRefreshScheduling(t *testing.T) {
	now := time.Now()
	fresh := &config.Account{ID: "a", LastRefresh: now.Add(-5 * time.Minute).Unix()}
	stale := &config.Account{ID: "a", LastRefresh: now.Add(-16 * time.Minute).Unix()}
	never := &config.Account{ID: "a"}
	if usageRefreshDue(fresh, now) || !usageRefreshDue(stale, now) || !usageRefreshDue(never, now) {
		t.Fatal("usage refresh must be due after 10–15 minutes and immediately without data")
	}
	if j := usageJitter("a", 1); j < 0 || j >= usageRefreshJitter {
		t.Fatalf("jitter %v outside window", j)
	}
}

func TestQuotaErrorForcesOneUsageRefresh(t *testing.T) {
	h, cleanup := setupResponsesTestHandler(t)
	defer cleanup()
	h.stopRefresh = make(chan struct{})
	var calls atomic.Int32
	old := refreshAccountInfoFn
	refreshAccountInfoFn = func(acc *config.Account) (*config.AccountInfo, error) {
		calls.Add(1)
		return &config.AccountInfo{UsageCurrent: 1, UsageLimit: 10, LastRefresh: time.Now().Unix()}, nil
	}
	defer func() { refreshAccountInfoFn = old }()

	_ = config.UpdateAccountInfo("test-account", config.AccountInfo{LastRefresh: time.Now().Unix()})
	if n := h.refreshDueUsage(time.Now()); n != 0 {
		t.Fatalf("fresh account refreshed %d times", n)
	}
	acc := config.GetAccounts()[0]
	h.handleAccountFailure(&acc, errors.New(`HTTP 429 from test: {"reason":"MONTHLY_REQUEST_COUNT"}`))
	h.handleAccountFailure(&acc, errors.New(`HTTP 429 from test: {"reason":"MONTHLY_REQUEST_COUNT"}`))
	if n := h.refreshDueUsage(time.Now()); n != 1 || calls.Load() != 1 {
		t.Fatalf("forced refreshes = %d (calls %d), want 1", n, calls.Load())
	}
	if n := h.refreshDueUsage(time.Now()); n != 0 {
		t.Fatalf("forced refresh must be consumed, got %d", n)
	}
}
