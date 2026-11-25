package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Fixtures shaped like a long coding-agent session: a long system prompt,
// about sixty tools with real-looking JSON schemas and a forty-turn history
// mixing text, tool calls and tool results. Deterministic, so the golden
// payload test and the benchmarks see the same bytes every run.

const perfToolCount = 60
const perfTurnCount = 40

func perfSystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are an interactive command line agent that helps users with software engineering tasks.\n")
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&b, "Rule %d: keep answers short, prefer editing existing files, run the tests before you report success, "+
			"and never invent file paths or APIs that you have not read in this repository (section %d).\n", i, i%7)
	}
	return b.String()
}

func perfToolSchema(i int) map[string]interface{} {
	props := map[string]interface{}{
		"path": map[string]interface{}{"type": "string", "description": fmt.Sprintf("Absolute path of the file for tool %d.", i)},
		"mode": map[string]interface{}{"type": "string", "enum": []interface{}{"read", "write", "append"}},
		"options": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit":  map[string]interface{}{"type": "integer", "minimum": 1},
				"offset": map[string]interface{}{"type": "integer", "minimum": 0},
				"filters": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
	return map[string]interface{}{
		"type":                 "object",
		"properties":           props,
		"required":             []interface{}{"path"},
		"additionalProperties": false,
	}
}

func perfToolName(i int) string {
	if i%15 == 0 {
		// A few names long enough to need Kiro's hashed short form.
		return fmt.Sprintf("mcp__workspace_server__very_long_tool_name_for_operation_number_%02d", i)
	}
	return fmt.Sprintf("Tool%02d", i)
}

func perfToolDescription(i int) string {
	return strings.Repeat(fmt.Sprintf("Tool %d performs a precise workspace operation and reports structured results. ", i), 6)
}

// perfClaudeRequestJSON is a /v1/messages body.
func perfClaudeRequestJSON(stream bool) []byte {
	tools := make([]map[string]interface{}, 0, perfToolCount)
	for i := 0; i < perfToolCount; i++ {
		tools = append(tools, map[string]interface{}{
			"name":         perfToolName(i),
			"description":  perfToolDescription(i),
			"input_schema": perfToolSchema(i),
		})
	}
	var messages []map[string]interface{}
	for turn := 0; turn < perfTurnCount; turn++ {
		if turn == 0 {
			messages = append(messages, map[string]interface{}{"role": "user", "content": "Please refactor the request pipeline and keep the tests green."})
		}
		toolID := fmt.Sprintf("toolu_%04d", turn)
		messages = append(messages, map[string]interface{}{"role": "assistant", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": fmt.Sprintf("Step %d: I will inspect the next file before editing it.", turn)},
			map[string]interface{}{"type": "tool_use", "id": toolID, "name": perfToolName(turn % perfToolCount),
				"input": map[string]interface{}{"path": fmt.Sprintf("/repo/pkg/file_%d.go", turn), "mode": "read"}},
		}})
		messages = append(messages, map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "tool_result", "tool_use_id": toolID,
				"content": strings.Repeat(fmt.Sprintf("line %d of file %d: func handle() error { return nil }\n", turn, turn), 25)},
		}})
	}
	// Cache markers where coding agents put them: the system prompt and the
	// last user turn.
	ephemeral := map[string]interface{}{"type": "ephemeral"}
	messages = append(messages, map[string]interface{}{"role": "user", "content": []interface{}{
		map[string]interface{}{"type": "text", "text": "Now summarise what changed.", "cache_control": ephemeral},
	}})
	body := map[string]interface{}{
		"model":      "claude-sonnet-4.5",
		"max_tokens": 8192,
		"stream":     stream,
		"system":     []interface{}{map[string]interface{}{"type": "text", "text": perfSystemPrompt(), "cache_control": ephemeral}},
		"tools":      tools,
		"messages":   messages,
	}
	out, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return out
}

// perfOpenAIRequestJSON is a /v1/chat/completions body with the same content.
func perfOpenAIRequestJSON(stream bool) []byte {
	tools := make([]map[string]interface{}, 0, perfToolCount)
	for i := 0; i < perfToolCount; i++ {
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        perfToolName(i),
				"description": perfToolDescription(i),
				"parameters":  perfToolSchema(i),
			},
		})
	}
	messages := []map[string]interface{}{
		{"role": "system", "content": perfSystemPrompt()},
		{"role": "user", "content": "Please refactor the request pipeline and keep the tests green."},
	}
	for turn := 0; turn < perfTurnCount; turn++ {
		callID := fmt.Sprintf("call_%04d", turn)
		args, _ := json.Marshal(map[string]interface{}{"path": fmt.Sprintf("/repo/pkg/file_%d.go", turn), "mode": "read"})
		messages = append(messages, map[string]interface{}{
			"role":    "assistant",
			"content": fmt.Sprintf("Step %d: I will inspect the next file before editing it.", turn),
			"tool_calls": []interface{}{map[string]interface{}{
				"id": callID, "type": "function",
				"function": map[string]interface{}{"name": perfToolName(turn % perfToolCount), "arguments": string(args)},
			}},
		})
		messages = append(messages, map[string]interface{}{
			"role": "tool", "tool_call_id": callID,
			"content": strings.Repeat(fmt.Sprintf("line %d of file %d: func handle() error { return nil }\n", turn, turn), 25),
		})
	}
	messages = append(messages, map[string]interface{}{"role": "user", "content": "Now summarise what changed."})
	body := map[string]interface{}{
		"model":    "claude-sonnet-4.5",
		"stream":   stream,
		"tools":    tools,
		"messages": messages,
	}
	out, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return out
}

func perfClaudeRequest() *ClaudeRequest {
	var req ClaudeRequest
	if err := json.Unmarshal(perfClaudeRequestJSON(false), &req); err != nil {
		panic(err)
	}
	return &req
}

func perfOpenAIRequest() *OpenAIRequest {
	var req OpenAIRequest
	if err := json.Unmarshal(perfOpenAIRequestJSON(false), &req); err != nil {
		panic(err)
	}
	return &req
}
