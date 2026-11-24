package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"kiro-go/config"
)

func autoCfg() config.ThinkingConfig {
	return config.ThinkingConfig{Suffix: "-thinking", Mode: "auto", Budget: config.DefaultThinkingBudget}
}

func TestResolveThinking_Policy(t *testing.T) {
	cases := []struct {
		name    string
		ask     thinkingAsk
		mode    string
		enabled bool
		budget  int
		effort  string
	}{
		{"nothing", thinkingAsk{}, "auto", false, 0, ""},
		{"suffix", thinkingAsk{Suffix: true}, "auto", true, 200000, ""},
		{"suffix beats disabled", thinkingAsk{Suffix: true, Explicit: boolPtr(false)}, "auto", true, 200000, ""},
		{"anthropic enabled budget", thinkingAsk{Explicit: boolPtr(true), Budget: 8000}, "auto", true, 8000, ""},
		{"adaptive + effort", thinkingAsk{Explicit: boolPtr(true), Effort: "medium"}, "auto", true, 24576, "medium"},
		{"explicit off keeps effort", thinkingAsk{Explicit: boolPtr(false), Effort: "low"}, "auto", false, 0, "low"},
		{"openai effort implies on", thinkingAsk{Effort: "HIGH"}, "auto", true, 65536, "high"},
		{"effort none", thinkingAsk{Effort: "none"}, "auto", false, 0, ""},
		{"budget clamped low", thinkingAsk{Explicit: boolPtr(true), Budget: 10}, "auto", true, 1024, ""},
		{"budget clamped high", thinkingAsk{Explicit: boolPtr(true), Budget: 900000}, "auto", true, 200000, ""},
		{"always", thinkingAsk{}, "always", true, 200000, ""},
		{"never beats suffix", thinkingAsk{Suffix: true, Effort: "high"}, "never", false, 0, "high"},
	}
	for _, tc := range cases {
		cfg := autoCfg()
		cfg.Mode = tc.mode
		got := resolveThinking(tc.ask, cfg)
		if got.Enabled != tc.enabled || got.Budget != tc.budget || got.Effort != tc.effort {
			t.Errorf("%s: got %+v, want enabled=%v budget=%d effort=%q", tc.name, got, tc.enabled, tc.budget, tc.effort)
		}
	}
}

func TestResolveThinking_ConfiguredDefaultBudget(t *testing.T) {
	cfg := autoCfg()
	cfg.Budget = 32000
	if got := resolveThinking(thinkingAsk{Suffix: true}, cfg); got.Budget != 32000 {
		t.Fatalf("budget = %d, want 32000", got.Budget)
	}
}

func TestThinkingModePrompt(t *testing.T) {
	if thinkingModePrompt(0) != ThinkingModePrompt || thinkingModePrompt(200000) != ThinkingModePrompt {
		t.Fatal("default budget must keep the original prompt byte-for-byte (prompt cache)")
	}
	if p := thinkingModePrompt(8000); !strings.Contains(p, "<max_thinking_length>8000</max_thinking_length>") {
		t.Fatalf("unexpected prompt %q", p)
	}
}

func TestRequestExtraction(t *testing.T) {
	var c ClaudeRequest
	json.Unmarshal([]byte(`{"model":"m","thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`), &c)
	ask := claudeThinkingAsk(&c, false)
	if ask.Explicit == nil || !*ask.Explicit || ask.Effort != "low" {
		t.Fatalf("claude ask = %+v", ask)
	}

	var o OpenAIRequest
	json.Unmarshal([]byte(`{"model":"m","reasoning_effort":"minimal"}`), &o)
	if ask := openAIThinkingAsk(&o, false); ask.Effort != "minimal" {
		t.Fatalf("openai ask = %+v", ask)
	}
	var o2 OpenAIRequest
	json.Unmarshal([]byte(`{"model":"m","reasoning":{"max_tokens":5000}}`), &o2)
	if ask := openAIThinkingAsk(&o2, false); ask.Explicit == nil || !*ask.Explicit || ask.Budget != 5000 {
		t.Fatalf("openrouter ask = %+v", ask)
	}

	var r ResponsesRequest
	json.Unmarshal([]byte(`{"model":"m","input":"hi","reasoning":{"effort":"high","summary":"auto"}}`), &r)
	if ask := responsesThinkingAsk(&r, false); ask.Effort != "high" || ask.Summary != "auto" {
		t.Fatalf("responses ask = %+v", ask)
	}
}

func TestTranslatorUsesResolvedBudget(t *testing.T) {
	req := &ClaudeRequest{Model: "claude-sonnet-4.5", Messages: []ClaudeMessage{{Role: "user", Content: "hi"}}}
	req.thinkingBudget = 8192
	raw := mustMarshalPayload(t, ClaudeToKiro(req, true))
	if !strings.Contains(raw, "8192") {
		t.Fatal("claude payload missing resolved thinking budget")
	}
	oreq := &OpenAIRequest{Model: "claude-sonnet-4.5", Messages: []OpenAIMessage{{Role: "user", Content: "hi"}}}
	oreq.thinkingBudget = 4096
	if raw := mustMarshalPayload(t, OpenAIToKiro(oreq, true)); !strings.Contains(raw, "4096") {
		t.Fatal("openai payload missing resolved thinking budget")
	}
}

const sampleClaudeSchema = `{"type":"object","properties":{"output_config":{"type":"object","properties":{"effort":{"type":"string","enum":["low","medium","high","xhigh","max"],"default":"high"}}},"thinking":{"type":"object","properties":{"type":{"type":"string","enum":["adaptive","disabled"]}}}}}`

func TestParseReasoningControls(t *testing.T) {
	rc, ok := parseReasoningControls(json.RawMessage(sampleClaudeSchema))
	if !ok || rc.Path != "output_config" || len(rc.Levels) != 5 || rc.DefaultLevel != "high" || !rc.Toggle {
		t.Fatalf("object schema: %+v ok=%v", rc, ok)
	}
	quoted, _ := json.Marshal(`{"properties":{"reasoning":{"properties":{"effort":{"enum":["low","medium","high"]}}}}}`)
	rc, ok = parseReasoningControls(quoted)
	if !ok || rc.Path != "reasoning" || rc.Toggle {
		t.Fatalf("string schema: %+v ok=%v", rc, ok)
	}
	if _, ok := parseReasoningControls(json.RawMessage(`{}`)); ok {
		t.Fatal("empty schema must report no controls")
	}
}

func TestNativeReasoningFields(t *testing.T) {
	recordModelReasoningControls([]ModelInfo{
		{ModelId: "claude-test-1", AdditionalFieldsSchema: json.RawMessage(sampleClaudeSchema)},
		{ModelId: "gpt-test-1", AdditionalFieldsSchema: json.RawMessage(`{"properties":{"reasoning":{"properties":{"effort":{"enum":["low","medium","high"]}}}}}`)},
		{ModelId: "plain-test-1"},
	})
	on := func(e string) thinkingSpec { return thinkingSpec{Enabled: true, Effort: e} }

	if f := nativeReasoningFields("claude-test-1", on("medium")); f["output_config"].(map[string]interface{})["effort"] != "medium" {
		t.Fatalf("claude fields = %v", f)
	}
	if f := nativeReasoningFields("gpt-test-1", on("max")); f["reasoning"].(map[string]interface{})["effort"] != "high" {
		t.Fatalf("max must clamp to high for gpt: %v", f)
	}
	if f := nativeReasoningFields("gpt-test-1", on("minimal")); f["reasoning"].(map[string]interface{})["effort"] != "low" {
		t.Fatalf("minimal must step up to low: %v", f)
	}
	if f := nativeReasoningFields("claude-test-1", thinkingSpec{Effort: "max"}); f["output_config"].(map[string]interface{})["effort"] != "high" {
		t.Fatalf("thinking off caps xhigh/max: %v", f)
	}
	if f := nativeReasoningFields("plain-test-1", on("high")); f != nil {
		t.Fatalf("model without schema must get nothing: %v", f)
	}
	if f := nativeReasoningFields("claude-test-1", on("")); f != nil {
		t.Fatalf("no effort must send nothing: %v", f)
	}
}

func TestAdditionalFieldsNeverInLegacyBody(t *testing.T) {
	p := &KiroPayload{AdditionalModelRequestFields: map[string]interface{}{"output_config": map[string]interface{}{"effort": "low"}}}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "effort") {
		t.Fatalf("legacy body leaked additionalModelRequestFields: %s", raw)
	}
}
