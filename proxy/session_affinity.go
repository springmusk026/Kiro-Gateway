package proxy

import (
	"os"
	"strings"
	"sync"
	"time"

	"kiro-go/config"
	"kiro-go/logger"
)

// pickAccount selects the upstream account for one attempt.
//
// Kiro caches prompt prefixes per account on its side, so moving a
// conversation to another account throws its cache away and pays for the
// whole prompt again. Two affinities keep traffic where the cache is warm:
//
//   - conversation affinity (default on): every turn of one conversation goes
//     to the account that served its previous turn, while that account stays
//     eligible. Different conversations still spread across the pool.
//   - API-key affinity (opt-in, config.sessionAffinity or
//     KIRO_SESSION_AFFINITY=1): an API key sticks to one account.
//
// A bound account is reused only if it passes the pool's normal eligibility
// rules (model, cooldown, quota); otherwise the request falls back to
// round-robin and the binding moves.
func (h *Handler) pickAccount(apiKeyID, conversationID, model string, excluded map[string]bool) *config.Account {
	keyAffinity := apiKeyID != "" && config.GetSessionAffinity()
	if keyAffinity {
		if v, ok := h.sessionAffinity.Load(apiKeyID); ok {
			if acc := h.pool.GetPreferredForModel(v.(string), model, excluded); acc != nil {
				return acc
			}
			h.sessionAffinity.Delete(apiKeyID)
		}
	}
	convAffinity := conversationID != "" && conversationAffinityEnabled()
	if convAffinity {
		if id, ok := h.conversations.get(conversationID); ok {
			if acc := h.pool.GetPreferredForModel(id, model, excluded); acc != nil {
				h.conversations.put(conversationID, acc.ID)
				return acc
			}
		}
	}
	acc := h.pool.GetNextForModelExcluding(model, excluded)
	if acc != nil {
		if keyAffinity {
			h.sessionAffinity.Store(apiKeyID, acc.ID)
			logger.Debugf("[Affinity] bound key %s to account %s", apiKeyID, acc.ID)
		}
		if convAffinity {
			h.conversations.put(conversationID, acc.ID)
		}
	}
	return acc
}

func conversationAffinityEnabled() bool {
	v := strings.TrimSpace(os.Getenv("KIRO_CONVERSATION_AFFINITY"))
	return v != "0" && !strings.EqualFold(v, "false")
}

// conversationKey returns the stable conversation identity of a payload.
func conversationKey(p *KiroPayload) string {
	if p == nil {
		return ""
	}
	return p.ConversationState.ConversationID
}

// conversationBindings maps conversationID → accountID with an idle TTL and
// a size bound, so long-running gateways do not accumulate state.
type conversationBindings struct {
	mu sync.Mutex
	m  map[string]convBinding
}

type convBinding struct {
	accountID string
	lastUsed  time.Time
}

const (
	conversationAffinityTTL = time.Hour
	conversationAffinityMax = 50000
)

func (c *conversationBindings) get(conv string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[conv]
	if !ok || time.Since(b.lastUsed) > conversationAffinityTTL {
		return "", false
	}
	return b.accountID, true
}

func (c *conversationBindings) put(conv, accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]convBinding)
	}
	if len(c.m) >= conversationAffinityMax {
		now := time.Now()
		for k, b := range c.m {
			if now.Sub(b.lastUsed) > conversationAffinityTTL {
				delete(c.m, k)
			}
		}
		if len(c.m) >= conversationAffinityMax {
			// Still full of active conversations: drop an arbitrary half.
			n := 0
			for k := range c.m {
				delete(c.m, k)
				if n++; n >= conversationAffinityMax/2 {
					break
				}
			}
		}
	}
	c.m[conv] = convBinding{accountID: accountID, lastUsed: time.Now()}
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
