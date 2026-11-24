package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStopSequenceMatcherAcrossChunks(t *testing.T) {
	m := newStopSequenceMatcher([]string{"STOP", "END"})
	var out strings.Builder
	for _, chunk := range []string{"hello S", "T", "ill going ST", "OP and more"} {
		out.WriteString(m.Push(chunk))
	}
	if !m.Done() || m.Matched() != "STOP" {
		t.Fatalf("done=%v matched=%q", m.Done(), m.Matched())
	}
	if out.String() != "hello STill going " {
		t.Fatalf("output = %q", out.String())
	}
	if m.Push("ignored") != "" || m.Flush() != "" {
		t.Fatal("matcher must emit nothing after a match")
	}
}

func TestStopSequenceMatcherHoldsOnlyPossiblePrefix(t *testing.T) {
	m := newStopSequenceMatcher([]string{"</answer>"})
	if got := m.Push("plain text"); got != "plain text" {
		t.Fatalf("text without a possible prefix must pass through, got %q", got)
	}
	if got := m.Push(" then </ans"); got != " then " {
		t.Fatalf("got %q", got)
	}
	if got := m.Flush(); got != "</ans" {
		t.Fatalf("flush = %q", got)
	}
	var nilMatcher *stopSequenceMatcher
	if nilMatcher.Push("x") != "x" || nilMatcher.Done() {
		t.Fatal("nil matcher must pass text through")
	}
}

func TestOpenAIStopParameterShapes(t *testing.T) {
	if got := openAIStopSequences("x"); len(got) != 1 || got[0] != "x" {
		t.Fatalf("string: %v", got)
	}
	if got := openAIStopSequences([]interface{}{"a", "b"}); len(got) != 2 {
		t.Fatalf("array: %v", got)
	}
}

func TestClaudeStreamStopSequence(t *testing.T) {
	server := textUpstream(t, "The answer is 4", "2. DONE and then", " more text that must not appear")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,"stream":true,"stop_sequences":["DONE"],
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, req)

	body := rec.Body.String()
	var text strings.Builder
	var delta map[string]interface{}
	for _, ev := range parseSSEEvents(t, body) {
		switch ev.name {
		case "content_block_delta":
			d := ev.data["delta"].(map[string]interface{})
			if s, ok := d["text"].(string); ok {
				text.WriteString(s)
			}
		case "message_delta":
			delta = ev.data["delta"].(map[string]interface{})
		}
	}
	if text.String() != "The answer is 42. " {
		t.Fatalf("text = %q", text.String())
	}
	if delta["stop_reason"] != "stop_sequence" || delta["stop_sequence"] != "DONE" {
		t.Fatalf("message_delta = %v", delta)
	}
}

func TestOpenAINonStreamStop(t *testing.T) {
	server := textUpstream(t, "one two ", "three four")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := runChat(t, h, `{"model":"claude-sonnet-4.5","stop":["three"],"messages":[{"role":"user","content":"count"}]}`)
	var resp struct {
		Choices []struct {
			Message      map[string]interface{} `json:"message"`
			FinishReason string                 `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Choices) != 1 {
		t.Fatalf("decode: %v %s", err, rec.Body.String())
	}
	if resp.Choices[0].Message["content"] != "one two " || resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected %s", rec.Body.String())
	}
}

func TestClaudeNonStreamStopSequence(t *testing.T) {
	server := textUpstream(t, "alpha beta ", "gamma")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":100,"stop_sequences":["beta"],
		"messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, req)

	var resp ClaudeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v %s", err, rec.Body.String())
	}
	if resp.StopReason != "stop_sequence" || resp.StopSequence == nil || *resp.StopSequence != "beta" {
		t.Fatalf("unexpected %s", rec.Body.String())
	}
	if len(resp.Content) == 0 || strings.Contains(resp.Content[0].Text, "gamma") || strings.Contains(resp.Content[0].Text, "beta") {
		t.Fatalf("content not cut: %s", rec.Body.String())
	}
}
