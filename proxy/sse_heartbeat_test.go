package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withHeartbeatInterval(t *testing.T, d time.Duration) {
	t.Helper()
	old := sseHeartbeatInterval
	sseHeartbeatInterval = d
	t.Cleanup(func() { sseHeartbeatInterval = old })
}

func slowUpstream(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "late answer"}))
		_, _ = w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "end_turn"}))
	}))
}

func TestClaudeStreamSendsPingWhileWaiting(t *testing.T) {
	withHeartbeatInterval(t, 20*time.Millisecond)
	server := slowUpstream(t, 150*time.Millisecond)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,"stream":true,
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: ping\ndata: {\"type\":\"ping\"}") {
		t.Fatalf("expected ping events, got %s", body)
	}
	if !strings.Contains(body, "late answer") || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("expected a complete stream, got %s", body)
	}
}

func TestOpenAIStreamSendsKeepAliveComments(t *testing.T) {
	withHeartbeatInterval(t, 20*time.Millisecond)
	server := slowUpstream(t, 150*time.Millisecond)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-sonnet-4.5","stream":true,
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleOpenAIChat(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") || !strings.Contains(body, "late answer") {
		t.Fatalf("expected keep-alive comments and content, got %s", body)
	}
}

// Once a heartbeat has committed the 200 status, an upstream failure must be
// reported in-stream with its classified type.
func TestClaudeStreamErrorAfterHeartbeatIsSSE(t *testing.T) {
	withHeartbeatInterval(t, 20*time.Millisecond)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Input is too long.","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`))
	}))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,"stream":true,
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, "event: error") || !strings.Contains(body, `"type":"invalid_request_error"`) || !strings.Contains(body, "prompt is too long: ") {
		t.Fatalf("expected classified SSE error, got %s", body)
	}
}
