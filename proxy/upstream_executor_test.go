package proxy

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
	accountpool "github.com/springmusk026/Kiro-Gateway/pool"
)

// executorHarness runs executeUpstream against a real account pool with a
// scripted upstream: script decides each call's outcome from the account and
// model it runs on.
type executorHarness struct {
	t *testing.T
	h *Handler

	mu    sync.Mutex
	calls []executorCall

	started      bool
	responded    int
	startedErr   error
	failErr      error
	failModel    string
	noAccounts   int
	successUsage upstreamUsage
}

type executorCall struct {
	account string
	model   string
}

func newExecutorHarness(t *testing.T, accounts ...string) *executorHarness {
	t.Helper()
	if err := config.InitForTest(t, t.TempDir()+"/config.json"); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	list := make([]config.Account, 0, len(accounts))
	for _, id := range accounts {
		list = append(list, config.Account{ID: id, Enabled: true, AccessToken: "token-" + id,
			ProfileArn: "arn:aws:codewhisperer:profile/" + id})
	}
	p := accountpool.NewTestPool(list)
	return &executorHarness{t: t, h: &Handler{pool: p, promptCache: newPromptCacheTracker(defaultPromptCacheTTL)}}
}

// run executes one request; script returns the error of a call (nil means
// success) and may mark output as started.
func (x *executorHarness) run(ctx context.Context, model string, script func(n int, c executorCall, cb *KiroStreamCallback) error) {
	x.t.Helper()
	prev := upstreamAttemptRunner
	upstreamAttemptRunner = func(ctx context.Context, account *config.Account, payload *KiroPayload, callback *KiroStreamCallback,
		measure func() (int, int, string, bool), reset func(), canRetry func() bool) error {
		c := executorCall{account: account.ID, model: payload.ConversationState.CurrentMessage.UserInputMessage.ModelID}
		x.mu.Lock()
		n := len(x.calls)
		x.calls = append(x.calls, c)
		x.mu.Unlock()
		return script(n, c, callback)
	}
	defer func() { upstreamAttemptRunner = prev }()

	payload := &KiroPayload{}
	payload.ConversationState.ConversationID = "conv-test"
	payload.ConversationState.CurrentMessage.UserInputMessage.ModelID = model
	w := httptest.NewRecorder()
	x.h.executeUpstream(ctx, w, &upstreamRequest{
		endpoint: "claude",
		payload:  payload,
		model:    model,
		started:  func() bool { return x.started },
		attempt: func(a *upstreamAttempt) upstreamAttemptRun {
			var text string
			return upstreamAttemptRun{
				callback: &KiroStreamCallback{OnText: func(s string, _ bool) {
					text += s
					x.started = true
				}},
				measure: func() (int, int, string, bool) { return len(text), 0, "end_turn", false },
				reset:   func() { text = "" },
				finish:  func() upstreamUsage { return upstreamUsage{inputTokens: 10, outputTokens: 5, credits: 0.5} },
				respond: func(u upstreamUsage) { x.responded++; x.successUsage = u },
			}
		},
		failStarted:    func(_ *upstreamAttempt, err error) { x.startedErr = err },
		failNoAccounts: func(string) { x.noAccounts++ },
		fail:           func(model string, err error) { x.failModel, x.failErr = model, err },
	})
}

func (x *executorHarness) accountsTried() []string {
	out := make([]string, 0, len(x.calls))
	for _, c := range x.calls {
		out = append(out, c.account)
	}
	return out
}

func TestExecutorFailsOverToNextAccountInOrder(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b", "acc-c")
	x.run(context.Background(), "test-model", func(n int, _ executorCall, _ *KiroStreamCallback) error {
		if n < 2 {
			return errors.New("HTTP 429 Too many requests")
		}
		return nil
	})
	tried := x.accountsTried()
	if len(tried) != 3 {
		t.Fatalf("attempts = %v, want 3", tried)
	}
	seen := map[string]bool{}
	for _, id := range tried {
		if seen[id] {
			t.Fatalf("account %s tried twice: %v", id, tried)
		}
		seen[id] = true
	}
	if x.responded != 1 || x.failErr != nil || x.noAccounts != 0 {
		t.Fatalf("responded=%d fail=%v noAccounts=%d", x.responded, x.failErr, x.noAccounts)
	}
	if x.successUsage.inputTokens != 10 || x.successUsage.outputTokens != 5 {
		t.Fatalf("usage = %+v", x.successUsage)
	}
	// The throttled accounts are cooling down; the one that succeeded is not.
	if got := x.h.pool.AvailableCount(); got != 1 {
		t.Fatalf("available accounts = %d, want 1", got)
	}
}

func TestExecutorStopsAfterMaxAccountAttempts(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b", "acc-c", "acc-d")
	x.run(context.Background(), "test-model", func(int, executorCall, *KiroStreamCallback) error {
		return errors.New("HTTP 500 internal failure")
	})
	if len(x.calls) != maxAccountRetryAttempts {
		t.Fatalf("attempts = %d, want %d", len(x.calls), maxAccountRetryAttempts)
	}
	if x.failErr == nil || x.responded != 0 {
		t.Fatalf("fail=%v responded=%d", x.failErr, x.responded)
	}
}

func TestExecutorDoesNotRetryAfterOutputStarted(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b")
	x.run(context.Background(), "test-model", func(_ int, _ executorCall, cb *KiroStreamCallback) error {
		cb.OnText("partial", false)
		return errors.New("HTTP 500 stream broke")
	})
	if len(x.calls) != 1 {
		t.Fatalf("attempts = %d, want 1 (output already reached the client)", len(x.calls))
	}
	if x.startedErr == nil || x.failErr != nil || x.responded != 0 {
		t.Fatalf("startedErr=%v fail=%v responded=%d", x.startedErr, x.failErr, x.responded)
	}
}

func TestExecutorTerminalClientErrorIsNotRetriedOrPenalised(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b")
	x.run(context.Background(), "test-model", func(int, executorCall, *KiroStreamCallback) error {
		return errors.New("HTTP 400 ValidationException: improperly formed request")
	})
	if len(x.calls) != 1 {
		t.Fatalf("attempts = %d, want 1", len(x.calls))
	}
	if x.failErr == nil || !isTerminalUpstreamError(x.failErr) {
		t.Fatalf("fail = %v, want the client error", x.failErr)
	}
	if got := x.h.pool.AvailableCount(); got != 2 {
		t.Fatalf("available accounts = %d, want 2 (client errors are not the account's fault)", got)
	}
}

func TestExecutorBudgetExhaustionEndsWithoutPenalty(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b", "acc-c")
	ctx := withUpstreamBudget(context.Background(), 1)
	x.run(ctx, "test-model", func(_ int, _ executorCall, _ *KiroStreamCallback) error {
		if err := takeUpstreamAttempt(ctx); err != nil {
			return err
		}
		return errors.New("HTTP 429 Too many requests")
	})
	if !errors.Is(x.failErr, errUpstreamBudgetExhausted) {
		t.Fatalf("fail = %v, want budget exhausted", x.failErr)
	}
	if len(x.calls) > maxAccountRetryAttempts {
		t.Fatalf("attempts = %d", len(x.calls))
	}
	// Only the account that really failed upstream cools down.
	if got := x.h.pool.AvailableCount(); got != 2 {
		t.Fatalf("available accounts = %d, want 2", got)
	}
}

func TestExecutorClientCancelIsNotPenalised(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b")
	ctx, cancel := context.WithCancel(context.Background())
	x.run(ctx, "test-model", func(int, executorCall, *KiroStreamCallback) error {
		cancel()
		return context.Canceled
	})
	if len(x.calls) != 1 {
		t.Fatalf("attempts = %d, want 1", len(x.calls))
	}
	if x.failErr != nil || x.startedErr != nil || x.noAccounts != 0 || x.responded != 0 {
		t.Fatalf("a cancelled client must get no response: fail=%v started=%v none=%d ok=%d",
			x.failErr, x.startedErr, x.noAccounts, x.responded)
	}
	if got := x.h.pool.AvailableCount(); got != 2 {
		t.Fatalf("available accounts = %d, want 2", got)
	}
}

func TestExecutorModelFallbackRetriesOnce(t *testing.T) {
	x := newExecutorHarness(t, "acc-a", "acc-b")
	if err := config.UpdateModelFallbacks([]config.ModelFallback{
		{Model: "test-primary", Fallbacks: []string{"test-backup"}},
		{Model: "test-backup", Fallbacks: []string{"test-third"}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx := withUpstreamBudget(context.Background(), 8)
	x.run(ctx, "test-primary", func(_ int, c executorCall, _ *KiroStreamCallback) error {
		return errors.New(`HTTP 500 {"reason":"MODEL_TEMPORARILY_UNAVAILABLE"}`)
	})
	var models []string
	for _, c := range x.calls {
		models = append(models, c.model)
	}
	// One call on the primary (model-unavailable stops the account loop), then
	// the fallback model, and never a second switch.
	if len(models) < 2 || models[0] != "test-primary" || models[1] != "test-backup" {
		t.Fatalf("models tried = %v", models)
	}
	for _, m := range models {
		if m == "test-third" {
			t.Fatalf("second fallback used: %v", models)
		}
	}
	if x.failErr == nil || x.failModel != "test-backup" {
		t.Fatalf("fail = %v on %q, want an error on test-backup", x.failErr, x.failModel)
	}
}

func TestExecutorModelFallbackSucceeds(t *testing.T) {
	x := newExecutorHarness(t, "acc-a")
	if err := config.UpdateModelFallbacks([]config.ModelFallback{{Model: "test-primary", Fallbacks: []string{"test-backup"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := withUpstreamBudget(context.Background(), 4)
	x.run(ctx, "test-primary", func(_ int, c executorCall, _ *KiroStreamCallback) error {
		if c.model == "test-primary" {
			return errors.New(`HTTP 500 {"reason":"MODEL_TEMPORARILY_UNAVAILABLE"}`)
		}
		return nil
	})
	if len(x.calls) != 2 || x.responded != 1 {
		t.Fatalf("calls=%v responded=%d", x.calls, x.responded)
	}
}

func TestExecutorNoAccounts(t *testing.T) {
	x := newExecutorHarness(t)
	x.run(context.Background(), "test-model", func(int, executorCall, *KiroStreamCallback) error { return nil })
	if x.noAccounts != 1 || len(x.calls) != 0 {
		t.Fatalf("noAccounts=%d calls=%d", x.noAccounts, len(x.calls))
	}
}
