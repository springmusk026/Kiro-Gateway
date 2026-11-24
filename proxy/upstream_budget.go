package proxy

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Per-request upstream budget.
//
// One client request can fan out through several retry layers (accounts ×
// same-account stream retries × endpoints × per-endpoint attempts × protocol
// fallback). Without a cap, a failing upstream turns one request into dozens
// of rapid calls to Kiro: wasteful, and exactly the bursty pattern a
// provider's abuse detection looks for. Every real chat call to Kiro takes one
// unit from the request's budget; once it is spent the request fails instead
// of retrying further.
var errUpstreamBudgetExhausted = errors.New("upstream attempt budget exhausted for this request")

type upstreamBudgetKey struct{}

type upstreamBudget struct{ remaining atomic.Int32 }

// maxUpstreamAttempts is the per-request budget (KIRO_MAX_UPSTREAM_ATTEMPTS).
func maxUpstreamAttempts() int32 {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_MAX_UPSTREAM_ATTEMPTS"))); err == nil && v > 0 {
		return int32(v)
	}
	// Enough for one same-account retry and a switch to another account.
	return 4
}

func withUpstreamBudget(ctx context.Context, n int32) context.Context {
	b := &upstreamBudget{}
	b.remaining.Store(n)
	return context.WithValue(ctx, upstreamBudgetKey{}, b)
}

// takeUpstreamAttempt consumes one attempt. Contexts without a budget
// (internal maintenance calls, tests) are not limited here.
func takeUpstreamAttempt(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	b, ok := ctx.Value(upstreamBudgetKey{}).(*upstreamBudget)
	if !ok {
		return nil
	}
	if b.remaining.Add(-1) < 0 {
		return errUpstreamBudgetExhausted
	}
	return nil
}

// jitter spreads retry delays by ±30% so concurrent retries do not fire in
// lockstep.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := int64(d) * 3 / 10
	return d - time.Duration(spread) + time.Duration(rand.Int63n(2*spread+1))
}

// attemptsUsed reports how many upstream attempts a request consumed.
func attemptsUsed(ctx context.Context) int {
	b, ok := ctx.Value(upstreamBudgetKey{}).(*upstreamBudget)
	if !ok {
		return 0
	}
	used := int(maxUpstreamAttempts()) - int(b.remaining.Load())
	if used < 0 {
		return 0
	}
	if max := int(maxUpstreamAttempts()); used > max {
		return max
	}
	return used
}

// retryBackoff is the wait before retry number n (0-based), following the
// official Kiro client: 250 ms·2ⁿ for transient errors and 1 s·2ⁿ for
// throttling, capped at 20 s (jitter is added by waitForStreamRetry).
func retryBackoff(n int, throttled bool) time.Duration {
	d := 250 * time.Millisecond
	if throttled {
		d = time.Second
	}
	for i := 0; i < n && d < 20*time.Second; i++ {
		d *= 2
	}
	if d > 20*time.Second {
		d = 20 * time.Second
	}
	return d
}

// kiroRetryAfter extracts the upstream's retry hint: the
// x-amzn-kiro-ratelimit-retry-after header (milliseconds), a
// retryAfterMilliseconds field in the body, or a standard Retry-After header
// (seconds). Capped at 300 s like the official client.
func kiroRetryAfter(h http.Header, body []byte) time.Duration {
	var d time.Duration
	if v, err := strconv.ParseInt(strings.TrimSpace(h.Get("x-amzn-kiro-ratelimit-retry-after")), 10, 64); err == nil && v > 0 {
		d = time.Duration(v) * time.Millisecond
	} else if m := retryAfterMsPattern.FindSubmatch(body); m != nil {
		if v, err := strconv.ParseInt(string(m[1]), 10, 64); err == nil && v > 0 {
			d = time.Duration(v) * time.Millisecond
		}
	} else if v, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && v > 0 {
		d = time.Duration(v) * time.Second
	}
	if d > 300*time.Second {
		d = 300 * time.Second
	}
	return d
}

var retryAfterMsPattern = regexp.MustCompile(`"retryAfterMilliseconds"\s*:\s*(\d+)`)

// isUsageLimitBody reports a usage-limit rejection, which the official client
// never retries (only a reset or more quota helps).
func isUsageLimitBody(body []byte) bool {
	s := strings.ToUpper(string(body))
	for _, k := range []string{"MONTHLY_REQUEST_COUNT", "DAILY_REQUEST_COUNT", "WEEKLY_REQUEST_COUNT", "HOURLY_REQUEST_COUNT", "USAGE_LIMIT_REACHED"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// retryAfterTag encodes a retry hint into an error message for failover.
func retryAfterTag(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	secs := int((d + time.Second - 1) / time.Second)
	return fmt.Sprintf(" (retry-after=%d)", secs)
}
