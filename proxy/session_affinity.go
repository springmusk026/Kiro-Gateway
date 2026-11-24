package proxy

import (
	"kiro-go/config"
	"kiro-go/logger"
)

// pickAccount selects the upstream account for one attempt. With session
// affinity enabled (config.sessionAffinity or KIRO_SESSION_AFFINITY=1), an API
// key keeps returning to the account that last served it while that account
// stays eligible, which keeps Kiro's per-account prompt cache warm. Otherwise,
// or when the bound account is unavailable, it falls back to round-robin.
//
// Based on upstream PR #128 / #168 (session affinity), reworked to reuse the
// pool's own eligibility rules instead of only checking Enabled.
func (h *Handler) pickAccount(apiKeyID, model string, excluded map[string]bool) *config.Account {
	affinity := apiKeyID != "" && config.GetSessionAffinity()
	if affinity {
		if v, ok := h.sessionAffinity.Load(apiKeyID); ok {
			if acc := h.pool.GetPreferredForModel(v.(string), model, excluded); acc != nil {
				return acc
			}
			h.sessionAffinity.Delete(apiKeyID)
		}
	}
	acc := h.pool.GetNextForModelExcluding(model, excluded)
	if affinity && acc != nil {
		h.sessionAffinity.Store(apiKeyID, acc.ID)
		logger.Debugf("[Affinity] bound key %s to account %s", apiKeyID, acc.ID)
	}
	return acc
}

// dropAffinity forgets a binding after its account failed a request.
func (h *Handler) dropAffinity(apiKeyID, accountID string) {
	if apiKeyID == "" {
		return
	}
	if v, ok := h.sessionAffinity.Load(apiKeyID); ok && v.(string) == accountID {
		h.sessionAffinity.Delete(apiKeyID)
	}
}
