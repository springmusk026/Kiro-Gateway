package pool

import (
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func usageAcc(id string, used, limit float64) config.Account {
	return config.Account{ID: id, UsageCurrent: used, UsageLimit: limit, LastRefresh: 1}
}

func TestSelectionPrefersLowerUsageBand(t *testing.T) {
	p := NewTestPool([]config.Account{usageAcc("busy", 80, 100), usageAcc("idle", 5, 100), usageAcc("mid", 50, 100)})
	for i := 0; i < 10; i++ {
		if acc := p.GetNextForModelExcluding("m", nil); acc == nil || acc.ID != "idle" {
			t.Fatalf("pick %d: got %v, want idle", i, acc)
		}
	}
}

func TestSelectionRotatesWithinUsageBand(t *testing.T) {
	p := NewTestPool([]config.Account{usageAcc("a", 10, 100), usageAcc("b", 12, 100), usageAcc("c", 90, 100)})
	got := map[string]int{}
	for i := 0; i < 20; i++ {
		got[p.GetNext().ID]++
	}
	if got["a"] == 0 || got["b"] == 0 || got["c"] != 0 {
		t.Fatalf("expected rotation between a and b only, got %v", got)
	}
}

func TestSaturatedAccountUsedOnlyAsLastResort(t *testing.T) {
	p := NewTestPool([]config.Account{usageAcc("full", 98, 100), usageAcc("ok", 60, 100)})
	for i := 0; i < 5; i++ {
		if acc := p.GetNext(); acc.ID != "ok" {
			t.Fatalf("got %s, want ok", acc.ID)
		}
	}
	// With the only unsaturated account excluded, the saturated one serves.
	if acc := p.GetNextForModelExcluding("m", map[string]bool{"ok": true}); acc == nil || acc.ID != "full" {
		t.Fatalf("expected saturated account as last resort, got %v", acc)
	}
}

func TestUsageThresholdFromEnv(t *testing.T) {
	t.Setenv("KIRO_USAGE_THRESHOLD", "0.5")
	p := NewTestPool([]config.Account{usageAcc("half", 55, 100), usageAcc("other", 70, 100)})
	// Both are saturated at 0.5; saturation never makes the pool empty.
	if acc := p.GetNext(); acc == nil {
		t.Fatal("expected an account when all are saturated")
	}
	p = NewTestPool([]config.Account{usageAcc("half", 55, 100), usageAcc("low", 40, 100)})
	for i := 0; i < 5; i++ {
		if acc := p.GetNext(); acc.ID != "low" {
			t.Fatalf("got %s, want low", acc.ID)
		}
	}
}

func TestLocalUsageEstimateMovesAccountAcrossThreshold(t *testing.T) {
	p := NewTestPool([]config.Account{usageAcc("a", 90, 100), usageAcc("b", 92, 100)})
	if acc := p.GetPreferredForModel("a", "m", nil); acc == nil {
		t.Fatal("bound account below the threshold must stay bound")
	}
	// Credits reported by requests push a past the threshold before the
	// next upstream usage refresh.
	p.mu.Lock()
	p.recordUsageLocked(&p.accounts[0], 8)
	p.mu.Unlock()
	if acc := p.GetPreferredForModel("a", "m", nil); acc != nil {
		t.Fatalf("saturated bound account must release its binding while b has headroom")
	}
	if acc := p.GetNext(); acc.ID != "b" {
		t.Fatalf("got %s, want b", acc.ID)
	}
	// A new usage refresh resets the estimate.
	p.accounts[0].LastRefresh = 2
	if got := p.LocalUsageCredits("a", 2); got != 0 {
		t.Fatalf("estimate after refresh = %v, want 0", got)
	}
}

func TestBoundSaturatedAccountKeptWhenNoAlternative(t *testing.T) {
	p := NewTestPool([]config.Account{usageAcc("a", 99, 100), usageAcc("b", 99.5, 100)})
	if acc := p.GetPreferredForModel("a", "m", nil); acc == nil || acc.ID != "a" {
		t.Fatalf("binding must hold when every account is saturated, got %v", acc)
	}
}

func TestTrialCreditsCountTowardsHeadroom(t *testing.T) {
	trial := usageAcc("trial", 50, 50) // main pool used up ...
	trial.OverageStatus = "ENABLED"    // (kept routable)
	trial.TrialUsageLimit = 500        // ... but a large active trial pool
	trial.TrialStatus = "ACTIVE"
	p := NewTestPool([]config.Account{trial, usageAcc("plain", 300, 500)})
	if acc := p.GetNext(); acc.ID != "trial" {
		t.Fatalf("got %s, want trial (9%% used across both pools)", acc.ID)
	}
}

func TestUnknownLimitIsNotPenalized(t *testing.T) {
	p := NewTestPool([]config.Account{{ID: "unknown"}, usageAcc("known", 30, 100)})
	if acc := p.GetNext(); acc.ID != "unknown" {
		t.Fatalf("got %s, want unknown (no usage data ranks in the lowest band)", acc.ID)
	}
}
