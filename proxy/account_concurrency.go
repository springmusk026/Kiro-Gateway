package proxy

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Optional per-account concurrency cap (KIRO_MAX_CONCURRENT_PER_ACCOUNT,
// 0 = unlimited). Claude Code fans out subagents in parallel; with a cap,
// extra requests for the same account queue (bounded by the client's request
// context) instead of hitting Kiro all at once.
func maxConcurrentPerAccount() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("KIRO_MAX_CONCURRENT_PER_ACCOUNT"))); err == nil && v > 0 {
		return v
	}
	return 0
}

var accountSlots sync.Map // accountID → chan struct{}

// acquireAccountSlot blocks until the account has a free slot or ctx ends.
// It returns a release function (a no-op when no cap is configured).
func acquireAccountSlot(ctx context.Context, accountID string) (func(), error) {
	limit := maxConcurrentPerAccount()
	if limit <= 0 || accountID == "" {
		return func() {}, nil
	}
	v, _ := accountSlots.LoadOrStore(accountID, make(chan struct{}, limit))
	slots := v.(chan struct{})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case slots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-slots }) }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}
