package pricing

import (
	"math"
	"os"
	"testing"
)

func loadSample(t *testing.T) *Catalog {
	t.Helper()
	data, err := os.ReadFile("testdata/models-dev-sample.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseModelsDev(data, "test")
	if err != nil {
		t.Fatal(err)
	}
	Set(c)
	t.Cleanup(func() { Set(nil) })
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestLookupKiroIDs(t *testing.T) {
	c := loadSample(t)
	if _, ok := c.Models["anthropic/claude-haiku-5-5"]; ok {
		t.Fatal("resellers must be ignored")
	}
	for kiro, want := range map[string]string{
		"claude-haiku-5.5":           "claude-haiku-5-5",
		"claude-sonnet-5.5-thinking": "claude-sonnet-5-5",
		"CLAUDE-OPUS-5.5":            "claude-opus-5-5",
		"gpt-5.6-sol":                "gpt-5.6-sol",
		"glm-5":                      "glm-5",
	} {
		m, ok := Lookup(kiro)
		if !ok || m.ID != want {
			t.Errorf("Lookup(%q) = %q %v, want %q", kiro, m.ID, ok, want)
		}
	}
	for _, unknown := range []string{"auto", "", "claude-haiku-9.9"} {
		if _, ok := Lookup(unknown); ok {
			t.Errorf("Lookup(%q) should be unknown", unknown)
		}
	}
}

func TestCost(t *testing.T) {
	loadSample(t)
	// sonnet-5.5: $2 in / $10 out / $0.10 cache read / $2.50 cache write per 1M.
	got, ok := Cost("claude-sonnet-5.5", Usage{Input: 1_000_000, Output: 100_000})
	if !ok || !near(got, 2+1) {
		t.Fatalf("plain cost = %v %v", got, ok)
	}
	// 800k of the 1M prompt read from cache, 100k written.
	got, _ = Cost("claude-sonnet-5.5", Usage{Input: 1_000_000, Output: 0, CacheRead: 800_000, CacheWrite: 100_000})
	if want := 0.1*2 + 0.8*0.1 + 0.1*2.5; !near(got, want) {
		t.Fatalf("cached cost = %v, want %v", got, want)
	}
	// haiku-5.5 costs 5x above 100k tokens of context.
	small, _ := Cost("claude-haiku-5.5", Usage{Input: 100_000, Output: 1000})
	large, _ := Cost("claude-haiku-5.5", Usage{Input: 100_001, Output: 1000})
	if !near(small, 0.1*0.1+0.001*0.5) || !near(large, 0.100001*0.5+0.001*2.5) {
		t.Fatalf("tiers: small=%v large=%v", small, large)
	}
	if _, ok := Cost("auto", Usage{Input: 10}); ok {
		t.Fatal("auto has no price")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	c := loadSample(t)
	dir := t.TempDir()
	if err := SaveCache(dir, c); err != nil {
		t.Fatal(err)
	}
	back, err := LoadCache(dir)
	if err != nil || len(back.Models) != len(c.Models) || back.Models["claude-haiku-5-5"].Price.Tiers[0].Above != 100_000 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}
