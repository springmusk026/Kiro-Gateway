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
