package proxy

import (
	"errors"
	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/pool"
	"strings"
	"time"
)

const maxAccountRetryAttempts = 3

// maxSameAccountStreamRetries bounds same-account recovery of a truncated
// stream. Kiro IDE caps its truncation retry at one (Dt3 = 1 in extension.js),
// but that budget was set for a single-credential client. This proxy also
// rotates accounts, and the two recover different failures: a same-account
// retry helps when the upstream hiccupped, rotation helps when that account's
// backend is unhealthy. Two is therefore not a copy of the IDE.
//
// Cost of the extra attempt is bounded and small. Truncation returns nil from
// CallKiroAPIContext, so it never reaches the endpoint fallback or
// maxStreamAttemptsPerEndpoint - those fire only on transport errors. Worst
// case across maxAccountRetryAttempts accounts is 3*(1+budget) requests: 9 at
// two retries versus 6 at one.
//
// The payoff lands mostly on the three fully buffered paths, whose canRetry is
// nil: a non-stream client gets a 500 with nothing usable, so one more chance
// is worth more there than on a stream that has already flushed partial text.
const maxSameAccountStreamRetries = 2

// errUpstreamTruncatedResponse is a soft failure raised when a transport-clean
// stream carried content but never a terminal signal. It is retryable on the
// same account and must not mark the account unhealthy.
//
// There is deliberately no empty-response error here. A stream that produced no
// output at all is already caught one layer down: parseEventStreamTracked
// returns errEmptyKiroStream when !sawOutput (proxy/kiro.go), and
// CallKiroAPIContext retries it internally. Since sawOutput is set by exactly
// the three signals classifyStreamIntegrity measures (content, reasoning,
// toolUse), an all-zero measurement can never reach this layer with a nil
// error.
var errUpstreamTruncatedResponse = errors.New("upstream truncated response without stop reason")

// classifyStreamIntegrity decides whether an upstream stream that returned no
// transport error is actually complete. parseEventStream reports success on a
// clean EOF, so a stream that died mid-answer is otherwise indistinguishable
// from a finished one.
//
// Complete when a stopReason arrived, or when a tool call was delivered. Both
// match Kiro IDE, whose empty and truncation predicates each require
// toolCallCount === 0.
//
// Also complete when answer content arrived and upstream billed the turn — but
// only for accounts that have never been seen delivering a stopReason.
// stopReason rides inside metadataEvent, and some accounts (observed on
// authMethod=idc) never send that event on any turn: the stream carries a full
// answer, then contextUsageEvent + meteringEvent, then EOFs cleanly. Requiring
// metadataEvent there rejected every complete answer as truncated. meteringEvent
// is upstream closing the books on the turn, so it stands in as the terminal
// signal those accounts do send.
//
// That substitution must stay scoped to those accounts. Upstream bills the
// tokens it produced before dying, so on an account that normally does send
// metadataEvent, "content + metering + no stopReason" is the signature of a
// truncated turn, not a complete one. Accepting it there returned a stream with
// no terminal signal as a success, and mapClaudeStopReason turns an empty
// stopReason into "end_turn" — so a turn that died mid-task was reported to the
// client as finished, which is exactly how an agent loop stalls with no error.
// accountSendsStopReason carries that per-account observation; when it is true,
// metering no longer clears a missing stopReason and the turn is retried.
//
// Truncated when content arrived with neither a terminal signal nor metering:
// upstream never finished the turn, so a retry can still recover it.
//
// Reasoning with no answer stays truncated even when metering arrived, and
// remains STRICTER THAN THE IDE. The IDE's truncation predicate ends in
// (contentChars > 0 || !reasoningSeen), so reasoning with no answer and no
// stopReason is treated as complete there and is never retried. That is the
// exact shape of the production symptom this proxy exists to fix: thinking
// streams in full, then the turn dies before the answer or the tool call.
// Upstream bills for the thinking tokens it did produce, so metering cannot
// distinguish that from a finished turn and must not be allowed to clear it.
func classifyStreamIntegrity(contentChars, toolCallCount int, stopReason string, sawReasoning, sawMetering, accountSendsStopReason bool) error {
	if strings.TrimSpace(stopReason) != "" {
		// A tool_use stop reason with no tool delivered is upstream telling us the
		// turn ended on a tool call whose frames we never received. Treating it as
		// complete sends it to mapClaudeStopReason, where an unrecognized reason
		// becomes "end_turn" — reporting "the model is done" for a turn whose whole
		// point was the tool call, which stalls an agent loop with no error. Retry
		// instead; the tool call is recoverable.
		if isToolUseStopReason(stopReason) && toolCallCount == 0 {
			return errUpstreamTruncatedResponse
		}
		return nil
	}
	if toolCallCount > 0 {
		return nil
	}
	if contentChars == 0 {
		// Reasoning-only is truncated per the doc comment above. No content, no
		// reasoning, no tools is unreachable through the wired paths
		// (errEmptyKiroStream fires first, see above) and is likewise treated as
		// truncated so a future caller that bypasses that guard still cannot
		// ship an empty turn as a success.
		return errUpstreamTruncatedResponse
	}
	if sawMetering && !accountSendsStopReason {
		return nil
	}
	return errUpstreamTruncatedResponse
}

// isToolUseStopReason reports whether an upstream stop reason means "the turn
// ended because the model called a tool". Upstream has been seen using both the
// snake_case and the SCREAMING_SNAKE form, so the comparison is normalized the
// same way mapClaudeStopReason normalizes its input.
func isToolUseStopReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool_use", "tool_calls", "tooluse":
		return true
	}
	return false
}

// isStreamIntegrityError reports whether err is a soft integrity failure.
// Callers may rotate accounts on these, but must not run them through
// handleAccountFailure: an upstream blip should not mark an account unhealthy.
func isStreamIntegrityError(err error) bool {
	return errors.Is(err, errUpstreamTruncatedResponse)
}

// isQuotaErrorMessage reports exhausted usage quota (long cooldown). A bare
// 429 is rate throttling, handled by isRateLimitMessage with a short cooldown.
func isQuotaErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "quota") ||
		strings.Contains(msg, "usage limit") ||
		strings.Contains(msg, "monthly_request_count") ||
		strings.Contains(msg, "limit reached")
}

// isRateLimitMessage reports transient upstream throttling.
func isRateLimitMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return pool.HasStatusToken(msg, "429") ||
		strings.Contains(lower, "throttl") ||
		strings.Contains(lower, "rate exceeded") ||
		strings.Contains(lower, "rate_exceeded") ||
		strings.Contains(lower, "too many requests")
}

// isClientRequestErrorMessage reports upstream rejections caused by the request
// itself (shape or size). Another account would reject it the same way, so it
// must not count against the account.
func isClientRequestErrorMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return pool.HasStatusToken(msg, "400") ||
		pool.HasStatusToken(msg, "413") ||
		pool.HasStatusToken(msg, "422") ||
		strings.Contains(lower, "validationexception") ||
		strings.Contains(lower, "improperly formed") ||
		strings.Contains(lower, "content_length_exceeds_threshold") ||
		strings.Contains(lower, "request_body_invalid") ||
		strings.Contains(lower, "input is too long")
}

// isNonAccountForbiddenMessage reports 403-like failures that say nothing
// about the account's credentials: Kiro rejecting the client version or
// protocol, or an outbound proxy refusing the CONNECT.
func isNonAccountForbiddenMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "feature_not_supported") ||
		strings.Contains(lower, "no longer supported") ||
		strings.Contains(lower, "proxyconnect") ||
		strings.Contains(lower, "does not support this application")
}

// isDefinitiveAuthFailure reports errors that a token refresh cannot fix.
func isDefinitiveAuthFailure(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "invalid_grant") ||
		strings.Contains(lower, "refresh token expired") ||
		strings.Contains(lower, "has no refresh token")
}

func isOverageErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "402") && strings.Contains(msg, "overage")
}

func isSuspensionErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "temporarily_suspended") ||
		strings.Contains(msg, "temporarily is suspended") ||
		strings.Contains(msg, "account suspended")
}

func isProfileUnavailableErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "no available kiro profile")
}

func isAuthErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "token invalid") ||
		strings.Contains(msg, "token expired") ||
		strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "access token expired") ||
		strings.Contains(msg, "refresh token expired")
}

func (h *Handler) disableAccount(account *config.Account, banStatus, banReason string) {
	if account == nil {
		return
	}

	if !account.Enabled && account.BanStatus == banStatus && account.BanReason == banReason {
		return
	}

	if err := config.SetAccountBanStatus(account.ID, banStatus, banReason); err != nil {
		logger.Warnf("[AccountFailover] Failed to disable %s: %v", account.Email, err)
		return
	}

	logger.Warnf("[AccountFailover] Disabled %s: %s", account.Email, banReason)
	h.pool.Reload()
}

func (h *Handler) disableAccountOverage(account *config.Account) {
	if account == nil {
		return
	}

	snap, fetchErr := FetchOverageStatus(account)
	if fetchErr != nil {
		logger.Warnf("[AccountFailover] Failed to refresh overage status for %s: %v", account.Email, fetchErr)
		return
	}
	if persistErr := PersistOverageSnapshot(account.ID, snap); persistErr != nil {
		logger.Warnf("[AccountFailover] Failed to persist overage snapshot for %s: %v", account.Email, persistErr)
		return
	}

	logger.Warnf("[AccountFailover] Refreshed overage status for %s after upstream overage limit error: %s", account.Email, snap.Status)
	h.pool.Reload()
}

func (h *Handler) handleAccountFailure(account *config.Account, err error) {
	if account == nil || err == nil {
		return
	}

	errMsg := err.Error()
	switch {
	case isOverageErrorMessage(errMsg):
		h.disableAccountOverage(account)
		h.pool.RecordError(account.ID, false)
	case isQuotaErrorMessage(errMsg):
		h.pool.RecordError(account.ID, true)
	case isRateLimitMessage(errMsg):
		// Throttling clears in seconds; an hour-long quota cooldown here took
		// healthy accounts out of rotation.
		h.pool.Throttle(account.ID, rateLimitCooldown)
	case isSuspensionErrorMessage(errMsg):
		h.disableAccount(account, "BANNED", "AWS temporarily suspended - unusual user activity detected")
	case isProfileUnavailableErrorMessage(errMsg):
		// Profile ARN may be transiently unresolvable (upstream blip, stale token).
		// Treat as a soft failure: short cooldown so the next request rotates account,
		// but never auto-disable — operators can still investigate via warn logs.
		h.pool.RecordError(account.ID, false)
	case isClientRequestErrorMessage(errMsg):
		logger.Debugf("[AccountFailover] request rejected by upstream, not penalizing %s: %s", account.Email, errMsg)
	case isNonAccountForbiddenMessage(errMsg):
		h.pool.RecordError(account.ID, false)
	case isAuthErrorMessage(errMsg):
		h.handleAuthFailure(account, errMsg)
	default:
		h.pool.RecordError(account.ID, false)
	}
}

// rateLimitCooldown keeps a throttled account out of rotation briefly.
const rateLimitCooldown = 30 * time.Second

// handleAuthFailure reacts to a 401/403-style error. An expired or revoked
// access token is normal (it lives about an hour), so the account is only
// banned when a forced refresh also fails, or when the error already says
// the refresh token itself is invalid.
func (h *Handler) handleAuthFailure(account *config.Account, errMsg string) {
	if !isDefinitiveAuthFailure(errMsg) && !config.IsAPIKeyAccount(account) &&
		strings.TrimSpace(account.RefreshToken) != "" {
		if _, err := h.refreshAccountToken(account, true); err == nil {
			logger.Infof("[AccountFailover] Refreshed token for %s after auth error; keeping account enabled", account.Email)
			h.pool.RecordError(account.ID, false)
			return
		} else if !isAuthErrorMessage(err.Error()) && !isDefinitiveAuthFailure(err.Error()) {
			// Refresh failed for a non-auth reason (network, IdP outage):
			// do not ban on a transient problem.
			logger.Warnf("[AccountFailover] Token refresh for %s failed transiently: %v", account.Email, err)
			h.pool.RecordError(account.ID, false)
			return
		}
	}
	h.disableAccount(account, "BANNED", "Authentication failed - token invalid or expired")
}
