package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func TestDisabledModelIsHiddenAndRejected(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	h.cachedModels = []ModelInfo{{ModelId: "claude-opus-4.7"}, {ModelId: "claude-sonnet-4.5"}}
	h.modelsCacheTime = time.Now().Unix()

	if err := config.SetModelEnabled("Claude-Opus-4.7", false); err != nil {
		t.Fatal(err)
	}
	if !config.IsModelDisabled("claude-opus-4.7") {
		t.Fatal("model should be disabled (case-insensitive)")
	}

	rec := httptest.NewRecorder()
	h.handleModels(rec, httptest.NewRequest("GET", "/v1/models", nil))
	var body struct {
		Data []struct{ ID string } `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	for _, m := range body.Data {
		if strings.HasPrefix(m.ID, "claude-opus-4.7") {
			t.Fatalf("disabled model %s still listed", m.ID)
		}
	}
	found := false
	for _, m := range body.Data {
		if m.ID == "claude-sonnet-4.5" {
			found = true
		}
	}
	if !found {
		t.Fatal("enabled model missing")
	}

	for _, tc := range []struct{ path, body string }{
		{"/v1/messages", `{"model":"claude-opus-4.7-thinking","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/chat/completions", `{"model":"claude-opus-4.7","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"claude-opus-4.7","input":"hi"}`},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "disabled") {
			t.Fatalf("%s: want 400 disabled, got %d %s", tc.path, rec.Code, rec.Body.String())
		}
	}

	config.SetModelEnabled("claude-opus-4.7", true)
	if config.IsModelDisabled("claude-opus-4.7") {
		t.Fatal("re-enable failed")
	}
}
