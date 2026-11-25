package proxy

import "testing"

// A conversation that grows past the budget must keep the same cut for many
// turns, so Kiro's prefix cache keeps hitting, and must always fit.
func TestStableTruncationCutKeepsPrefixAcrossTurns(t *testing.T) {
	const limit, base = 100_000, 5_000
	var sizes []int
	cuts := map[int]int{}
	prev, changes, turnsOverBudget := -1, 0, 0
	for turn := 0; turn < 400; turn++ {
		sizes = append(sizes, 600+(turn*37)%900) // user or assistant entry
		total := base
		for _, s := range sizes {
			total += s
		}
		cut := stableTruncationCut(sizes, base, limit)
		kept := base
		for _, s := range sizes[cut:] {
			kept += s
		}
		if kept > limit {
			t.Fatalf("turn %d: kept %d bytes > limit %d", turn, kept, limit)
		}
		if len(sizes)-cut < minRecentHistoryTurns && len(sizes) >= minRecentHistoryTurns {
			t.Fatalf("turn %d: kept only %d entries", turn, len(sizes)-cut)
		}
		if total > limit {
			turnsOverBudget++
			if cut != prev {
				changes++
			}
		} else if cut != 0 {
			t.Fatalf("turn %d: cut %d although everything fits", turn, cut)
		}
		cuts[cut]++
		prev = cut
	}
	if turnsOverBudget < 200 {
		t.Fatalf("test did not exercise truncation enough: %d turns over budget", turnsOverBudget)
	}
	// With quarter-budget steps the cut moves roughly once per quarter budget
	// of growth (~25 turns here), not on every turn.
	if changes > turnsOverBudget/10 {
		t.Fatalf("cut changed on %d of %d truncated turns; prefix would rarely be cached", changes, turnsOverBudget)
	}
}

func TestStableTruncationCutFallsBackForHugeTail(t *testing.T) {
	// The last entries alone exceed the budget: keep the minimum tail.
	sizes := []int{10, 10, 10, 10, 50_000, 50_000, 50_000, 50_000}
	cut := stableTruncationCut(sizes, 1_000, 60_000)
	if got := len(sizes) - cut; got != minRecentHistoryTurns {
		t.Fatalf("kept %d entries, want %d", got, minRecentHistoryTurns)
	}
}
