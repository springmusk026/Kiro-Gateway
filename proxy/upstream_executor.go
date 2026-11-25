package proxy

import (
	"context"
	"net/http"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// Upstream executor.
//
// Every chat API (Anthropic Messages, OpenAI Chat Completions, OpenAI
// Responses; streaming and buffered) runs the same loop against the account
// pool:
//
//	pick an account (affinity, key routing, exclusions)
//	→ make sure its token is valid
//	→ call Kiro (protocol selection, per-account concurrency slot and the
//	  request's attempt budget live below, in CallKiroAPIContext), with
//	  same-account integrity retries while nothing has been streamed
//	→ on failure: classify, penalise the account unless the error is the
//	  client's own, and move to the next account — unless output already
//	  reached the client, the error is terminal, or the model is unavailable
//	→ after the account loop: one retry on a fallback model when eligible
//	→ on success: usage accounting, then the API's own success response.
//
// executeUpstream owns that loop. The API-specific code supplies an
// upstreamRequest: how to build one attempt (callback, accumulators, reset),
// whether output has started, and how to write success and errors in its own
// wire format.

// upstreamUsage is what a successful attempt reports for accounting.
type upstreamUsage struct {
	inputTokens  int
	outputTokens int
	credits      float64
}

// upstreamAttempt is one try on one account.
type upstreamAttempt struct {
	// ctx is the attempt's upstream context: a child of the request context
	// that cancel ends early (stop sequences).
	ctx     context.Context
	cancel  context.CancelFunc
	account *config.Account
	// model is the model this attempt runs on (it changes after a fallback).
	model      string
	cacheUsage promptCacheUsage
}

// upstreamAttemptRun is what the API code builds for one attempt. Its
// closures share the attempt's accumulators.
type upstreamAttemptRun struct {
	callback *KiroStreamCallback
	// measure and reset feed runKiroWithIntegrityRetry.
	measure func() (contentChars, toolCount int, stopReason string, sawReasoning bool)
	reset   func()
	// stopped reports that a stop sequence matched and cancelled the upstream
	// call; the cancellation error then counts as success. Optional.
	stopped func() bool
	// finish runs after a successful call: drain buffered output and compute
	// usage. respond writes the success response after usage was recorded.
	finish  func() upstreamUsage
	respond func(upstreamUsage)
}

// upstreamRequest describes one client request to the executor.
type upstreamRequest struct {
	// endpoint labels the request log ("claude", "openai", "responses").
	endpoint     string
	payload      *KiroPayload
	model        string
	apiKeyID     string
	cacheProfile *promptCacheProfile

	// started reports whether output has reached the client. After that
	// nothing is retried. Nil for fully buffered responses.
	started func() bool
	// keepAccountAfterOutput skips the account penalty for an error that
	// arrives after output started (the Responses stream's behaviour).
	keepAccountAfterOutput bool

	// attempt builds the per-attempt state.
	attempt func(a *upstreamAttempt) upstreamAttemptRun
	// failStarted reports an error after output started, in-stream.
	failStarted func(a *upstreamAttempt, err error)
	// failNoAccounts reports that no account could serve the request.
	failNoAccounts func(model string)
	// fail reports the last upstream error once every option is spent.
	fail func(model string, err error)
}

// upstreamAttemptRunner is the upstream call with integrity retries; a
// variable so executor tests can run without a Kiro endpoint.
var upstreamAttemptRunner = runKiroWithIntegrityRetry

// executeUpstream runs req against the account pool until it succeeds, an
// error ends it, or the retry options are spent. w receives the fallback
// model header; it may be nil.
func (h *Handler) executeUpstream(ctx context.Context, w http.ResponseWriter, req *upstreamRequest) {
	reqStart := time.Now()
	model := req.model
	excluded := h.routingExclusions(req.apiKeyID)
	var lastErr error

	for {
		for i := 0; i < maxAccountRetryAttempts; i++ {
			account := h.pickAccount(req.apiKeyID, conversationKey(req.payload), model, excluded)
			if account == nil {
				break
			}
			if err := h.ensureValidToken(account); err != nil {
				lastErr = err
				excluded[account.ID] = true
				h.handleAccountFailure(account, err)
				h.dropAffinity(req.apiKeyID, account.ID)
				continue
			}

			done, retry, err := h.runUpstreamAttempt(ctx, req, account, model, reqStart)
			if done {
				return
			}
			lastErr = err
			if !retry {
				break
			}
			excluded[account.ID] = true
		}

		if fb := h.takeModelFallback(ctx, w, req.payload, model, lastErr); fb != "" {
			model, excluded = fb, h.routingExclusions(req.apiKeyID)
			continue
		}
		break
	}

	if lastErr == nil {
		req.failNoAccounts(model)
		return
	}
	h.recordFailureWithDetails(ctx, req.endpoint, model, "", lastErr)
	req.fail(model, lastErr)
}

// runUpstreamAttempt runs one attempt on account. done means the request is
// finished (success, client gone, or an in-stream error was written); retry
// means another account may be tried; err is the attempt's failure.
func (h *Handler) runUpstreamAttempt(ctx context.Context, req *upstreamRequest, account *config.Account, model string, reqStart time.Time) (done, retry bool, err error) {
	upstreamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a := &upstreamAttempt{
		ctx:        upstreamCtx,
		cancel:     cancel,
		account:    account,
		model:      model,
		cacheUsage: h.promptCache.Compute(account.ID, req.cacheProfile),
	}
	run := req.attempt(a)

	var canRetry func() bool
	if req.started != nil {
		canRetry = func() bool { return !req.started() }
	}
	err = upstreamAttemptRunner(upstreamCtx, account, req.payload, run.callback, run.measure, run.reset, canRetry)
	if err != nil && run.stopped != nil && run.stopped() {
		// Upstream was cancelled because a stop sequence matched.
		err = nil
	}

	if err != nil {
		if ctx.Err() != nil {
			// The client went away: not the account's fault.
			return true, false, err
		}
		started := req.started != nil && req.started()
		if !started || !req.keepAccountAfterOutput {
			// Integrity failures are upstream hiccups, not account faults.
			if !isStreamIntegrityError(err) {
				h.handleAccountFailure(account, err)
				h.dropAffinity(req.apiKeyID, account.ID)
			}
		}
		if started {
			// Bytes already reached the client: a retry would duplicate them.
			h.recordFailureWithDetails(ctx, req.endpoint, model, account.ID, err)
			req.failStarted(a, err)
			return true, false, err
		}
		if isTerminalUpstreamError(err) || stopForModelFallback(ctx, model, err) {
			return false, false, err
		}
		return false, true, err
	}

	usage := run.finish()
	h.recordSuccessForApiKey(ctx, req.apiKeyID, usage.inputTokens, usage.outputTokens, usage.credits)
	h.pool.RecordSuccess(account.ID)
	h.pool.UpdateStats(account.ID, usage.inputTokens+usage.outputTokens, usage.credits)
	h.promptCache.Update(account.ID, req.cacheProfile)
	h.recordSuccessLog(ctx, req.endpoint, model, account.ID, usage.inputTokens+usage.outputTokens, usage.credits, time.Since(reqStart).Milliseconds())
	run.respond(usage)
	return true, false, nil
}

// resolveInputTokens prefers Kiro's context-usage figure, then the upstream
// count, then the local estimate.
func resolveInputTokens(realInputTokens, upstreamInputTokens, estimated int) int {
	if realInputTokens > 0 {
		return realInputTokens
	}
	if upstreamInputTokens <= 0 {
		return estimated
	}
	return upstreamInputTokens
}

// routingExclusions returns the starting exclusion set for a request.
func (h *Handler) routingExclusions(apiKeyID string) map[string]bool {
	return make(map[string]bool)
}
