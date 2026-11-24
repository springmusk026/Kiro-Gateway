package proxy

import (
	"encoding/base64"
	"strings"
)

// Reasoning output on each API.
//
// Anthropic: thinking blocks carry a `signature`, which Anthropic clients and
// SDKs expect (and some require before replaying a turn). Kiro does not hand
// out a signature the gateway can pass on, so the value is an opaque, constant
// marker: it is NOT a cryptographic signature and cannot be verified. Clients
// send it back with thinking blocks in history, where the gateway accepts and
// drops thinking / redacted_thinking blocks without checking it.
//
// Responses: reasoning is returned as `reasoning` output items with a summary
// (the reasoning text), streamed with the official reasoning_summary_* events,
// before the message item.
//
// OpenAI Chat and Responses: usage reports reasoning tokens, estimated from
// the reasoning text with the gateway's token estimator.

// thinkingSignature is the opaque signature attached to thinking blocks.
var thinkingSignature = base64.StdEncoding.EncodeToString([]byte("kiro-gateway:thinking:unverified:v1"))

// reasoningTokens estimates the tokens spent on reasoning text.
func reasoningTokens(reasoning string) int {
	if strings.TrimSpace(reasoning) == "" {
		return 0
	}
	return estimateApproxTokens(reasoning)
}

// ---- OpenAI Chat ----

// withReasoningTokenDetails adds completion_tokens_details.reasoning_tokens
// to an OpenAI usage map when reasoning was returned.
func withReasoningTokenDetails(usage map[string]interface{}, reasoning string) map[string]interface{} {
	if usage == nil {
		return usage
	}
	if n := reasoningTokens(reasoning); n > 0 {
		usage["completion_tokens_details"] = map[string]int{"reasoning_tokens": n}
	}
	return usage
}

// ---- Responses ----

// ResponseSummaryPart is one summary entry of a reasoning output item.
type ResponseSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// newReasoningOutputItem builds a completed reasoning item.
func newReasoningOutputItem(id, text string) ResponseOutputItem {
	return ResponseOutputItem{
		ID:      id,
		Type:    "reasoning",
		Summary: []ResponseSummaryPart{{Type: "summary_text", Text: text}},
	}
}

// applyReasoningToResponse puts the reasoning items first in the output and
// reports the reasoning token estimate in usage.
func applyReasoningToResponse(obj *ResponsesObject, items []ResponseOutputItem, reasoning string) {
	if obj == nil {
		return
	}
	if len(items) > 0 {
		out := make([]ResponseOutputItem, 0, len(items)+len(obj.Output))
		out = append(out, items...)
		obj.Output = append(out, obj.Output...)
	}
	if n := reasoningTokens(reasoning); n > 0 {
		obj.Usage.OutputTokensDetails = &ResponsesOutputTokensDetails{ReasoningTokens: n}
	}
}

// responsesReasoningStream emits a reasoning item with the official event
// sequence through the stream's send function (which assigns
// sequence_number):
//
//	response.output_item.added            {item:{type:"reasoning", summary:[]}}
//	response.reasoning_summary_part.added
//	response.reasoning_summary_text.delta  (one per chunk)
//	response.reasoning_summary_text.done
//	response.reasoning_summary_part.done
//	response.output_item.done              {item:{…, summary:[{summary_text}]}}
type responsesReasoningStream struct {
	send func(event string, payload map[string]interface{})

	open        bool
	itemID      string
	outputIndex int
	text        strings.Builder
}

// delta starts the item if needed and streams a chunk.
func (r *responsesReasoningStream) delta(outputIndex int, chunk string) {
	if chunk == "" {
		return
	}
	if !r.open {
		r.open = true
		r.itemID = generateOutputItemID("rs")
		r.outputIndex = outputIndex
		r.text.Reset()
		r.send("response.output_item.added", map[string]interface{}{
			"type":         "response.output_item.added",
			"output_index": r.outputIndex,
			"item": map[string]interface{}{
				"id":      r.itemID,
				"type":    "reasoning",
				"summary": []interface{}{},
			},
		})
		r.send("response.reasoning_summary_part.added", map[string]interface{}{
			"type":          "response.reasoning_summary_part.added",
			"item_id":       r.itemID,
			"output_index":  r.outputIndex,
			"summary_index": 0,
			"part":          ResponseSummaryPart{Type: "summary_text", Text: ""},
		})
	}
	r.text.WriteString(chunk)
	r.send("response.reasoning_summary_text.delta", map[string]interface{}{
		"type":          "response.reasoning_summary_text.delta",
		"item_id":       r.itemID,
		"output_index":  r.outputIndex,
		"summary_index": 0,
		"delta":         chunk,
	})
}

// close finishes an open item and returns it; ok is false when none was open.
func (r *responsesReasoningStream) close() (ResponseOutputItem, bool) {
	if !r.open {
		return ResponseOutputItem{}, false
	}
	r.open = false
	text := r.text.String()
	r.send("response.reasoning_summary_text.done", map[string]interface{}{
		"type":          "response.reasoning_summary_text.done",
		"item_id":       r.itemID,
		"output_index":  r.outputIndex,
		"summary_index": 0,
		"text":          text,
	})
	r.send("response.reasoning_summary_part.done", map[string]interface{}{
		"type":          "response.reasoning_summary_part.done",
		"item_id":       r.itemID,
		"output_index":  r.outputIndex,
		"summary_index": 0,
		"part":          ResponseSummaryPart{Type: "summary_text", Text: text},
	})
	item := newReasoningOutputItem(r.itemID, text)
	r.send("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": r.outputIndex,
		"item":         item,
	})
	return item, true
}
