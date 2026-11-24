package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func textUpstream(t *testing.T, chunks ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for _, c := range chunks {
			_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": c}))
		}
		_, _ = w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "end_turn"}))
	}))
}

// sseDataChunks returns the decoded JSON of every "data:" line except [DONE].
func sseDataChunks(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatalf("bad chunk %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func runChat(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleOpenAIChat(rec, req)
	return rec
}

func TestOpenAIStreamFirstChunkCarriesRole(t *testing.T) {
	server := textUpstream(t, strings.Repeat("hello world ", 10))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := runChat(t, h, `{"model":"claude-sonnet-4.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	chunks := sseDataChunks(t, rec.Body.String())
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %s", rec.Body.String())
	}
	first := chunks[0]["choices"].([]interface{})[0].(map[string]interface{})
	delta := first["delta"].(map[string]interface{})
	if delta["role"] != "assistant" || delta["content"] != "" {
		t.Fatalf("first delta = %v", delta)
	}
	if _, ok := first["logprobs"]; !ok {
		t.Fatalf("choice must carry logprobs: %v", first)
	}
	created := chunks[0]["created"]
	for _, c := range chunks {
		if c["created"] != created {
			t.Fatalf("created changed within a stream: %v vs %v", c["created"], created)
		}
	}
}
