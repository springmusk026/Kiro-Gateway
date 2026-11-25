package proxy

import (
	"context"
	"fmt"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// Per-key routing rules (config/apikey_routing.go).
//
// A key pinned to accounts is served by those accounts only: every other
// account starts out excluded, so neither affinity, rotation, failover nor a
// model fallback can pick it (with none of them available the request fails
// as "no accounts"). A key's model map replaces the client's model name at
// the translation entry; responses still echo the name the client sent.

// routingExclusions returns the starting exclusion set for a request: the
// accounts outside the key's pinned set, or nothing when it has none.
func (h *Handler) routingExclusions(apiKeyID string) map[string]bool {
	excluded := make(map[string]bool)
	allowed, _ := config.ApiKeyRouting(apiKeyID)
	if len(allowed) == 0 {
		return excluded
	}
	for _, id := range config.AccountIDs() {
		excluded[id] = true
	}
	for _, id := range allowed {
		delete(excluded, id)
	}
	return excluded
}

// mapKeyModel applies the request's key model map to model.
func mapKeyModel(ctx context.Context, model string) string {
	_, modelMap := config.ApiKeyRouting(apiKeyIDFromContext(ctx))
	if len(modelMap) == 0 {
		return model
	}
	return config.MapApiKeyModel(modelMap, model)
}

// validateApiKeyRouting rejects pinned account IDs that do not exist.
func validateApiKeyRouting(e config.ApiKeyEntry) error {
	ids := config.NormalizeAccountIDs(e.AccountIDs)
	if len(ids) == 0 {
		return nil
	}
	known := make(map[string]bool)
	for _, id := range config.AccountIDs() {
		known[id] = true
	}
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("unknown account id %q in accountIds", id)
		}
	}
	return nil
}
