package proxy

import (
	"context"
	"errors"
	"math/rand"
	"os"
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
