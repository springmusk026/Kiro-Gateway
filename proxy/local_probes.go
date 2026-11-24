package proxy

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

func localProbesEnabled() bool {
	v := strings.TrimSpace(os.Getenv("KIRO_LOCAL_PROBES"))
	return v != "0" && !strings.EqualFold(v, "false")
}

// isClaudeQuotaProbe recognises Claude Code's start-up check: max_tokens 1,
// a single user message whose whole text is "quota", no tools. Anything
// else (including a real one-token request) goes upstream.
func isClaudeQuotaProbe(req *ClaudeRequest) bool {
	if !localProbesEnabled() || req == nil || req.MaxTokens != 1 || len(req.Tools) != 0 || len(req.Messages) != 1 {
		return false
	}
	m := req.Messages[0]
	if m.Role != "user" {
		return false
	}
	var text string
	switch c := m.Content.(type) {
	case string:
		text = c
	case []interface{}:
		if len(c) != 1 {
			return false
		}
		block, ok := c[0].(map[string]interface{})
		if !ok || block["type"] != "text" {
			return false
		}
		text, _ = block["text"].(string)
	default:
		return false
	}
	return strings.EqualFold(strings.TrimSpace(text), "quota")
}

func writeClaudeProbeResponse(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            "msg_" + newRequestID()[4:],
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       []map[string]string{{"type": "text", "text": "ok"}},
		"stop_reason":   "max_tokens",
		"stop_sequence": nil,
		"usage":         map[string]int{"input_tokens": 1, "output_tokens": 1},
	})
}
