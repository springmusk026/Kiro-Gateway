package config

import (
	"slices"
	"strings"
)

// Per-key routing rules (optional): AccountIDs pins a key to a set of
// accounts, and ModelMap replaces a client model name with another model for
// that key. Both are empty by default, meaning "all accounts" and "models as
// sent".

// NormalizeAccountIDs trims and de-duplicates account IDs, dropping empty
// ones. An empty result means "all accounts".
func NormalizeAccountIDs(in []string) []string {
	var out []string
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// NormalizeModelMap trims names and lower-cases the client-side keys (they
// match case-insensitively), dropping entries with an empty side.
func NormalizeModelMap(in map[string]string) map[string]string {
	var out map[string]string
	for from, to := range in {
		from, to = strings.ToLower(strings.TrimSpace(from)), strings.TrimSpace(to)
		if from == "" || to == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(in))
		}
		out[from] = to
	}
	return out
}

// applyApiKeyRouting copies the normalized routing fields from src into dst.
func applyApiKeyRouting(dst *ApiKeyEntry, src ApiKeyEntry) {
	dst.AccountIDs = NormalizeAccountIDs(src.AccountIDs)
	dst.ModelMap = NormalizeModelMap(src.ModelMap)
}

// ApiKeyRouting returns the routing rules of the key with the given ID
// (copies; nil when the key has none or does not exist).
func ApiKeyRouting(id string) (accountIDs []string, modelMap map[string]string) {
	if id == "" {
		return nil, nil
	}
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil, nil
	}
	for i := range cfg.ApiKeys {
		if cfg.ApiKeys[i].ID != id {
			continue
		}
		e := &cfg.ApiKeys[i]
		if len(e.AccountIDs) > 0 {
			accountIDs = slices.Clone(e.AccountIDs)
		}
		if len(e.ModelMap) > 0 {
			modelMap = make(map[string]string, len(e.ModelMap))
			for k, v := range e.ModelMap {
				modelMap[k] = v
			}
		}
		return accountIDs, modelMap
	}
	return nil, nil
}

// MapApiKeyModel returns the model the key maps model to, or model itself.
func MapApiKeyModel(modelMap map[string]string, model string) string {
	if to, ok := modelMap[strings.ToLower(strings.TrimSpace(model))]; ok {
		return to
	}
	return model
}

// AccountIDs lists the IDs of every configured account.
func AccountIDs() []string {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Accounts))
	for i := range cfg.Accounts {
		out = append(out, cfg.Accounts[i].ID)
	}
	return out
}
