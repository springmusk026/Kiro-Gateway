package proxy

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestOpenAIMaxCompletionTokensAlias(t *testing.T) {
	req := &OpenAIRequest{
		Model:               "claude-sonnet-4.5",
		Messages:            []OpenAIMessage{{Role: "user", Content: "hi"}},
		MaxCompletionTokens: 321,
	}
	payload := OpenAIToKiro(req, false)
	if payload.InferenceConfig == nil || payload.InferenceConfig.MaxTokens != 321 {
		t.Fatalf("inference config = %+v", payload.InferenceConfig)
	}
}

func TestOpenAIRejectsMultipleChoices(t *testing.T) {
	h := &Handler{}
	rec := runChat(t, h, `{"model":"claude-sonnet-4.5","n":2,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOpenAIStreamIncludeUsageSeparateChunk(t *testing.T) {
	server := textUpstream(t, "hello there")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := runChat(t, h, `{"model":"claude-sonnet-4.5","stream":true,"stream_options":{"include_usage":true},
		"messages":[{"role":"user","content":"hi"}]}`)
	chunks := sseDataChunks(t, rec.Body.String())
	last := chunks[len(chunks)-1]
	if choices, _ := last["choices"].([]interface{}); len(choices) != 0 || last["usage"] == nil {
		t.Fatalf("last chunk must be usage-only: %v", last)
	}
	finish := chunks[len(chunks)-2]
	if _, ok := finish["usage"]; ok {
		t.Fatalf("finish chunk must not carry usage when include_usage is set: %v", finish)
	}

	// Without include_usage the finish chunk keeps usage for compatibility.
	rec = runChat(t, h, `{"model":"claude-sonnet-4.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	chunks = sseDataChunks(t, rec.Body.String())
	if chunks[len(chunks)-1]["usage"] == nil {
		t.Fatalf("finish chunk must carry usage: %v", chunks[len(chunks)-1])
	}
}

func TestOpenAINonStreamKeepsTextWithToolCalls(t *testing.T) {
	resp := KiroToOpenAIResponseWithReasoning("Let me check.", "", []KiroToolUse{{ToolUseID: "c1", Name: "f", Input: map[string]interface{}{}}},
		1, 1, "m", "reasoning_content", "tool_use", promptCacheUsage{}, false)
	raw, _ := json.Marshal(resp)
	var decoded struct {
		Choices []struct {
			Message map[string]interface{} `json:"message"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(raw, &decoded)
	msg := decoded.Choices[0].Message
	if msg["content"] != "Let me check." || msg["tool_calls"] == nil {
		t.Fatalf("message = %v", msg)
	}
}
