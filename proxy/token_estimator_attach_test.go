package proxy

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A large inline image must not be estimated as hundreds of thousands of
// tokens of base64 text.
func TestEstimateIgnoresBase64Attachments(t *testing.T) {
	big := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 1_000_000)))
	creq := &ClaudeRequest{Messages: []ClaudeMessage{{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "text", "text": "what is in this picture?"},
		map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": big}},
		map[string]interface{}{"type": "document", "source": map[string]interface{}{"type": "base64", "media_type": "application/pdf", "data": big}},
	}}}}
	if n := estimateClaudeRequestInputTokens(creq); n > 10_000 {
		t.Fatalf("claude estimate = %d, attachments counted as text", n)
	}
	oreq := &OpenAIRequest{Messages: []OpenAIMessage{{Role: "user", Content: []interface{}{
		map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64," + big}},
	}}}}
	if n := estimateOpenAIRequestInputTokens(oreq); n > 10_000 || n < imageTokenEstimate {
		t.Fatalf("openai image-only estimate = %d", n)
	}
}
