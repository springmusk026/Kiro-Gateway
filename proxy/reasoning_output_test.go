package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// reasoningUpstream streams reasoning, then answer text.
func reasoningUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(awsEventStreamFrame(t, "reasoningContentEvent", map[string]interface{}{"text": "Let me think. "}))
		_, _ = w.Write(awsEventStreamFrame(t, "reasoningContentEvent", map[string]interface{}{"text": "Two plus two is four."}))
		_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "The answer is 4."}))
		_, _ = w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "end_turn"}))
	}))
}

func TestResponsesStreamReasoningItems(t *testing.T) {
	server := reasoningUpstream(t)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5","input":"2+2?","stream":true,
		"reasoning":{"effort":"low"},"include":["reasoning.encrypted_content"]}`)))
	events := parseSSEEvents(t, rec.Body.String())

	var names []string
	for i, ev := range events {
		names = append(names, ev.name)
		if seq, _ := ev.data["sequence_number"].(float64); int(seq) != i {
			t.Fatalf("event %d (%s) has sequence_number %v", i, ev.name, ev.data["sequence_number"])
		}
	}
	want := []string{
		"response.created", "response.in_progress",
		"response.output_item.added", "response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta", "response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done", "response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added", "response.content_part.added", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done", "response.output_item.done",
		"response.completed",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("event order:\n got %v\nwant %v", names, want)
	}

	added := events[2].data["item"].(map[string]interface{})
	if added["type"] != "reasoning" || added["summary"] == nil || len(added["summary"].([]interface{})) != 0 || events[2].data["output_index"].(float64) != 0 {
		t.Fatalf("reasoning item added = %#v", events[2].data)
	}
	if done := events[6].data; done["text"] != "Let me think. Two plus two is four." {
		t.Fatalf("summary text done = %#v", done)
	}
	doneItem := events[8].data["item"].(map[string]interface{})
	summary := doneItem["summary"].([]interface{})[0].(map[string]interface{})
	if summary["type"] != "summary_text" || summary["text"] != "Let me think. Two plus two is four." {
		t.Fatalf("reasoning item done = %#v", doneItem)
	}
	if events[9].data["output_index"].(float64) != 1 {
		t.Fatalf("message must follow the reasoning item: %#v", events[9].data)
	}

	final := events[len(events)-1].data["response"].(map[string]interface{})
	output := final["output"].([]interface{})
	if output[0].(map[string]interface{})["type"] != "reasoning" || output[0].(map[string]interface{})["id"] != doneItem["id"] ||
		output[1].(map[string]interface{})["type"] != "message" {
		t.Fatalf("final output = %#v", output)
	}
	details := final["usage"].(map[string]interface{})["output_tokens_details"].(map[string]interface{})
	if details["reasoning_tokens"].(float64) <= 0 {
		t.Fatalf("reasoning_tokens = %v", details["reasoning_tokens"])
	}

	stored, err := loadResponse(final["id"].(string))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored.Output) < 2 || stored.Output[0].Type != "reasoning" || stored.Output[0].Summary[0].Text == "" {
		t.Fatalf("stored output = %+v", stored.Output)
	}
}

func TestResponsesNonStreamReasoningItem(t *testing.T) {
	server := reasoningUpstream(t)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5","input":"2+2?","reasoning":{"effort":"medium"}}`)))
	var resp ResponsesObject
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(resp.Output) != 2 || resp.Output[0].Type != "reasoning" || resp.Output[1].Type != "message" {
		t.Fatalf("output = %+v", resp.Output)
	}
	if resp.Output[0].Summary[0].Text != "Let me think. Two plus two is four." {
		t.Fatalf("summary = %+v", resp.Output[0].Summary)
	}
	if resp.Usage.OutputTokensDetails == nil || resp.Usage.OutputTokensDetails.ReasoningTokens <= 0 {
		t.Fatalf("usage = %+v", resp.Usage)
	}

	// Without thinking there is no reasoning item.
	rec = httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5","input":"2+2?"}`)))
	resp = ResponsesObject{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	for _, it := range resp.Output {
		if it.Type == "reasoning" {
			t.Fatalf("unexpected reasoning item without thinking: %+v", resp.Output)
		}
	}
	if resp.Usage.OutputTokensDetails == nil || resp.Usage.OutputTokensDetails.ReasoningTokens != 0 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestAnthropicThinkingSignature(t *testing.T) {
	server := reasoningUpstream(t)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)
	body := `{"model":"claude-sonnet-4.5","max_tokens":4000,"thinking":{"type":"enabled","budget_tokens":2000},
		"messages":[{"role":"user","content":"2+2?"}]`

	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body+`,"stream":true}`)))
	events := parseSSEEvents(t, rec.Body.String())
	thinkingIndex := -1.0
	sawSignature := false
	for i, ev := range events {
		switch ev.name {
		case "content_block_start":
			cb := ev.data["content_block"].(map[string]interface{})
			if cb["type"] == "thinking" {
				thinkingIndex = ev.data["index"].(float64)
				if sig, ok := cb["signature"]; !ok || sig != "" {
					t.Fatalf("thinking content_block_start must carry signature \"\": %#v", cb)
				}
			}
		case "content_block_delta":
			delta := ev.data["delta"].(map[string]interface{})
			if delta["type"] == "signature_delta" {
				if ev.data["index"].(float64) != thinkingIndex || delta["signature"] != thinkingSignature {
					t.Fatalf("bad signature_delta %#v", ev.data)
				}
				next := events[i+1]
				if next.name != "content_block_stop" || next.data["index"].(float64) != thinkingIndex {
					t.Fatalf("signature_delta must directly precede the thinking block's stop, got %s", next.name)
				}
				sawSignature = true
			}
		}
	}
	if thinkingIndex < 0 || !sawSignature {
		t.Fatalf("no thinking block or signature: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body+`}`)))
	var resp ClaudeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Content) == 0 || resp.Content[0].Type != "thinking" || resp.Content[0].Signature != thinkingSignature || resp.Content[0].Thinking == "" {
		t.Fatalf("content = %+v", resp.Content)
	}
}

func TestOpenAIChatReasoningTokens(t *testing.T) {
	server := reasoningUpstream(t)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := httptest.NewRecorder()
	h.handleOpenAIChat(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-sonnet-4.5","reasoning_effort":"low","messages":[{"role":"user","content":"2+2?"}]}`)))
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	usage := resp["usage"].(map[string]interface{})
	details, ok := usage["completion_tokens_details"].(map[string]interface{})
	if !ok || details["reasoning_tokens"].(float64) <= 0 {
		t.Fatalf("usage = %#v", usage)
	}

	rec = httptest.NewRecorder()
	h.handleOpenAIChat(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-sonnet-4.5","reasoning_effort":"low","stream":true,"stream_options":{"include_usage":true},
		"messages":[{"role":"user","content":"2+2?"}]}`)))
	found := false
	for _, chunk := range sseDataChunks(t, rec.Body.String()) {
		if u, ok := chunk["usage"].(map[string]interface{}); ok {
			if d, ok := u["completion_tokens_details"].(map[string]interface{}); ok && d["reasoning_tokens"].(float64) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("stream usage has no reasoning_tokens: %s", rec.Body.String())
	}
}
