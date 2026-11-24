package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The alias below resolves to a different Kiro id; every response shape must
// still report the name the client sent.
const echoTestModel = "claude-sonnet-4-5-20250929"

func TestEchoModelAnthropic(t *testing.T) {
	if got := MapModel(echoTestModel); got == echoTestModel {
		t.Fatalf("test needs a model name that resolves to another id, got %q", got)
	}
	server := textUpstream(t, "hello")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	for _, stream := range []bool{false, true} {
		body := `{"model":"` + echoTestModel + `","max_tokens":100,"messages":[{"role":"user","content":"hi"}],"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `}`
		rec := httptest.NewRecorder()
		h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("stream=%v status %d: %s", stream, rec.Code, rec.Body.String())
		}
		if stream {
			found := false
			for _, ev := range parseSSEEvents(t, rec.Body.String()) {
				if ev.name == "message_start" {
					msg := ev.data["message"].(map[string]interface{})
					if msg["model"] != echoTestModel {
						t.Fatalf("message_start model = %v", msg["model"])
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("no message_start: %s", rec.Body.String())
			}
			continue
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["model"] != echoTestModel {
			t.Fatalf("non-stream model = %v", resp["model"])
		}
	}
}

func TestEchoModelOpenAIChat(t *testing.T) {
	server := textUpstream(t, "hello")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	for _, stream := range []string{"false", "true"} {
		body := `{"model":"` + echoTestModel + `","messages":[{"role":"user","content":"hi"}],"stream":` + stream + `}`
		rec := httptest.NewRecorder()
		h.handleOpenAIChat(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if stream == "true" {
			chunks := 0
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				raw, ok := strings.CutPrefix(line, "data: ")
				if !ok || raw == "[DONE]" {
					continue
				}
				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
					t.Fatalf("bad chunk %q", raw)
				}
				if chunk["model"] != echoTestModel {
					t.Fatalf("chunk model = %v", chunk["model"])
				}
				chunks++
			}
			if chunks == 0 {
				t.Fatal("no chunks")
			}
			continue
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["model"] != echoTestModel {
			t.Fatalf("non-stream model = %v", resp["model"])
		}
	}
}

func TestEchoModelResponses(t *testing.T) {
	server := textUpstream(t, "hello")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	// Non-stream, stored: the stored object keeps the requested name too.
	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"`+echoTestModel+`","input":"hi"}`)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp ResponsesObject
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Model != echoTestModel {
		t.Fatalf("model = %q", resp.Model)
	}
	stored, err := loadResponse(resp.ID)
	if err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if stored.Model != echoTestModel {
		t.Fatalf("stored model = %q", stored.Model)
	}

	rec = httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"`+echoTestModel+`","input":"hi","stream":true}`)))
	seen := 0
	for _, ev := range parseSSEEvents(t, rec.Body.String()) {
		if r, ok := ev.data["response"].(map[string]interface{}); ok {
			if r["model"] != echoTestModel {
				t.Fatalf("%s model = %v", ev.name, r["model"])
			}
			seen++
		}
	}
	if seen < 3 {
		t.Fatalf("expected created/in_progress/completed events, got %d", seen)
	}
}
