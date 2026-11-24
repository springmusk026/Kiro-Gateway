package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyUpstreamError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		anthropic  string
		openai     string
		code       string
		client     bool
		retryAfter time.Duration
	}{
		{"content length", errors.New(`HTTP 400 from kiro: {"message":"Input is too long.","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`), 400, "invalid_request_error", "invalid_request_error", "context_length_exceeded", true, 0},
		{"invalid model", errors.New(`HTTP 400 from kiro: {"reason":"INVALID_MODEL_ID"}`), 404, "not_found_error", "invalid_request_error", "model_not_found", true, 0},
		{"throttled", errors.New(`HTTP 429 from kiro (retry-after=7): {"message":"Too many requests"}`), 429, "rate_limit_error", "rate_limit_exceeded", "rate_limit_exceeded", false, 7 * time.Second},
		{"quota", errors.New(`HTTP 429 from kiro: {"reason":"MONTHLY_REQUEST_COUNT"}`), 429, "rate_limit_error", "insufficient_quota", "insufficient_quota", false, 0},
		{"bad request", errors.New(`HTTP 400 from kiro: Improperly formed request`), 400, "invalid_request_error", "invalid_request_error", "invalid_request_error", true, 0},
		{"overloaded", errors.New(`HTTP 503 from kiro: ServiceUnavailableException`), 529, "overloaded_error", "server_error", "server_overloaded", false, 0},
		{"budget", fmt.Errorf("wrap: %w", errUpstreamBudgetExhausted), 529, "overloaded_error", "server_error", "server_overloaded", false, 0},
		{"generic", errors.New("connection reset by peer"), 500, "api_error", "server_error", "server_error", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := classifyUpstreamError(tc.err)
			if info.Status != tc.status || info.AnthropicType != tc.anthropic || info.OpenAIType != tc.openai || info.Code != tc.code || info.ClientError != tc.client || info.RetryAfter != tc.retryAfter {
				t.Fatalf("got %+v", info)
			}
		})
	}
	if got := classifyUpstreamError(errors.New("HTTP 503 overloaded")).openAIStatus(); got != 503 {
		t.Fatalf("openai status for overloaded = %d", got)
	}
}

func TestClaudePromptTooLongMessage(t *testing.T) {
	info := classifyUpstreamError(errors.New("Input is too long."))
	msg := info.claudeMessage(10, "claude-sonnet-4.5")
	if !strings.HasPrefix(msg, "prompt is too long: ") || !strings.Contains(msg, "tokens >") {
		t.Fatalf("unexpected message %q", msg)
	}
}

// A request the upstream rejects as too long must surface as a 400 that Claude
// Code recognizes, and must not be retried on other accounts.
func TestClaudeNonStreamPromptTooLongIs400AndNotRetried(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Input is too long.","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`))
	}))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct{ Type, Message string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Type != "invalid_request_error" || !strings.HasPrefix(body.Error.Message, "prompt is too long: ") {
		t.Fatalf("unexpected error body %s", rec.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected a single upstream call, got %d", hits.Load())
	}
}

func TestOpenAINonStreamThrottleIs429WithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Too many requests"}`))
	}))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-sonnet-4.5","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleOpenAIChat(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "12" {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	var body struct {
		Error map[string]interface{} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error["code"] != "rate_limit_exceeded" {
		t.Fatalf("unexpected error body %s", rec.Body.String())
	}
	if _, ok := body.Error["param"]; !ok {
		t.Fatalf("error object must carry param: %s", rec.Body.String())
	}
}
