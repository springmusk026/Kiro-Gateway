package proxy

import "encoding/json"

type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`
	Instructions       string            `json:"instructions,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Reasoning          *OpenAIReasoning  `json:"reasoning,omitempty"`
	Tools              []OpenAITool      `json:"tools,omitempty"`
	ToolChoice         json.RawMessage   `json:"tool_choice,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	Truncation         string            `json:"truncation,omitempty"`
	// RawTools is the request's tools array as sent, echoed on the response.
	RawTools json.RawMessage `json:"-"`
}

type ResponsesObject struct {
	ID                 string                      `json:"id"`
	Object             string                      `json:"object"`
	CreatedAt          int64                       `json:"created_at"`
	Status             string                      `json:"status"`
	Model              string                      `json:"model"`
	Output             []ResponseOutputItem        `json:"output"`
	Usage              ResponsesUsage              `json:"usage"`
	PreviousResponseID string                      `json:"previous_response_id,omitempty"`
	Metadata           map[string]string           `json:"metadata,omitempty"`
	Error              *ResponsesError             `json:"error"`
	IncompleteDetails  *ResponsesIncompleteDetails `json:"incomplete_details"`
	Instructions       string                      `json:"instructions,omitempty"`
	StoredInput        json.RawMessage             `json:"-"`
	StoredInstr        string                      `json:"-"`
	StoredAt           int64                       `json:"stored_at,omitempty"`

	// Request parameters echoed back like the official API does.
	Tools             json.RawMessage `json:"tools,omitempty"`
	ToolChoice        json.RawMessage `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
	Temperature       *float64        `json:"temperature"`
	TopP              *float64        `json:"top_p"`
	MaxOutputTokens   *int            `json:"max_output_tokens"`
	Truncation        string          `json:"truncation,omitempty"`
}

// applyRequestEcho copies the request parameters the official API echoes on
// every response object, filling the documented defaults.
func (r *ResponsesObject) applyRequestEcho(req *ResponsesRequest, rawTools json.RawMessage) {
	if req != nil {
		r.ToolChoice = req.ToolChoice
		r.ParallelToolCalls = req.ParallelToolCalls
		r.Temperature = req.Temperature
		r.TopP = req.TopP
		r.MaxOutputTokens = req.MaxOutputTokens
		r.Truncation = req.Truncation
	}
	if len(rawTools) > 0 && string(rawTools) != "null" {
		r.Tools = rawTools
	}
	r.fillWireDefaults()
}

// fillWireDefaults sets the defaults of echoed fields that were not supplied.
func (r *ResponsesObject) fillWireDefaults() {
	if len(r.Tools) == 0 {
		r.Tools = json.RawMessage("[]")
	}
	if len(r.ToolChoice) == 0 {
		r.ToolChoice = json.RawMessage(`"auto"`)
	}
	if r.ParallelToolCalls == nil {
		t := true
		r.ParallelToolCalls = &t
	}
	if r.Truncation == "" {
		r.Truncation = "disabled"
	}
	if r.Output == nil {
		r.Output = []ResponseOutputItem{}
	}
	if r.Usage.InputTokensDetails == nil {
		r.Usage.InputTokensDetails = &ResponsesInputTokensDetails{}
	}
	if r.Usage.OutputTokensDetails == nil {
		r.Usage.OutputTokensDetails = &ResponsesOutputTokensDetails{}
	}
}

// responsesCustomToolCallType is the output item Codex expects back for a
// freeform (grammar) tool. A freeform call returned as a plain function_call is
// abandoned by Codex instead of being executed.
const responsesCustomToolCallType = "custom_tool_call"

type ResponseOutputItem struct {
	ID        string                `json:"id"`
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Status    string                `json:"status,omitempty"`
	Content   []ResponseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Namespace string                `json:"namespace,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	// Input carries the raw payload of a custom_tool_call output item, which
	// Codex reads instead of arguments for freeform tools.
	Input string `json:"input,omitempty"`
}

type ResponseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MarshalJSON always emits text and annotations on output_text parts, as the
// official API does; strict clients reject an output_text part without them.
func (p ResponseContentPart) MarshalJSON() ([]byte, error) {
	if p.Type != "output_text" {
		type plain ResponseContentPart
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		Type        string        `json:"type"`
		Text        string        `json:"text"`
		Annotations []interface{} `json:"annotations"`
	}{p.Type, p.Text, []interface{}{}})
}

type ResponsesUsage struct {
	InputTokens        int                          `json:"input_tokens"`
	InputTokensDetails *ResponsesInputTokensDetails `json:"input_tokens_details,omitempty"`
	OutputTokens       int                          `json:"output_tokens"`
	TotalTokens        int                          `json:"total_tokens"`
	// OutputTokensDetails is always reported; Kiro does not break out
	// reasoning tokens, so reasoning_tokens stays 0.
	OutputTokensDetails *ResponsesOutputTokensDetails `json:"output_tokens_details,omitempty"`
}

type ResponsesOutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type ResponsesInputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type ResponsesError struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResponsesIncompleteDetails struct {
	Reason string `json:"reason"`
}
