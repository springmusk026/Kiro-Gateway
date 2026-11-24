package proxy

import (
	"testing"
	"time"

	"kiro-go/config"
	"kiro-go/pool"
)

func TestPickAccountAffinity(t *testing.T) {
	mustInitConfig(t)
	p := pool.NewTestPool([]config.Account{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	h := &Handler{pool: p}

	t.Setenv("KIRO_SESSION_AFFINITY", "")
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		seen[h.pickAccount("key1", "m", nil).ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("affinity off must keep round-robin, saw %v", seen)
	}

	t.Setenv("KIRO_SESSION_AFFINITY", "1")
	first := h.pickAccount("key1", "m", nil).ID
	for i := 0; i < 5; i++ {
		if got := h.pickAccount("key1", "m", nil).ID; got != first {
			t.Fatalf("affinity broke: got %s want %s", got, first)
		}
	}
	// Excluded (failed this request) → fall back and rebind.
	next := h.pickAccount("key1", "m", map[string]bool{first: true}).ID
	if next == first {
		t.Fatal("excluded account must not be returned")
	}
	// Cooling-down binding is skipped.
	p.SetCooldownForTest(next, time.Minute)
	if got := h.pickAccount("key1", "m", nil).ID; got == next {
		t.Fatal("cooling-down account must not be reused via affinity")
	}
	// A failure on the bound account drops the binding.
	bound := h.pickAccount("key1", "m", nil).ID
	h.dropAffinity("key1", bound)
	if _, ok := h.sessionAffinity.Load("key1"); ok {
		t.Fatal("binding should be dropped")
	}
}
