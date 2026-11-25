package proxy

import (
	"encoding/json"
	"math"
	"unicode/utf8"
)

func estimateApproxTokens(text string) int {
	if text == "" {
		return 0
	}
	var regularAscii, digits, symbols, nonASCII int
	for i := 0; i < len(text); i++ {
		countTokenClass(text[i], &regularAscii, &digits, &symbols)
	}
	// Every rune that is not a single ASCII byte counts once as non-ASCII
	// (invalid bytes decode to U+FFFD, one rune each, as []rune would).
	if regularAscii+digits+symbols < len(text) {
		nonASCII = utf8.RuneCountInString(text) - (regularAscii + digits + symbols)
	}
	return tokensFromClasses(regularAscii, digits, symbols, nonASCII)
}

// estimateApproxTokensBytes is estimateApproxTokens for encoded bytes.
func estimateApproxTokensBytes(text []byte) int {
	if len(text) == 0 {
		return 0
	}
	var regularAscii, digits, symbols, nonASCII int
	for _, b := range text {
		countTokenClass(b, &regularAscii, &digits, &symbols)
	}
	if regularAscii+digits+symbols < len(text) {
		nonASCII = utf8.RuneCount(text) - (regularAscii + digits + symbols)
	}
	return tokensFromClasses(regularAscii, digits, symbols, nonASCII)
}

// countTokenClass classifies one ASCII byte; bytes >= 0x80 belong to
// multi-byte runes and are counted by the caller.
func countTokenClass(b byte, regularAscii, digits, symbols *int) {
	switch {
	case b >= 0x80:
	case b >= '0' && b <= '9':
		*digits++
	case (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~'):
		*symbols++
	default:
		*regularAscii++
	}
}

func tokensFromClasses(regularAscii, digits, symbols, nonASCII int) int {
	length := regularAscii + digits + symbols + nonASCII
	if length == 0 {
		return 0
	}
	if length < 5 {
		return max(1, int(math.Ceil(float64(length)/3.0)))
	}

	estimated := int(math.Ceil(
		float64(regularAscii)/4.5 +
			float64(digits)/2.0 +
			float64(symbols)/1.5 +
			float64(nonASCII)/1.5,
	))

	if estimated < 1 {
		return 1
	}
	return estimated
}

func estimateClaudeRequestInputTokens(req *ClaudeRequest) int {
	if req == nil {
		return 0
	}

	total := estimateClaudeValueTokens(req.System)

	for _, msg := range req.Messages {
		total += estimateClaudeValueTokens(msg.Content)
	}

	for _, tool := range req.Tools {
		total += estimateApproxTokens(tool.Name)
		total += estimateApproxTokens(tool.Description)
		total += estimateJSONTokens(tool.InputSchema)
	}

	return total
}

func estimateClaudeOutputTokens(content, thinkingContent string, toolUses []KiroToolUse) int {
	total := estimateApproxTokens(content)
	total += estimateApproxTokens(thinkingContent)

	for _, tu := range toolUses {
		total += estimateApproxTokens(tu.Name)
		total += estimateJSONTokens(tu.Input)
	}

	return total
}

func estimateClaudeValueTokens(v interface{}) int {
	switch value := v.(type) {
	case nil:
		return 0
	case string:
		return estimateApproxTokens(value)
	case []interface{}:
		total := 0
		for _, part := range value {
			total += estimateClaudeValueTokens(part)
		}
		return total
	case map[string]interface{}:
		typeName, _ := value["type"].(string)
		switch typeName {
		case "text":
			if text, ok := value["text"].(string); ok {
				return estimateApproxTokens(text)
			}
		case "thinking":
			if thinking, ok := value["thinking"].(string); ok {
				return estimateApproxTokens(thinking)
			}
		case "tool_use":
			total := 0
			if name, ok := value["name"].(string); ok {
				total += estimateApproxTokens(name)
			}
			if input, ok := value["input"]; ok {
				total += estimateJSONTokens(input)
			}
			if total > 0 {
				return total
			}
		case "tool_result":
			if content, ok := value["content"]; ok {
				return estimateClaudeValueTokens(content)
			}
		case "image":
			return imageTokenEstimate
		case "document":
			return estimateDocumentTokens(value)
		case "redacted_thinking":
			return 0
		}

		total := 0
		if text, ok := value["text"].(string); ok {
			total += estimateApproxTokens(text)
		}
		if thinking, ok := value["thinking"].(string); ok {
			total += estimateApproxTokens(thinking)
		}
		if content, ok := value["content"]; ok {
			total += estimateClaudeValueTokens(content)
		}
		if total > 0 {
			return total
		}

		return estimateJSONTokens(value)
	default:
		return estimateJSONTokens(value)
	}
}

func estimateJSONTokens(v interface{}) int {
	if v == nil {
		return 0
	}

	// Same bytes as json.Marshal (the encoder only adds a trailing newline),
	// encoded into a reused buffer.
	buf := getJSONBuf()
	defer putJSONBuf(buf)
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return 0
	}
	b := buf.Bytes()
	return estimateApproxTokensBytes(b[:len(b)-1])
}

func estimateOpenAIRequestInputTokens(req *OpenAIRequest) int {
	if req == nil {
		return 0
	}

	total := 0

	for _, msg := range req.Messages {
		total += estimateOpenAIContentTokens(msg.Content)
		total += estimateApproxTokens(msg.ToolCallID)
		for _, tc := range msg.ToolCalls {
			total += estimateApproxTokens(tc.Function.Name)
			total += estimateApproxTokens(tc.Function.Arguments)
		}
	}

	for _, tool := range req.Tools {
		total += estimateApproxTokens(tool.Function.Name)
		total += estimateApproxTokens(tool.Function.Description)
		total += estimateJSONTokens(tool.Function.Parameters)
	}

	return total
}

func estimateOpenAIContentTokens(content interface{}) int {
	switch value := content.(type) {
	case nil:
		return 0
	case string:
		return estimateApproxTokens(value)
	case []interface{}:
		// Count parts individually so inline attachments are not counted as
		// base64 text.
		total := 0
		for _, p := range value {
			part, ok := p.(map[string]interface{})
			if !ok {
				total += estimateJSONTokens(p)
				continue
			}
			switch t, _ := part["type"].(string); t {
			case "text", "input_text", "output_text":
				if s, ok := part["text"].(string); ok {
					total += estimateApproxTokens(s)
				}
			case "image_url", "input_image", "image":
				total += imageTokenEstimate
			case "file", "input_file", "document":
				total += documentTokenEstimate
			default:
				total += estimateJSONTokens(part)
			}
		}
		return total
	default:
		text := extractOpenAIMessageText(value)
		if text != "" {
			return estimateApproxTokens(text)
		}
		return estimateJSONTokens(value)
	}
}

func estimateOpenAIOutputTokens(content, reasoningContent string, toolUses []KiroToolUse) int {
	return estimateClaudeOutputTokens(content, reasoningContent, toolUses)
}

// Attachments are billed by content, not by their base64 size; counting the
// encoded bytes as text inflated estimates far past the context window and
// made clients compact needlessly when Kiro reported no context usage.
const (
	// imageTokenEstimate approximates a typical image (Anthropic counts about
	// width*height/750, capped near 1,600 for images resized to fit).
	imageTokenEstimate = 1600
	// documentTokenEstimate is used when a binary document's text is unknown.
	documentTokenEstimate = 2000
)

// estimateDocumentTokens counts a text document's text and uses a flat
// estimate for binary (base64 or URL) documents.
func estimateDocumentTokens(block map[string]interface{}) int {
	src, _ := block["source"].(map[string]interface{})
	if src == nil {
		return documentTokenEstimate
	}
	switch t, _ := src["type"].(string); t {
	case "text":
		if s, ok := src["data"].(string); ok {
			return estimateApproxTokens(s)
		}
	case "content":
		return estimateClaudeValueTokens(src["content"])
	}
	return documentTokenEstimate
}
