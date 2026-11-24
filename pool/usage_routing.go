package pool

import (
	"strings"
	"sync/atomic"
	"time"

	"kiro-go/config"
)

// Usage-aware routing.
//
// Kiro reports each account's usage for the current period (refreshed in the
// background, see proxy/usage_refresh.go). Selection prefers accounts with
// headroom:
//
//   - an account whose usage is at or above the routing threshold (default
//     0.97 of its limit, config usageRoutingThreshold / KIRO_USAGE_THRESHOLD)
//     is "saturated" and only used when no other account is eligible;
//   - among the others, accounts in a lower usage band (10 percentage points
//     wide) win; within a band the weighted round-robin order decides, so
//     traffic still spreads instead of piling onto the single emptiest
//     account between two usage refreshes.
//
// Credits reported by each successful request are added to a local estimate
// until the next refresh, so an account's band moves without upstream calls.
// Quota blocking (isQuotaBlocked) and the overage settings are unchanged.

const usageBandWidth = 0.10

// usageEstimate is the credits spent on an account since its last usage
// refresh (identified by Account.LastRefresh).
type usageEstimate struct {
	refresh int64
	credits float64
}

// AccountUsage returns an account's reported usage and limit for the current
// period. Trial credits that are still active count towards both.
func AccountUsage(acc *config.Account) (used, limit float64) {
	used, limit = acc.UsageCurrent, acc.UsageLimit
	if acc.TrialUsageLimit > 0 && trialActive(acc) {
		used += acc.TrialUsageCurrent
		limit += acc.TrialUsageLimit
	}
	return used, limit
}

// usageRatio is the account's estimated share of its period limit (reported
// usage plus the local estimate), and whether a limit is known.
func (p *AccountPool) usageRatio(acc *config.Account) (float64, bool) {
	used, limit := AccountUsage(acc)
	if limit <= 0 {
		return 0, false
	}
	if est, ok := p.usageLocal[acc.ID]; ok && est.refresh == acc.LastRefresh {
		used += est.credits
	}
	return used / limit, true
}

func trialActive(acc *config.Account) bool {
	switch strings.ToUpper(strings.TrimSpace(acc.TrialStatus)) {
	case "EXPIRED", "NONE":
		return false
	}
	return acc.TrialExpiresAt == 0 || time.Now().Unix() < acc.TrialExpiresAt
}

// usageBand ranks an account for selection: lower is better. Saturated
// accounts get a band above every unsaturated one.
func (p *AccountPool) usageBand(acc *config.Account, threshold float64) int {
	ratio, ok := p.usageRatio(acc)
	if !ok {
		return 0
	}
	if ratio >= threshold {
		return 1 << 20
	}
	if ratio < 0 {
		ratio = 0
	}
	return int(ratio / usageBandWidth)
}

// isSaturatedLocked reports whether the account is at or above the routing
// threshold.
func (p *AccountPool) isSaturatedLocked(acc *config.Account, threshold float64) bool {
	ratio, ok := p.usageRatio(acc)
	return ok && ratio >= threshold
}

// selectLocked runs one round of weighted round-robin selection over the
// eligible accounts, preferring the lowest usage band. checkModel enables
// the per-account model filter. Caller holds p.mu (read).
func (p *AccountPool) selectLocked(model string, checkModel bool, excluded map[string]bool) *config.Account {
	n := len(p.accounts)
	if n == 0 {
		return nil
	}
	allowOverUsage := config.GetAllowOverUsage()
	threshold := config.GetUsageRoutingThreshold()
	now := time.Now()
	start := atomic.AddUint64(&p.currentIndex, 1)
	var best *config.Account
	bestBand := 0
	seen := make(map[string]bool, n) // weighted accounts appear several times
	for i := 0; i < n; i++ {
		acc := &p.accounts[(start+uint64(i))%uint64(n)]
		if seen[acc.ID] {
			continue
		}
		seen[acc.ID] = true
		if !p.eligibleLocked(acc, model, checkModel, excluded, allowOverUsage, now) {
			continue
		}
		band := p.usageBand(acc, threshold)
		if best == nil || band < bestBand {
			best, bestBand = acc, band
		}
	}
	return best
}

func (p *AccountPool) eligibleLocked(acc *config.Account, model string, checkModel bool, excluded map[string]bool, allowOverUsage bool, now time.Time) bool {
	if excluded != nil && excluded[acc.ID] {
		return false
	}
	if checkModel && !p.accountHasModel(acc.ID, model) {
		return false
	}
	if cooldown, ok := p.cooldowns[acc.ID]; ok && now.Before(cooldown) {
		return false
	}
	if acc.ExpiresAt > 0 && now.Unix() > acc.ExpiresAt-tokenRefreshSkewSeconds {
		return false
	}
	return !isQuotaBlocked(*acc, allowOverUsage)
}

// hasUnsaturatedAlternativeLocked reports whether another eligible account
// below the routing threshold exists. Caller holds p.mu (read).
func (p *AccountPool) hasUnsaturatedAlternativeLocked(skipID, model string, excluded map[string]bool, threshold float64) bool {
	allowOverUsage := config.GetAllowOverUsage()
	now := time.Now()
	for i := range p.accounts {
		acc := &p.accounts[i]
		if acc.ID == skipID {
			continue
		}
		if p.eligibleLocked(acc, model, true, excluded, allowOverUsage, now) && !p.isSaturatedLocked(acc, threshold) {
			return true
		}
	}
	return false
}

// recordUsageLocked adds a request's credits to the account's local usage
// estimate. Caller holds p.mu (write).
func (p *AccountPool) recordUsageLocked(acc *config.Account, credits float64) {
	if credits <= 0 {
		return
	}
	if p.usageLocal == nil {
		p.usageLocal = map[string]usageEstimate{}
	}
	est := p.usageLocal[acc.ID]
	if est.refresh != acc.LastRefresh {
		est = usageEstimate{refresh: acc.LastRefresh}
	}
	est.credits += credits
	p.usageLocal[acc.ID] = est
}

// LocalUsageCredits returns the credits spent on an account since the usage
// refresh identified by lastRefresh (0 once a newer refresh arrived).
func (p *AccountPool) LocalUsageCredits(id string, lastRefresh int64) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if est, ok := p.usageLocal[id]; ok && est.refresh == lastRefresh {
		return est.credits
	}
	return 0
}
