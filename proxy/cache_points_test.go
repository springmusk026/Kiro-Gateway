package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func cachePointTestPayload(t *testing.T, model string, systemWords, tools int) *KiroPayload {
	t.Helper()
	req := &ClaudeRequest{
		Model:     model,
		MaxTokens: 16,
		System:    strings.Repeat("stable system instructions ", systemWords),
		Messages:  []ClaudeMessage{{Role: "user", Content: "hi"}},
	}
	for i := 0; i < tools; i++ {
		req.Tools = append(req.Tools, ClaudeTool{
			Name:        "tool_" + string(rune('a'+i)),
			Description: strings.Repeat("does something useful ", 50),
			InputSchema: map[string]interface{}{"type": "object"},
		})
	}
	return ClaudeToKiro(req, false)
}

func registerCachingModel(t *testing.T, id string, supported bool, minTokens, maxPoints int) {
	t.Helper()
	m := ModelInfo{ModelId: id}
	m.PromptCaching = &struct {
		SupportsPromptCaching             bool `json:"supportsPromptCaching"`
		MinimumTokensPerCacheCheckpoint   int  `json:"minimumTokensPerCacheCheckpoint"`
		MaximumCacheCheckpointsPerRequest int  `json:"maximumCacheCheckpointsPerRequest"`
	}{supported, minTokens, maxPoints}
	recordModelPromptCaching([]ModelInfo{m})
	t.Cleanup(func() { modelPromptCaching.Delete(strings.ToLower(id)) })
}

// wire decodes the serialized legacy body generically.
func wire(t *testing.T, p *KiroPayload) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(withCachePoints(p))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func jsonAt(m interface{}, keys ...interface{}) interface{} {
	for _, k := range keys {
		switch kk := k.(type) {
		case string:
			mm, _ := m.(map[string]interface{})
			m = mm[kk]
		case int:
			aa, _ := m.([]interface{})
			if kk < 0 {
				kk += len(aa)
			}
			if kk < 0 || kk >= len(aa) {
				return nil
			}
			m = aa[kk]
		}
	}
	return m
}

func TestCachePointsOffByDefault(t *testing.T) {
	registerCachingModel(t, "cache-model", true, 10, 4)
	p := cachePointTestPayload(t, "cache-model", 500, 3)
	b, _ := json.Marshal(withCachePoints(p))
	if strings.Contains(string(b), "cachePoint") {
		t.Fatal("cache points must not be sent unless KIRO_CACHE_POINTS=1")
	}
}

func TestCachePointsPayloadShape(t *testing.T) {
	t.Setenv("KIRO_CACHE_POINTS", "1")
	registerCachingModel(t, "cache-model", true, 100, 4)
	p := cachePointTestPayload(t, "cache-model", 500, 3)
	before, _ := json.Marshal(p)

	m := wire(t, p)
	hist := jsonAt(m, "conversationState", "history")
	if got := jsonAt(hist, 1, "assistantResponseMessage", "cachePoint", "type"); got != "default" {
		t.Fatalf("priming ack cachePoint = %v; history=%v", got, jsonAt(hist, 1))
	}
	if jsonAt(hist, 0, "userInputMessage", "cachePoint") != nil {
		t.Fatal("the system message itself carries no marker")
	}
	tools := jsonAt(m, "conversationState", "currentMessage", "userInputMessage", "userInputMessageContext", "tools").([]interface{})
	if len(tools) != 4 {
		t.Fatalf("tools = %d, want 3 + cache point", len(tools))
	}
	last := tools[3].(map[string]interface{})
	if len(last) != 1 || jsonAt(last, "cachePoint", "type") != "default" {
		t.Fatalf("last tool entry = %v, want only {cachePoint:{type:default}}", last)
	}
	if jsonAt(tools[0], "cachePoint") != nil || jsonAt(tools[0], "toolSpecification", "name") == nil {
		t.Fatalf("regular tool entries keep their shape: %v", tools[0])
	}
	// The shared payload is untouched (only the serialized copy has markers).
	after, _ := json.Marshal(p)
	if string(before) != string(after) || strings.Contains(string(after), "cachePoint") {
		t.Fatal("withCachePoints must not modify the payload")
	}
}

func TestCachePointsRespectLimits(t *testing.T) {
	t.Setenv("KIRO_CACHE_POINTS", "1")

	registerCachingModel(t, "one-point", true, 100, 1)
	m := wire(t, cachePointTestPayload(t, "one-point", 500, 3))
	if strings.Count(mustJSON(m), "cachePoint") != 1 {
		t.Fatalf("maximumCacheCheckpointsPerRequest=1 must yield one marker: %s", mustJSON(m))
	}

	registerCachingModel(t, "big-min", true, 1_000_000, 4)
	if s := mustJSON(wire(t, cachePointTestPayload(t, "big-min", 500, 3))); strings.Contains(s, "cachePoint") {
		t.Fatal("no marker when the stable prefix is below minimumTokensPerCacheCheckpoint")
	}

	registerCachingModel(t, "no-cache", false, 1, 4)
	if s := mustJSON(wire(t, cachePointTestPayload(t, "no-cache", 500, 3))); strings.Contains(s, "cachePoint") {
		t.Fatal("no marker for models without prompt caching")
	}

	if s := mustJSON(wire(t, cachePointTestPayload(t, "unknown-model", 500, 3))); strings.Contains(s, "cachePoint") {
		t.Fatal("no marker for models missing from the catalog")
	}

	// Short system prompt, long tools: only the tools marker (it closes
	// system + tools, which together reach the minimum).
	registerCachingModel(t, "tools-only", true, 500, 4)
	m = wire(t, cachePointTestPayload(t, "tools-only", 5, 6))
	if jsonAt(m, "conversationState", "history", 1, "assistantResponseMessage", "cachePoint") != nil {
		t.Fatal("system prefix below the minimum must not be marked")
	}
	if jsonAt(m, "conversationState", "currentMessage", "userInputMessage", "userInputMessageContext", "tools", -1, "cachePoint", "type") != "default" {
		t.Fatalf("tools marker missing: %s", mustJSON(m))
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
