package pool

import "time"

// CoolingIDs returns the IDs of accounts that are cooling down (throttled or
// parked) right now.
func (p *AccountPool) CoolingIDs() map[string]bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	out := make(map[string]bool)
	for id, until := range p.cooldowns {
		if now.Before(until) {
			out[id] = true
		}
	}
	return out
}

// CooldownsUntil returns, for each account cooling down right now (throttled,
// parked until a quota reset, or disabled), when its cooldown ends.
func (p *AccountPool) CooldownsUntil() map[string]time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	out := make(map[string]time.Time)
	for id, until := range p.cooldowns {
		if now.Before(until) {
			out[id] = until
		}
	}
	return out
}
