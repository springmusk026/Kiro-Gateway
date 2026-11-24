package proxy

import (
	"hash/fnv"
	"strconv"
	"time"

	"kiro-go/config"
	"kiro-go/logger"
)

// Periodic usage refresh for usage-aware routing.
//
// Routing ranks accounts by the usage Kiro reports (getUsageLimits). The
// 30-minute background refresh is too coarse for that, so this loop refreshes
// each account's usage once its data is older than 10 minutes plus a per-
// account jitter of up to 5 minutes, one account at a time, outside the
// request path. A quota error on the request path only flags the account; the
// loop refreshes it on its next tick (at most once per usageForcedMinGap).

const (
	usageRefreshTick    = time.Minute
	usageRefreshMinAge  = 10 * time.Minute
	usageRefreshJitter  = 5 * time.Minute
	usageForcedMinGap   = 2 * time.Minute
	usageRefreshSpacing = 500 * time.Millisecond
)

// usageRefreshDue reports whether an account's usage data is stale.
func usageRefreshDue(acc *config.Account, now time.Time) bool {
	if acc.LastRefresh <= 0 {
		return true
	}
	return now.Sub(time.Unix(acc.LastRefresh, 0)) >= usageRefreshMinAge+usageJitter(acc.ID, acc.LastRefresh)
}

// usageJitter spreads refreshes of different accounts (and of successive
// periods of one account) across the jitter window, deterministically so the
// due check is stable between ticks.
func usageJitter(id string, lastRefresh int64) time.Duration {
	h := fnv.New32a()
	h.Write([]byte(id))
	h.Write([]byte(strconv.FormatInt(lastRefresh, 10)))
	return time.Duration(h.Sum32()%uint32(usageRefreshJitter/time.Second)) * time.Second
}

// requestUsageRefresh asks the loop to refresh an account soon (after a
// quota error). Repeated requests within usageForcedMinGap are ignored.
func (h *Handler) requestUsageRefresh(accountID string) {
	if accountID == "" {
		return
	}
	now := time.Now()
	if v, ok := h.cost.usageForced.Load(accountID); ok {
		if last, _ := v.(time.Time); now.Sub(last) < usageForcedMinGap {
			return
		}
	}
	h.cost.usageForced.Store(accountID, now)
	h.cost.usageForcedPending.Store(accountID, true)
}

func (h *Handler) usageRefreshLoop() {
	ticker := time.NewTicker(usageRefreshTick)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.refreshDueUsage(time.Now())
		case <-h.stopRefresh:
			return
		}
	}
}

// refreshDueUsage refreshes the usage of every enabled account whose data is
// stale or that a quota error flagged.
func (h *Handler) refreshDueUsage(now time.Time) int {
	refreshed := 0
	accounts := config.GetAccounts()
	for i := range accounts {
		acc := &accounts[i]
		if !acc.Enabled || accountBearerToken(acc) == "" {
			continue
		}
		_, forced := h.cost.usageForcedPending.LoadAndDelete(acc.ID)
		if !forced && !usageRefreshDue(acc, now) {
			continue
		}
		if refreshed > 0 {
			select {
			case <-h.stopRefresh:
				return refreshed
			case <-time.After(usageRefreshSpacing):
			}
		}
		if err := h.ensureValidToken(acc); err != nil {
			logger.Debugf("[UsageRefresh] token not usable for %s: %v", acc.Email, err)
			continue
		}
		info, err := refreshAccountInfoFn(acc)
		if err != nil {
			logger.Debugf("[UsageRefresh] %s: %v", acc.Email, err)
			continue
		}
		config.UpdateAccountInfo(acc.ID, *info)
		refreshed++
	}
	if refreshed > 0 {
		h.pool.Reload()
	}
	return refreshed
}

// refreshAccountInfoFn is swapped in tests.
var refreshAccountInfoFn = RefreshAccountInfo
