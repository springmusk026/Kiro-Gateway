package config

import (
	"errors"
	"strings"
	"time"
)

// Per-key access restrictions: which models a key may use, how many requests
// per minute it may make, and when it stops working.

// NormalizeAllowedModels trims, lower-cases and de-duplicates model patterns,
// dropping empty entries. An empty result means "all models".
func NormalizeAllowedModels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ValidateApiKeyRestrictions checks the restriction fields of an entry.
func ValidateApiKeyRestrictions(e ApiKeyEntry) error {
	if e.RPMLimit < 0 {
		return errors.New("rpmLimit must be 0 (unlimited) or positive")
	}
	if e.ExpiresAt < 0 {
		return errors.New("expiresAt must be 0 (never) or a Unix timestamp")
	}
	return nil
}

// applyApiKeyRestrictions copies the (normalized) restriction fields from src
// into dst. Caller holds cfgLock when dst lives in cfg.
func applyApiKeyRestrictions(dst *ApiKeyEntry, src ApiKeyEntry) {
	dst.AllowedModels = NormalizeAllowedModels(src.AllowedModels)
	dst.RPMLimit = max(src.RPMLimit, 0)
	dst.ExpiresAt = max(src.ExpiresAt, 0)
	applyApiKeyRouting(dst, src)
}

// ApiKeyExpired reports whether the entry has an expiry in the past.
func ApiKeyExpired(e ApiKeyEntry, now time.Time) bool {
	return e.ExpiresAt > 0 && now.Unix() >= e.ExpiresAt
}

// ModelPatternMatches reports whether model (any case) matches one allowed
// pattern: an exact ID, or a prefix ending in "*" ("claude-sonnet-*"). A lone
// "*" matches everything.
func ModelPatternMatches(pattern, model string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	model = strings.ToLower(strings.TrimSpace(model))
	if pattern == "" || model == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(model, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == model
}

// ApiKeyAllowsModel reports whether any of the candidate model names (the
// requested name, its resolved upstream ID, …) is allowed for the entry.
func ApiKeyAllowsModel(e ApiKeyEntry, candidates ...string) bool {
	if len(e.AllowedModels) == 0 {
		return true
	}
	for _, p := range e.AllowedModels {
		for _, c := range candidates {
			if ModelPatternMatches(p, c) {
				return true
			}
		}
	}
	return false
}
