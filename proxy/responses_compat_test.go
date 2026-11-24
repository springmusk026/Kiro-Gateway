package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sseEvent struct {
	name string
	data map[string]interface{}
}

func parseSSEEvents(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(body, "\n\n") {
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				raw := strings.TrimPrefix(line, "data: ")
				if raw == "[DONE]" {
					ev.name = "[DONE]"
					continue
				}
				if err := json.Unmarshal([]byte(raw), &ev.data); err != nil {
					t.Fatalf("bad event data %q: %v", raw, err)
				}
			}
		}
		if ev.name != "" {
			out = append(out, ev)
		}
	}
	return out
}

func toolAndTextUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "checking"}))
		_, _ = w.Write(awsEventStreamFrame(t, "toolUseEvent", map[string]interface{}{
			"toolUseId": "call_1", "name": "get_weather", "input": `{"city":"Paris"}`, "stop": true,
		}))
		_, _ = w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "tool_use"}))
	}))
}

func TestResponsesStreamEventFidelity(t *testing.T) {
	server := toolAndTextUpstream(t)
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5","input":"weather?","stream":true,"store":false,
		"temperature":0.3,"max_output_tokens":500,
		"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]}`))
	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, req)

	events := parseSSEEvents(t, rec.Body.String())
	counts := map[string]int{}
	for i, ev := range events {
		counts[ev.name]++
		if ev.name == "[DONE]" {
			t.Fatalf("Responses stream must not end with [DONE]")
		}
		if got, ok := ev.data["sequence_number"].(float64); !ok || int(got) != i {
			t.Fatalf("event %d (%s) sequence_number = %v", i, ev.name, ev.data["sequence_number"])
		}
	}
	for _, name := range []string{
		"response.created", "response.in_progress", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done",
		"response.function_call_arguments.delta", "response.function_call_arguments.done",
		"response.output_item.done", "response.completed",
	} {
		if counts[name] == 0 {
			t.Fatalf("missing %s in %s", name, rec.Body.String())
		}
	}
	if counts["response.in_progress"] != 1 {
		t.Fatalf("in_progress sent %d times", counts["response.in_progress"])
	}

	last := events[len(events)-1]
	if last.name != "response.completed" {
		t.Fatalf("last event = %s", last.name)
	}
	resp := last.data["response"].(map[string]interface{})
	if resp["truncation"] != "disabled" || resp["temperature"] != 0.3 || resp["max_output_tokens"] != float64(500) {
		t.Fatalf("missing request echo: %v", resp)
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("error must be present (null): %v", resp)
	}
	if tools, ok := resp["tools"].([]interface{}); !ok || len(tools) != 1 {
		t.Fatalf("tools echo = %v", resp["tools"])
	}
	usage := resp["usage"].(map[string]interface{})
	if _, ok := usage["output_tokens_details"].(map[string]interface{})["reasoning_tokens"]; !ok {
		t.Fatalf("usage.output_tokens_details missing: %v", usage)
	}
	if _, ok := usage["input_tokens_details"].(map[string]interface{})["cached_tokens"]; !ok {
		t.Fatalf("usage.input_tokens_details missing: %v", usage)
	}
	for _, item := range resp["output"].([]interface{}) {
		m := item.(map[string]interface{})
		if m["type"] != "message" {
			continue
		}
		part := m["content"].([]interface{})[0].(map[string]interface{})
		if _, ok := part["annotations"].([]interface{}); !ok {
			t.Fatalf("output_text part without annotations: %v", part)
		}
	}
}

func TestResponsesStreamFailedCarriesFullObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Input is too long.","reason":"CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`))
	}))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5","input":"hi","stream":true,"store":false}`))
	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, req)

	events := parseSSEEvents(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "response.failed" {
		t.Fatalf("last event = %s body=%s", last.name, rec.Body.String())
	}
	resp := last.data["response"].(map[string]interface{})
	for _, key := range []string{"id", "object", "created_at", "model", "status", "output", "error"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("failed response missing %s: %v", key, resp)
		}
	}
	errObj := resp["error"].(map[string]interface{})
	if errObj["code"] != "context_length_exceeded" || resp["status"] != "failed" {
		t.Fatalf("unexpected failure %v", resp)
	}
}
