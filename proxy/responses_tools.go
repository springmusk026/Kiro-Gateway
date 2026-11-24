package proxy

import "encoding/json"

// responsesToolCallShape describes how one Kiro tool use should surface on the
// Responses API. Tools that arrived as Codex freeform (grammar) tools must come
// back as custom_tool_call carrying a raw input string; everything else is a
// function_call carrying JSON arguments. Codex abandons a freeform call that
// returns as a plain function_call, so the distinction decides whether the tool
// actually runs.
type responsesToolCallShape struct {
	Type      string
	Arguments string // JSON object, for function_call
	Input     string // raw payload, for custom_tool_call
	Namespace string // Codex namespace the tool was declared in, if any
}

func shapeResponsesToolCall(tu KiroToolUse, customTools map[string]bool, namespaces ...map[string]string) responsesToolCallShape {
	ns := ""
	for _, m := range namespaces {
		if v := m[tu.Name]; v != "" {
			ns = v
		}
	}
	if customTools[tu.Name] {
		return responsesToolCallShape{
			Type:  responsesCustomToolCallType,
			Input: extractCustomToolInput(tu.Input),
		}
	}
	// A nil map marshals to "null", which Codex fails to deserialize as an
	// arguments object and then silently discards the whole call. Arguments must
	// always be a JSON object.
	args := []byte("{}")
	if len(tu.Input) > 0 {
		if encoded, err := json.Marshal(tu.Input); err == nil && len(encoded) > 0 {
			args = encoded
		}
	}
	return responsesToolCallShape{Type: "function_call", Arguments: string(args), Namespace: ns}
}

// outputItem renders the shape as an output item for the SSE stream. withPayload
// distinguishes the opening event, which announces an empty payload, from the
// closing one that carries the full call.
func (s responsesToolCallShape) outputItem(id string, tu KiroToolUse, status string, withPayload bool) map[string]interface{} {
	item := map[string]interface{}{
		"id":      id,
		"type":    s.Type,
		"status":  status,
		"call_id": tu.ToolUseID,
		"name":    tu.Name,
	}
	if s.Namespace != "" {
		item["namespace"] = s.Namespace
	}
	key, payload := "arguments", s.Arguments
	if s.Type == responsesCustomToolCallType {
		key, payload = "input", s.Input
	}
	if withPayload {
		item[key] = payload
	} else {
		item[key] = ""
	}
	return item
}

// deltaEvent names the incremental event carrying this call's payload. Codex
// ignores both variants and reads tool calls only from
// response.output_item.done, but other Responses API clients do consume them.
func (s responsesToolCallShape) deltaEvent() string {
	if s.Type == responsesCustomToolCallType {
		return "response.custom_tool_call_input.delta"
	}
	return "response.function_call_arguments.delta"
}

func (s responsesToolCallShape) deltaPayload(itemID string, outputIndex int) map[string]interface{} {
	payload := s.Arguments
	if s.Type == responsesCustomToolCallType {
		payload = s.Input
	}
	return map[string]interface{}{
		"type":         s.deltaEvent(),
		"item_id":      itemID,
		"output_index": outputIndex,
		"delta":        payload,
	}
}

// doneEvent names the event that closes this call's payload stream.
func (s responsesToolCallShape) doneEvent() string {
	if s.Type == responsesCustomToolCallType {
		return "response.custom_tool_call_input.done"
	}
	return "response.function_call_arguments.done"
}

func (s responsesToolCallShape) donePayload(itemID string, outputIndex int, name string) map[string]interface{} {
	out := map[string]interface{}{
		"type":         s.doneEvent(),
		"item_id":      itemID,
		"output_index": outputIndex,
	}
	if s.Type == responsesCustomToolCallType {
		out["input"] = s.Input
	} else {
		out["arguments"] = s.Arguments
		out["name"] = name
	}
	return out
}

// extractCustomToolInput recovers the raw freeform payload the model emitted.
// convertOpenAITools advertises freeform tools as a single string argument, so
// the payload normally arrives under that key; the fallbacks keep a model that
// ignored the advertised schema from producing an empty call.
func extractCustomToolInput(input map[string]interface{}) string {
	if raw, ok := input[customToolInputKey].(string); ok {
		return raw
	}
	if len(input) == 1 {
		for _, v := range input {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	if len(input) == 0 {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}

// wrapCustomToolArguments re-wraps a replayed freeform payload in the
// single-argument schema convertOpenAITools advertises, so a custom_tool_call
// coming back as conversation history still matches its tool definition.
func wrapCustomToolArguments(input string) string {
	b, err := json.Marshal(map[string]string{customToolInputKey: input})
	if err != nil {
		return "{}"
	}
	return string(b)
}
