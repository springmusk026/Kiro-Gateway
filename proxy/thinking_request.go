package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// Request-driven thinking.
//
// Clients ask for reasoning in their own API's terms: Anthropic `thinking`
// (enabled with budget_tokens, adaptive, disabled) plus `output_config.effort`,
// OpenAI Chat `reasoning_effort` (or an OpenRouter-style `reasoning` object),
// and Responses `reasoning.effort`. The legacy "-thinking" model suffix keeps
// working. resolveThinking folds all of these and the operator policy into one
// thinkingSpec:
//
//   - Enabled drives the thinking prompt and whether reasoning is surfaced.
//   - Budget becomes the prompt's <max_thinking_length>.
//   - Effort is forwarded natively to Kiro (additionalModelRequestFields), but
//     only to models whose ListAvailableModels schema declares effort levels,
//     and only on the /v1/responses transport, the one Kiro's own clients use.

type thinkingSpec struct {
	Enabled bool
	Budget  int
	Effort  string // normalized effort (low|medium|high|xhigh|max), "" if none
	Summary string // Responses reasoning.summary / Anthropic display, informational
	Source  string // suffix | request | policy | "" (for logs)
}

// thinkingAsk is what one request said about thinking, before policy.
type thinkingAsk struct {
	Suffix   bool   // model name carried the thinking suffix
	Explicit *bool  // request explicitly enabled (true) or disabled (false) thinking
	Budget   int    // explicit token budget (Anthropic budget_tokens)
	Effort   string // raw effort string as sent
	Summary  string
}

const (
	minThinkingBudget = 1024
	maxThinkingBudget = 200000
)

// effortBudgets maps effort levels to a thinking length for the prompt. The
// values only bound the hint; Kiro's native effort control (when available)
// does the real work.
var effortBudgets = map[string]int{
	"minimal": 2048,
	"low":     8192,
	"medium":  24576,
	"high":    65536,
	"xhigh":   131072,
	"max":     200000,
}

// normalizeEffort lowercases and maps vendor synonyms onto Kiro's levels.
// "none" is returned as-is so the caller can treat it as "thinking off".
func normalizeEffort(e string) string {
	e = strings.ToLower(strings.TrimSpace(e))
	switch e {
	case "", "default", "auto":
		return ""
	case "none", "off", "disabled":
		return "none"
	case "minimal", "min":
		return "minimal"
	case "extra_high", "extra-high", "x-high", "very_high":
		return "xhigh"
	case "maximum":
		return "max"
	}
	return e
}

func boolPtr(b bool) *bool { return &b }

// resolveThinking applies the operator policy to what the request asked for.
func resolveThinking(ask thinkingAsk, cfg config.ThinkingConfig) thinkingSpec {
	effort := normalizeEffort(ask.Effort)
	spec := thinkingSpec{Summary: ask.Summary}

	switch config.NormalizeThinkingMode(cfg.Mode) {
	case "always":
		spec.Enabled, spec.Source = true, "policy"
	case "never":
		spec.Source = "policy"
	default:
		switch {
		case ask.Suffix:
			// The suffix is an explicit choice made in the model name, so it
			// wins over a request-level "disabled" (keeps old setups intact).
			spec.Enabled, spec.Source = true, "suffix"
		case ask.Explicit != nil:
			spec.Enabled, spec.Source = *ask.Explicit, "request"
		case effort == "none":
			spec.Source = "request"
		case effort != "":
			// An effort without an explicit thinking switch is a request
			// for reasoning in OpenAI's model (reasoning_effort implies it).
			spec.Enabled, spec.Source = true, "request"
		}
	}
	if effort != "none" {
		spec.Effort = effort
	}
	if !spec.Enabled {
		return spec
	}

	budget := cfg.Budget
	if budget <= 0 {
		budget = config.DefaultThinkingBudget
	}
	if b, ok := effortBudgets[spec.Effort]; ok {
		budget = b
	}
	if ask.Budget > 0 {
		budget = ask.Budget
	}
	if budget < minThinkingBudget {
		budget = minThinkingBudget
	}
	if budget > maxThinkingBudget {
		budget = maxThinkingBudget
	}
	spec.Budget = budget
	return spec
}

// thinkingModePrompt is the system priming that turns on Kiro's thinking mode.
func thinkingModePrompt(budget int) string {
	if budget <= 0 || budget == config.DefaultThinkingBudget {
		return ThinkingModePrompt
	}
	return fmt.Sprintf("<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>%d</max_thinking_length>", budget)
}

// ---- request extraction ----

// claudeThinkingAsk reads an Anthropic Messages request.
func claudeThinkingAsk(req *ClaudeRequest, suffix bool) thinkingAsk {
	ask := thinkingAsk{Suffix: suffix}
	if req.Thinking != nil {
		switch strings.ToLower(strings.TrimSpace(req.Thinking.Type)) {
		case "enabled":
			ask.Explicit = boolPtr(true)
			ask.Budget = req.Thinking.BudgetTokens
		case "adaptive":
			ask.Explicit = boolPtr(true)
		case "disabled":
			ask.Explicit = boolPtr(false)
		}
		ask.Summary = req.Thinking.Display
	}
	if req.OutputConfig != nil {
		ask.Effort = req.OutputConfig.Effort
	}
	return ask
}

// resolveClaudeThinking resolves the model and thinking for a Messages
// request and records the budget on the request for the translator.
func resolveClaudeThinking(req *ClaudeRequest, cfg config.ThinkingConfig) (string, thinkingSpec) {
	model, suffix := ParseModelAndThinking(req.Model, cfg.Suffix)
	spec := resolveThinking(claudeThinkingAsk(req, suffix), cfg)
	req.thinkingBudget = spec.Budget
	return model, spec
}

// openAIThinkingAsk reads an OpenAI Chat Completions request.
func openAIThinkingAsk(req *OpenAIRequest, suffix bool) thinkingAsk {
	ask := thinkingAsk{Suffix: suffix, Effort: req.ReasoningEffort}
	if r := req.Reasoning; r != nil {
		if r.Effort != "" {
			ask.Effort = r.Effort
		}
		if r.MaxTokens > 0 {
			ask.Budget = r.MaxTokens
		}
		if r.Enabled != nil {
			ask.Explicit = r.Enabled
		}
		if r.Exclude != nil && *r.Exclude && ask.Explicit == nil && ask.Effort == "" {
			ask.Explicit = boolPtr(false)
		}
		ask.Summary = r.Summary
		if ask.Explicit == nil && ask.Effort == "" && ask.Budget > 0 {
			ask.Explicit = boolPtr(true)
		}
	}
	if req.Thinking != nil { // some clients send Anthropic-style thinking to /chat/completions
		switch strings.ToLower(strings.TrimSpace(req.Thinking.Type)) {
		case "enabled", "adaptive":
			ask.Explicit = boolPtr(true)
			if req.Thinking.BudgetTokens > 0 {
				ask.Budget = req.Thinking.BudgetTokens
			}
		case "disabled":
			ask.Explicit = boolPtr(false)
		}
	}
	return ask
}

// responsesThinkingAsk reads a Responses API request.
func responsesThinkingAsk(req *ResponsesRequest, suffix bool) thinkingAsk {
	ask := thinkingAsk{Suffix: suffix}
	if r := req.Reasoning; r != nil {
		ask.Effort = r.Effort
		ask.Summary = r.Summary
		if r.Effort == "" && r.Summary != "" {
			// Asking for a reasoning summary only makes sense with reasoning on.
			ask.Explicit = boolPtr(true)
		}
	}
	return ask
}

// ---- native effort forwarding ----

// reasoningControls is what a model's additionalModelRequestFieldsSchema
// declares: where effort lives and which levels it accepts.
type reasoningControls struct {
	Path         string   // "output_config" or "reasoning"
	Levels       []string // accepted effort levels, in schema order (ascending)
	DefaultLevel string
	Toggle       bool // thinking.type accepts both "adaptive" and "disabled"
}

var modelReasoningControls sync.Map // lowercased model id -> reasoningControls

func recordModelReasoningControls(models []ModelInfo) {
	for _, m := range models {
		key := strings.ToLower(strings.TrimSpace(m.ModelId))
		if key == "" {
			continue
		}
		if rc, ok := parseReasoningControls(m.AdditionalFieldsSchema); ok {
			modelReasoningControls.Store(key, rc)
		} else {
			modelReasoningControls.Delete(key)
		}
	}
}

func lookupReasoningControls(model string) (reasoningControls, bool) {
	v, ok := modelReasoningControls.Load(strings.ToLower(strings.TrimSpace(model)))
	if !ok {
		return reasoningControls{}, false
	}
	return v.(reasoningControls), true
}

type schemaNode struct {
	Properties map[string]*schemaNode `json:"properties"`
	Enum       []interface{}          `json:"enum"`
	Default    interface{}            `json:"default"`
}

func (n *schemaNode) prop(path ...string) *schemaNode {
	cur := n
	for _, p := range path {
		if cur == nil || cur.Properties == nil {
			return nil
		}
		cur = cur.Properties[p]
	}
	return cur
}

func enumStrings(n *schemaNode) []string {
	if n == nil {
		return nil
	}
	out := make([]string, 0, len(n.Enum))
	for _, v := range n.Enum {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseReasoningControls understands the schema both as a JSON object and as
// a JSON-encoded string (smithy documents arrive either way).
func parseReasoningControls(raw json.RawMessage) (reasoningControls, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return reasoningControls{}, false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return reasoningControls{}, false
		}
		raw = json.RawMessage(s)
	}
	var root schemaNode
	if json.Unmarshal(raw, &root) != nil {
		return reasoningControls{}, false
	}
	var rc reasoningControls
	for _, path := range []string{"output_config", "reasoning"} {
		node := root.prop(path, "effort")
		if levels := enumStrings(node); len(levels) > 0 {
			rc.Path, rc.Levels = path, levels
			if d, ok := node.Default.(string); ok {
				rc.DefaultLevel = d
			}
			break
		}
	}
	types := enumStrings(root.prop("thinking", "type"))
	hasAdaptive, hasDisabled := false, false
	for _, t := range types {
		hasAdaptive = hasAdaptive || t == "adaptive"
		hasDisabled = hasDisabled || t == "disabled"
	}
	rc.Toggle = hasAdaptive && hasDisabled
	return rc, rc.Path != "" || rc.Toggle
}

// effortRank orders the known levels so an unsupported one can be clamped to
// the closest the model accepts.
var effortRank = map[string]int{"minimal": 0, "low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 5}

// pickEffortLevel returns want when the model accepts it, otherwise the
// closest accepted level below it, otherwise the closest above, or "".
func pickEffortLevel(want string, levels []string) string {
	for _, l := range levels {
		if l == want {
			return l
		}
	}
	wr, ok := effortRank[want]
	if !ok {
		return ""
	}
	below, above := "", ""
	for _, l := range levels {
		r, ok := effortRank[l]
		if !ok {
			continue
		}
		if r < wr && (below == "" || r > effortRank[below]) {
			below = l
		}
		if r > wr && (above == "" || r < effortRank[above]) {
			above = l
		}
	}
	if below != "" {
		return below
	}
	return above
}

// nativeReasoningFields builds additionalModelRequestFields for the model, or
// nil when the model declares no effort control or nothing was requested.
func nativeReasoningFields(model string, spec thinkingSpec) map[string]interface{} {
	if spec.Effort == "" {
		return nil
	}
	rc, ok := lookupReasoningControls(model)
	if !ok || rc.Path == "" {
		return nil
	}
	level := pickEffortLevel(spec.Effort, rc.Levels)
	if level == "" {
		return nil
	}
	// Mirror Kiro's own client: the top levels need thinking on.
	if !spec.Enabled && (level == "xhigh" || level == "max") {
		for i := len(rc.Levels) - 1; i >= 0; i-- {
			if l := rc.Levels[i]; l != "xhigh" && l != "max" {
				level = l
				break
			}
		}
	}
	return map[string]interface{}{rc.Path: map[string]interface{}{"effort": level}}
}

// applyThinkingSpec stamps the native controls onto a translated payload.
func applyThinkingSpec(payload *KiroPayload, model string, spec thinkingSpec) {
	if payload == nil {
		return
	}
	if fields := nativeReasoningFields(model, spec); fields != nil {
		payload.AdditionalModelRequestFields = fields
	}
}
