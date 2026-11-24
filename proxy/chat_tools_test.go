package proxy

import (
	"strings"
	"testing"
)

func openAIToolsFixture(names ...string) []OpenAITool {
	tools := make([]OpenAITool, 0, len(names))
	for _, n := range names {
		var tool OpenAITool
		tool.Type = "function"
		tool.Function.Name = n
		tool.Function.Parameters = map[string]interface{}{"type": "object"}
		tools = append(tools, tool)
	}
	return tools
}

func TestOpenAIToolChoiceNoneDropsTools(t *testing.T) {
	req := &OpenAIRequest{
		Model:      "claude-sonnet-4.5",
		Messages:   []OpenAIMessage{{Role: "user", Content: "hi"}},
		Tools:      openAIToolsFixture("get_weather"),
		ToolChoice: "none",
	}
	payload := OpenAIToKiro(req, false)
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx != nil && len(ctx.Tools) > 0 {
		t.Fatalf("tool_choice none must drop tools, got %d", len(ctx.Tools))
	}
}

func TestOpenAIToolChoiceDirectives(t *testing.T) {
	cases := []struct {
		name   string
		choice interface{}
		want   string
	}{
		{"required", "required", "must call at least one available tool"},
		{"chat named", map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}}, `tool named "get_weather"`},
		{"responses named", map[string]interface{}{"type": "function", "name": "get_weather"}, `tool named "get_weather"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &OpenAIRequest{
				Model:      "claude-sonnet-4.5",
				Messages:   []OpenAIMessage{{Role: "user", Content: "hi"}},
				Tools:      openAIToolsFixture("get_weather", "get_time"),
				ToolChoice: tc.choice,
			}
			content := OpenAIToKiro(req, false).ConversationState.CurrentMessage.UserInputMessage.Content
			if !strings.Contains(content, tc.want) {
				t.Fatalf("missing directive %q in %q", tc.want, content)
			}
		})
	}

	no := false
	req := &OpenAIRequest{
		Model:             "claude-sonnet-4.5",
		Messages:          []OpenAIMessage{{Role: "user", Content: "hi"}},
		Tools:             openAIToolsFixture("get_weather"),
		ParallelToolCalls: &no,
	}
	if content := OpenAIToKiro(req, false).ConversationState.CurrentMessage.UserInputMessage.Content; !strings.Contains(content, "at most one tool") {
		t.Fatalf("parallel_tool_calls=false must add a single-tool directive: %q", content)
	}
}

func TestOpenAILongToolNamesAreRestored(t *testing.T) {
	long := "mcp__some_server_with_a_really_long_name__" + strings.Repeat("x", 70)
	call := ToolCall{ID: "call_1", Type: "function"}
	call.Function.Name = long
	call.Function.Arguments = "{}"
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []ToolCall{call}},
			{Role: "tool", ToolCallID: "call_1", Content: "done"},
		},
		Tools: openAIToolsFixture(long),
	}
	payload := OpenAIToKiro(req, false)
	short := shortenToolName(long)
	if short == long || payload.ToolNameMap[short] != long {
		t.Fatalf("ToolNameMap = %v", payload.ToolNameMap)
	}
	for _, hm := range payload.ConversationState.History {
		if hm.AssistantResponseMessage == nil {
			continue
		}
		for _, tu := range hm.AssistantResponseMessage.ToolUses {
			if len(tu.Name) > 64 {
				t.Fatalf("history tool name not shortened: %q", tu.Name)
			}
		}
	}

	var got string
	cb := withToolNameRestore(payload, &KiroStreamCallback{OnToolUse: func(tu KiroToolUse) { got = tu.Name }})
	cb.OnToolUse(KiroToolUse{Name: short})
	if got != long {
		t.Fatalf("restored name = %q", got)
	}
}

func TestClaudeToolResultIsErrorSetsErrorStatus(t *testing.T) {
	_, _, results := extractClaudeUserContent([]interface{}{
		map[string]interface{}{"type": "tool_result", "tool_use_id": "t1", "content": "boom", "is_error": true},
		map[string]interface{}{"type": "tool_result", "tool_use_id": "t2", "content": "fine"},
	})
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Status != "error" || !strings.HasPrefix(results[0].Content[0].Text, "Error: ") {
		t.Fatalf("error result = %+v", results[0])
	}
	if results[1].Status != "success" || results[1].Content[0].Text != "fine" {
		t.Fatalf("success result = %+v", results[1])
	}
}
