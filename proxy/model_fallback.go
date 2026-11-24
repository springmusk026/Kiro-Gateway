package proxy

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
)

// Model fallback.
//
// When a request fails because the model itself is unavailable — Kiro says
// the model is temporarily unavailable or overloaded, every account in the
// loop answered with a 503/529-class error, or the model id is no longer
// offered — the request is retried once on the first configured fallback
// model (config modelFallbacks). Client errors, auth, quota and rate limits
// never fall back. The retry happens only before anything was streamed to
// the client and only while the request's upstream attempt budget has room.
// The fallback model is reported in the x-kiro-gateway-fallback-model header (when
// the status line has not been sent yet) and in the request log.

const fallbackModelHeader = "x-kiro-gateway-fallback-model"

// isModelUnavailableMessage matches errors that name the model (not the
// account) as unavailable. Every account would fail the same way, so the
// account loop can stop early and fall back straight away.
func isModelUnavailableMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "model_temporarily_unavailable") ||
		strings.Contains(lower, "server_is_overloaded") ||
		strings.Contains(lower, "insufficient_model_capacity")
}

// modelFallbackEligible reports whether err is a model-availability failure
// that may be retried on a fallback model.
func modelFallbackEligible(err error) bool {
	if err == nil || errors.Is(err, errUpstreamBudgetExhausted) || errors.Is(err, context.Canceled) {
		return false
	}
	msg := err.Error()
	if isInvalidModelMessage(msg) || isModelUnavailableMessage(msg) {
		return true
	}
	info := classifyUpstreamError(err)
	if info.ClientError || info.Status == http.StatusTooManyRequests {
		return false
	}
	return info.Status == 529
}

// upstreamAttemptsLeft reports whether the request's budget has room for
// another upstream call (true for contexts without a budget).
func upstreamAttemptsLeft(ctx context.Context) bool {
	b, ok := ctx.Value(upstreamBudgetKey{}).(*upstreamBudget)
	return !ok || b.remaining.Load() > 0
}

// fallbackCandidate returns the model to fall back to from model, or "".
func fallbackCandidate(ctx context.Context, model string) string {
	if ctx == nil || ctx.Err() != nil || !upstreamAttemptsLeft(ctx) {
		return ""
	}
	if b, ok := ctx.Value(upstreamBudgetKey{}).(*upstreamBudget); ok && b.fallbackUsed.Load() {
		return ""
	}
	for _, fb := range config.FallbackModelsFor(model) {
		if !strings.EqualFold(fb, model) && !config.IsModelDisabled(fb) {
			return fb
		}
	}
	return ""
}

// stopForModelFallback tells the account loop to stop rotating accounts
// because err names the model as unavailable and a fallback can take over.
func stopForModelFallback(ctx context.Context, model string, err error) bool {
	if err == nil || !isModelUnavailableMessage(err.Error()) {
		return false
	}
	return fallbackCandidate(ctx, model) != ""
}

// takeModelFallback switches payload to the fallback model of model when
// lastErr allows it. It returns the new model, or "" when the request must
// fail with lastErr. Call only when nothing has been streamed yet.
func (h *Handler) takeModelFallback(ctx context.Context, w http.ResponseWriter, payload *KiroPayload, model string, lastErr error) string {
	if payload == nil || !modelFallbackEligible(lastErr) {
		return ""
	}
	fb := fallbackCandidate(ctx, model)
	if fb == "" {
		return ""
	}
	if b, ok := ctx.Value(upstreamBudgetKey{}).(*upstreamBudget); ok && !b.fallbackUsed.CompareAndSwap(false, true) {
		return ""
	}
	switchPayloadModel(payload, model, fb)
	if w != nil {
		// No effect once a heartbeat has sent the status line.
		w.Header().Set(fallbackModelHeader, fb)
	}
	if tr := traceFrom(ctx); tr != nil {
		tr.setFallbackModel(fb)
	}
	logger.Warnf("[ModelFallback] %s unavailable (%s); retrying on %s", model, truncateForLog(lastErr.Error(), 200), fb)
	return fb
}

// switchPayloadModel re-stamps every message of payload with the fallback
// model and adapts the native reasoning controls to it.
func switchPayloadModel(payload *KiroPayload, from, to string) {
	cur := &payload.ConversationState.CurrentMessage.UserInputMessage
	cur.ModelID = to
	for i := range payload.ConversationState.History {
		if u := payload.ConversationState.History[i].UserInputMessage; u != nil && u.ModelID != "" {
			u.ModelID = to
		}
	}
	payload.AdditionalModelRequestFields = remapReasoningFields(payload.AdditionalModelRequestFields, to)
}

// remapReasoningFields carries the requested effort over to the fallback
// model's own control, or drops it when that model declares none.
func remapReasoningFields(fields map[string]interface{}, model string) map[string]interface{} {
	if len(fields) == 0 {
		return fields
	}
	effort := ""
	for _, v := range fields {
		if m, ok := v.(map[string]interface{}); ok {
			if e, ok := m["effort"].(string); ok {
				effort = e
			}
		}
	}
	rc, ok := lookupReasoningControls(model)
	if effort == "" || !ok || rc.Path == "" {
		return nil
	}
	level := pickEffortLevel(effort, rc.Levels)
	if level == "" {
		return nil
	}
	return map[string]interface{}{rc.Path: map[string]interface{}{"effort": level}}
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
