package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelsEndpointExposesUpstreamMetadata(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	m := ModelInfo{ModelId: "claude-opus-5", ModelName: "Claude Opus 5", InputTypes: []string{"TEXT", "IMAGE"}, RateMultiplier: 2.2}
	m.TokenLimits = &struct {
		MaxInputTokens  int `json:"maxInputTokens"`
		MaxOutputTokens int `json:"maxOutputTokens"`
	}{1000000, 64000}
	h.cachedModels = []ModelInfo{m}
	h.modelsCacheTime = time.Now().Unix()

	rec := httptest.NewRecorder()
	h.handleModels(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var body struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var found map[string]interface{}
	for _, d := range body.Data {
		if d["id"] == "claude-opus-5" {
			found = d
		}
	}
	if found == nil {
		t.Fatalf("model missing: %s", rec.Body.String())
	}
	if found["context_length"] != float64(1000000) || found["max_output_tokens"] != float64(64000) || found["display_name"] != "Claude Opus 5" {
		t.Fatalf("metadata missing: %#v", found)
	}
}

func TestEnsureModelsFreshServesStaleCacheWithoutBlocking(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	h.cachedModels = []ModelInfo{{ModelId: "old-model"}}
	h.modelsCacheTime = time.Now().Add(-2 * modelsCacheTTL).Unix()
	got := h.ensureModelsFresh()
	if len(got) != 1 || got[0].ModelId != "old-model" {
		t.Fatalf("stale cache should be served immediately, got %#v", got)
	}
	// The background refresh holds the lock while running; wait for it.
	h.modelsRefreshMu.Lock()
	h.modelsRefreshMu.Unlock()
}
