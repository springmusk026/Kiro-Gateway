package proxy

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/logger"
	"github.com/springmusk026/Kiro-Gateway/pool"
)

// Upstream protocol selection.
//
// Kiro serves chat through two operations: the newer runtime /v1/responses
// SSE endpoint and the older generateAssistantResponse event stream. Which one
// an account may use is not uniform: some report the legacy operation as
// retired (403 FEATURE_NOT_SUPPORTED), while other gateways (9router,
// OmniRoute, kiro2cc) still default to it and only switch on specific
// rejections such as USER_REQUEST_RATE_EXCEEDED. In "auto" mode (the
// default) each account tries its last working protocol first and falls back
// to the other one when upstream rejects the protocol itself, as long as
// nothing has been streamed to the client yet.
const responsesProtocolAuto = "auto"

// protocolMemory is how long a working protocol is preferred for an account.
const protocolMemory = 6 * time.Hour

type protocolChoice struct {
	protocol string
	until    time.Time
}

var accountProtocols sync.Map // accountID → protocolChoice

func upstreamProtocolMode(account *config.Account) string {
	if account == nil || config.IsAPIKeyAccount(account) {
		return responsesProtocolLegacy // API keys use the CLI runtime path
	}
	switch strings.ToLower(strings.TrimSpace(config.GetUpstreamProtocol())) {
	case responsesProtocolLegacy:
		return responsesProtocolLegacy
	case responsesProtocolResponses:
		return responsesProtocolResponses
	default:
		return responsesProtocolAuto
	}
}

func preferredProtocols(account *config.Account) (first, second string) {
	if v, ok := accountProtocols.Load(account.ID); ok {
		c := v.(protocolChoice)
		if time.Now().Before(c.until) && c.protocol == responsesProtocolLegacy {
			return responsesProtocolLegacy, responsesProtocolResponses
		}
	}
	return responsesProtocolResponses, responsesProtocolLegacy
}

func rememberProtocol(account *config.Account, protocol string) {
	accountProtocols.Store(account.ID, protocolChoice{protocol: protocol, until: time.Now().Add(protocolMemory)})
}

// isProtocolRejection reports errors meaning "this account cannot use this
// operation", as opposed to account, quota or request problems.
func isProtocolRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "feature_not_supported") ||
		strings.Contains(lower, "no longer supported") ||
		strings.Contains(lower, "user_request_rate_exceeded") ||
		strings.Contains(lower, "unknownoperation") ||
		strings.Contains(lower, "unknown operation") ||
		strings.Contains(lower, "does not support this application") ||
		// /v1/responses insists on a profile ARN; accounts that have none
		// (Builder ID often cannot resolve one) still work on the legacy
		// operation, which accepts ARN-less requests.
		strings.Contains(lower, "profilearn is required") ||
		pool.HasStatusToken(msg, "404")
}

// callProtocolFn is swapped in tests.
var callProtocolFn = callProtocol

func callProtocol(protocol string, ctx context.Context, account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if protocol == responsesProtocolLegacy {
		return callKiroLegacyAPIContext(ctx, account, payload, callback)
	}
	return callKiroResponsesAPIContext(ctx, account, payload, callback)
}

// trackEmission wraps the output callbacks so the caller knows whether any
// content reached the client (after which switching protocol would duplicate
// output).
func trackEmission(cb *KiroStreamCallback) (*KiroStreamCallback, *bool) {
	emitted := new(bool)
	if cb == nil {
		return cb, emitted
	}
	wrapped := *cb
	if cb.OnText != nil {
		inner := cb.OnText
		wrapped.OnText = func(text string, thinking bool) {
			*emitted = true
			inner(text, thinking)
		}
	}
	if cb.OnToolUse != nil {
		inner := cb.OnToolUse
		wrapped.OnToolUse = func(tu KiroToolUse) {
			*emitted = true
			inner(tu)
		}
	}
	return &wrapped, emitted
}

func callKiroWithProtocolSelection(ctx context.Context, account *config.Account, payload *KiroPayload, callback *KiroStreamCallback) error {
	if tr := traceFrom(ctx); tr != nil && callback != nil {
		wrapped := *callback
		inner := callback.OnCacheUsage
		wrapped.OnCacheUsage = func(read, write int) {
			tr.setCache(read, write)
			if inner != nil {
				inner(read, write)
			}
		}
		callback = &wrapped
	}
	if account != nil {
		release, err := acquireAccountSlot(ctx, account.ID)
		if err != nil {
			return err
		}
		defer release()
	}
	switch mode := upstreamProtocolMode(account); mode {
	case responsesProtocolLegacy, responsesProtocolResponses:
		if tr := traceFrom(ctx); tr != nil {
			tr.setProtocol(mode)
		}
		if mode == responsesProtocolLegacy {
			return callKiroLegacyAPIContext(ctx, account, payload, callback)
		}
		return callKiroResponsesAPIContext(ctx, account, payload, callback)
	}

	first, second := preferredProtocols(account)
	tracked, emitted := trackEmission(callback)
	err := callProtocolFn(first, ctx, account, payload, tracked)
	if tr := traceFrom(ctx); tr != nil {
		tr.setProtocol(first)
	}
	if err == nil {
		rememberProtocol(account, first)
		return nil
	}
	if *emitted || !isProtocolRejection(err) || (ctx != nil && ctx.Err() != nil) {
		return err
	}
	logger.Infof("[Upstream] %s protocol rejected for %s (%v); trying %s",
		first, accountEmailForLog(account), err, second)
	if tr := traceFrom(ctx); tr != nil {
		tr.setProtocol(second)
	}
	if err2 := callProtocolFn(second, ctx, account, payload, tracked); err2 != nil {
		return err2
	}
	rememberProtocol(account, second)
	return nil
}
