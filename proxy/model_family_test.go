package proxy

import "testing"

func TestClaudeCodeDefaultNamesUpgradeToOfferedFamily(t *testing.T) {
	resetOfferedModels(t)

	// Nothing known yet: names are only normalized.
	if got := MapModel("claude-haiku-4-5-20251001"); got != "claude-haiku-4.5" {
		t.Fatalf("cold: %q", got)
	}
	recordOfferedModels([]ModelInfo{{ModelId: "auto"}, {ModelId: "claude-haiku-5.5"}, {ModelId: "claude-sonnet-5.5"},
		{ModelId: "claude-sonnet-5"}, {ModelId: "claude-opus-5.5"}, {ModelId: "glm-5"}})
	for in, want := range map[string]string{
		"claude-haiku-4-5-20251001":  "claude-haiku-5.5",
		"claude-sonnet-4-5-20250929": "claude-sonnet-5.5",
		"claude-sonnet-4-20250514":   "claude-sonnet-5.5",
		"claude-opus-4-1-20250805":   "claude-opus-5.5",
		"claude-3-haiku-20240307":    "claude-haiku-5.5",
		"claude-sonnet-5":            "claude-sonnet-5", // offered: unchanged
		"claude-haiku-5.5":           "claude-haiku-5.5",
		"glm-5":                      "glm-5",
		"gpt-5.6-sol":                "gpt-5.6-sol",
	} {
		if got := MapModel(in); got != want {
			t.Errorf("MapModel(%q) = %q, want %q", in, got, want)
		}
	}
}

// resetOfferedModels gives a test an empty offered-model set (other tests fill
// it through model refreshes) and empties it again afterwards.
func resetOfferedModels(t *testing.T) {
	clear := func() { offeredModels.Range(func(k, _ interface{}) bool { offeredModels.Delete(k); return true }) }
	clear()
	t.Cleanup(clear)
}
