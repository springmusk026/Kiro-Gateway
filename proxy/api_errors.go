package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// upstreamErrorInfo is the client-facing shape of an upstream failure. The
// same classification feeds HTTP error bodies, the Anthropic SSE "error"
// event, the OpenAI stream error chunk and the Responses response.failed
// event, so a given upstream failure looks the same on every surface.
type upstreamErrorInfo struct {
	Status        int
	AnthropicType string
	OpenAIType    string
	Code          string // OpenAI error code (also used for Responses error.code)
	Message       string
	RetryAfter    time.Duration
	// PromptTooLong marks a context-length rejection. Claude Code auto-compacts
	// when an error message starts with "prompt is too long".
	PromptTooLong bool
	// ClientError means the request itself was rejected: every account would
	// reject it the same way, so the account loop must stop.
	ClientError bool
}

const promptTooLongPrefix = "prompt is too long: "

func isPromptTooLongMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "content_length_exceeds_threshold") ||
		strings.Contains(lower, "input is too long") ||
		strings.Contains(lower, "content-length exceeds") ||
		strings.Contains(lower, "content length exceeds") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "prompt is too long")
}

func isInvalidModelMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "invalid_model_id") || strings.Contains(lower, "invalid model id")
}

func isOverloadedMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return pool503(msg) ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "serviceunavailable") ||
		strings.Contains(lower, "service unavailable") ||
		strings.Contains(lower, "insufficient_model_capacity") ||
		strings.Contains(lower, "model capacity")
}

func pool503(msg string) bool {
	return strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "HTTP 529")
}

// classifyUpstreamError maps an upstream (or proxy-internal) failure to the
// status and error types the official APIs would use for the same condition.
func classifyUpstreamError(err error) upstreamErrorInfo {
	if err == nil {
		return upstreamErrorInfo{Status: 500, AnthropicType: "api_error", OpenAIType: "server_error", Message: "unknown error"}
	}
	msg := err.Error()
	info := upstreamErrorInfo{Message: msg}
	switch {
	case isPromptTooLongMessage(msg):
		info.Status = http.StatusBadRequest
		info.AnthropicType = "invalid_request_error"
		info.OpenAIType = "invalid_request_error"
		info.Code = "context_length_exceeded"
		info.PromptTooLong = true
		info.ClientError = true
		if !strings.HasPrefix(strings.ToLower(msg), "prompt is too long") {
			info.Message = promptTooLongPrefix + msg
		}
	case isInvalidModelMessage(msg):
		info.Status = http.StatusNotFound
		info.AnthropicType = "not_found_error"
		info.OpenAIType = "invalid_request_error"
		info.Code = "model_not_found"
		info.ClientError = true
	case isMonthlyLimitMessage(msg) || isQuotaErrorMessage(msg) || isUsageLimitBody([]byte(msg)):
		info.Status = http.StatusTooManyRequests
		info.AnthropicType = "rate_limit_error"
		info.OpenAIType = "insufficient_quota"
		info.Code = "insufficient_quota"
		info.RetryAfter = parseRetryAfterFromMessage(msg)
	case isRateLimitMessage(msg):
		info.Status = http.StatusTooManyRequests
		info.AnthropicType = "rate_limit_error"
		info.OpenAIType = "rate_limit_exceeded"
		info.Code = "rate_limit_exceeded"
		info.RetryAfter = parseRetryAfterFromMessage(msg)
	case isClientRequestErrorMessage(msg):
		info.Status = http.StatusBadRequest
		info.AnthropicType = "invalid_request_error"
		info.OpenAIType = "invalid_request_error"
		info.Code = "invalid_request_error"
		info.ClientError = true
	case errors.Is(err, errUpstreamBudgetExhausted) || isOverloadedMessage(msg):
		info.Status = 529
		info.AnthropicType = "overloaded_error"
		info.OpenAIType = "server_error"
		info.Code = "server_overloaded"
	default:
		info.Status = http.StatusInternalServerError
		info.AnthropicType = "api_error"
		info.OpenAIType = "server_error"
		info.Code = "server_error"
	}
	return info
}

// openAIStatus is the HTTP status for OpenAI-shaped surfaces, which have no
// 529: an overloaded upstream is a 503 there.
func (i upstreamErrorInfo) openAIStatus() int {
	if i.Status == 529 {
		return http.StatusServiceUnavailable
	}
	return i.Status
}

// isTerminalUpstreamError reports failures caused by the request itself, which
// another account would reject identically.
func isTerminalUpstreamError(err error) bool {
	if err == nil {
		return false
	}
	return classifyUpstreamError(err).ClientError
}

// claudeMessage renders the message for Anthropic clients. A context-length
// rejection is phrased like Anthropic's own ("prompt is too long: N tokens >
// M maximum") so Claude Code can detect it and compact the conversation.
func (i upstreamErrorInfo) claudeMessage(estimatedInputTokens int, model string) string {
	if !i.PromptTooLong {
		return i.Message
	}
	window := getContextWindowSize(model)
	if window <= 0 {
		return i.Message
	}
	tokens := estimatedInputTokens
	if tokens <= window {
		tokens = window + 1
	}
	return fmt.Sprintf("%s%d tokens > %d maximum", promptTooLongPrefix, tokens, window)
}

func setRetryAfterHeader(w http.ResponseWriter, d time.Duration) {
	if d <= 0 {
		return
	}
	secs := int((d + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

// sendClaudeUpstreamError writes an upstream failure as an Anthropic HTTP error.
func (h *Handler) sendClaudeUpstreamError(w http.ResponseWriter, err error, estimatedInputTokens int, model string) {
	info := classifyUpstreamError(err)
	setRetryAfterHeader(w, info.RetryAfter)
	h.sendClaudeError(w, info.Status, info.AnthropicType, info.claudeMessage(estimatedInputTokens, model))
}

// claudeStreamErrorEvent is the data of an Anthropic SSE "error" event.
func claudeStreamErrorEvent(err error, estimatedInputTokens int, model string) map[string]interface{} {
	info := classifyUpstreamError(err)
	return map[string]interface{}{
		"type": "error",
		"error": map[string]string{
			"type":    info.AnthropicType,
			"message": info.claudeMessage(estimatedInputTokens, model),
		},
	}
}

// sendOpenAIUpstreamError writes an upstream failure as an OpenAI HTTP error.
func (h *Handler) sendOpenAIUpstreamError(w http.ResponseWriter, err error) {
	info := classifyUpstreamError(err)
	setRetryAfterHeader(w, info.RetryAfter)
	h.sendOpenAIErrorWithCode(w, info.openAIStatus(), info.OpenAIType, info.Code, info.Message)
}

// openAIErrorObject is the "error" member of an OpenAI error body.
func openAIErrorObject(errType, code, message string) map[string]interface{} {
	var codeVal interface{}
	if code != "" {
		codeVal = code
	}
	return map[string]interface{}{
		"message": message,
		"type":    errType,
		"param":   nil,
		"code":    codeVal,
	}
}

func openAIStreamErrorObject(err error) map[string]interface{} {
	info := classifyUpstreamError(err)
	return openAIErrorObject(info.OpenAIType, info.Code, info.Message)
}

func (h *Handler) sendOpenAIErrorWithCode(w http.ResponseWriter, status int, errType, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": openAIErrorObject(errType, code, message),
	})
}

// noAccountsRetryAfter is the wait until the first cooled-down account comes
// back, or a short default when nothing is cooling down.
func (h *Handler) noAccountsRetryAfter() time.Duration {
	if h != nil && h.pool != nil {
		if d := h.pool.EarliestCooldown(); d > 0 {
			if d > time.Hour {
				d = time.Hour
			}
			return d
		}
	}
	return 0
}

const noAvailableAccountsMessage = "No available accounts"

// sendClaudeNoAccounts mirrors Anthropic's capacity error: 529 overloaded_error.
func (h *Handler) sendClaudeNoAccounts(w http.ResponseWriter) {
	setRetryAfterHeader(w, h.noAccountsRetryAfter())
	h.sendClaudeError(w, 529, "overloaded_error", noAvailableAccountsMessage)
}

// sendOpenAINoAccounts mirrors OpenAI's capacity error: 503.
func (h *Handler) sendOpenAINoAccounts(w http.ResponseWriter) {
	setRetryAfterHeader(w, h.noAccountsRetryAfter())
	h.sendOpenAIErrorWithCode(w, http.StatusServiceUnavailable, "server_error", "server_overloaded", noAvailableAccountsMessage)
}
