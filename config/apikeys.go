package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// API keys are stored hashed: an entry keeps the hex SHA-256 of its key
// (KeyHash) and a short display hint (KeyHint), never the key itself. The
// full key is shown once, when it is created or regenerated. Requests are
// authenticated by hashing the presented key and looking the hash up in
// apiKeyIndex.

// apiKeyIndex maps KeyHash to the position in cfg.ApiKeys. Guarded by cfgLock
// and rebuilt by reindexApiKeysLocked whenever the list changes.
var apiKeyIndex map[string]int

// HashApiKey returns the hex SHA-256 of a key value.
func HashApiKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ApiKeyHint returns a display hint for a key: its first 7 and last 4
// characters ("sk-ab12…9f3c"). Shorter custom keys reveal less: a few
// trailing characters, or nothing.
func ApiKeyHint(key string) string {
	n := len(key)
	switch {
	case n == 0:
		return ""
	case n >= 20:
		return key[:7] + "…" + key[n-4:]
	case n >= 12:
		return "…" + key[n-4:]
	case n >= 8:
		return "…" + key[n-2:]
	}
	return "…"
}

// hashApiKeyEntry moves a plaintext Key into KeyHash/KeyHint and clears it.
// It reports whether the entry changed. Encrypted values are left alone (the
// stores decrypt before this runs).
func hashApiKeyEntry(k *ApiKeyEntry) bool {
	key := strings.TrimSpace(k.Key)
	if k.Key == "" || IsEncryptedValue(key) {
		return false
	}
	k.Key = ""
	if key != "" {
		k.KeyHash = HashApiKey(key)
		k.KeyHint = ApiKeyHint(key)
	}
	return true
}

// ApiKeyDigest returns the hash identifying an entry's key: KeyHash, or the
// hash of a not yet migrated plaintext Key ("" when neither is known).
func ApiKeyDigest(k ApiKeyEntry) string {
	if k.KeyHash != "" {
		return k.KeyHash
	}
	if key := strings.TrimSpace(k.Key); key != "" && !IsEncryptedValue(key) {
		return HashApiKey(key)
	}
	return ""
}

// migrateApiKeysLocked hashes every plaintext key and drops the legacy
// single-key field. Reports whether anything changed. Caller holds cfgLock.
func migrateApiKeysLocked() bool {
	changed := false
	if cfg.ApiKey != "" {
		cfg.ApiKey = ""
		changed = true
	}
	for i := range cfg.ApiKeys {
		if hashApiKeyEntry(&cfg.ApiKeys[i]) {
			changed = true
		}
	}
	reindexApiKeysLocked()
	return changed
}

func reindexApiKeysLocked() {
	idx := make(map[string]int, len(cfg.ApiKeys))
	for i := range cfg.ApiKeys {
		if h := cfg.ApiKeys[i].KeyHash; h != "" {
			if _, dup := idx[h]; !dup {
				idx[h] = i
			}
		}
	}
	apiKeyIndex = idx
}

// setLegacyApiKeyLocked handles the deprecated single "apiKey" setting: a
// non-empty value becomes a hashed key entry instead of being stored in
// plaintext. Caller holds cfgLock.
func setLegacyApiKeyLocked(key string) {
	cfg.ApiKey = ""
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	h := HashApiKey(key)
	if _, exists := apiKeyIndex[h]; exists {
		return
	}
	cfg.ApiKeys = append(cfg.ApiKeys, ApiKeyEntry{
		ID: newUUID(), Name: "legacy", KeyHash: h, KeyHint: ApiKeyHint(key),
		Enabled: true, Migrated: true, CreatedAt: time.Now().Unix(),
	})
	reindexApiKeysLocked()
}

// ListApiKeys returns a snapshot of all configured API key entries.
func ListApiKeys() []ApiKeyEntry {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil
	}
	out := make([]ApiKeyEntry, len(cfg.ApiKeys))
	copy(out, cfg.ApiKeys)
	return out
}

// GetApiKeyEntry returns a copy of the entry with the given ID, or nil if not found.
func GetApiKeyEntry(id string) *ApiKeyEntry {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return nil
	}
	for i := range cfg.ApiKeys {
		if cfg.ApiKeys[i].ID == id {
			cp := cfg.ApiKeys[i]
			return &cp
		}
	}
	return nil
}

// AddApiKey appends a new API key entry. Generates ID and CreatedAt if missing,
// rejects empty Key values, and refuses duplicates of an existing key. The key
// is stored hashed: the returned entry has KeyHash/KeyHint and no Key.
func AddApiKey(entry ApiKeyEntry) (ApiKeyEntry, error) {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return ApiKeyEntry{}, errors.New("config not initialized")
	}
	entry.Key = strings.TrimSpace(entry.Key)
	if entry.Key == "" {
		return ApiKeyEntry{}, errors.New("api key value must not be empty")
	}
	hashApiKeyEntry(&entry)
	if _, exists := apiKeyIndex[entry.KeyHash]; exists {
		return ApiKeyEntry{}, errors.New("api key already exists")
	}
	applyApiKeyRestrictions(&entry, entry)
	if entry.ID == "" {
		entry.ID = newUUID()
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	cfg.ApiKeys = append(cfg.ApiKeys, entry)
	if err := saveLocked(); err != nil {
		// Roll back the in-memory append so we don't leave inconsistent state.
		cfg.ApiKeys = cfg.ApiKeys[:len(cfg.ApiKeys)-1]
		return ApiKeyEntry{}, err
	}
	reindexApiKeysLocked()
	return entry, nil
}

// UpdateApiKey applies a patch to an existing API key. Patch semantics:
//   - Name, Key are overwritten when non-empty in patch (a new Key is hashed).
//   - Enabled, TokenLimit, CreditLimit are always overwritten (zero values are valid).
//   - Counters (TokensUsed/CreditsUsed/RequestsCount) are not touched here; use
//     RecordApiKeyUsage or ResetApiKeyUsage instead.
//   - Migrated stays as-is once true; only flips when explicitly set in patch.
func UpdateApiKey(id string, patch ApiKeyEntry) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	idx := -1
	for i := range cfg.ApiKeys {
		if cfg.ApiKeys[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errors.New("api key not found")
	}
	if patch.Name != "" {
		cfg.ApiKeys[idx].Name = patch.Name
	}
	if newKey := strings.TrimSpace(patch.Key); newKey != "" {
		h := HashApiKey(newKey)
		// Reject duplicates against any other entry.
		if j, exists := apiKeyIndex[h]; exists && j != idx {
			return errors.New("api key value collides with existing entry")
		}
		cfg.ApiKeys[idx].Key = ""
		cfg.ApiKeys[idx].KeyHash = h
		cfg.ApiKeys[idx].KeyHint = ApiKeyHint(newKey)
		reindexApiKeysLocked()
	}
	cfg.ApiKeys[idx].Enabled = patch.Enabled
	cfg.ApiKeys[idx].TokenLimit = patch.TokenLimit
	cfg.ApiKeys[idx].CreditLimit = patch.CreditLimit
	applyApiKeyRestrictions(&cfg.ApiKeys[idx], patch)
	cfg.ApiKeys[idx].DailyCreditLimit = patch.DailyCreditLimit
	if patch.Migrated {
		cfg.ApiKeys[idx].Migrated = true
	}
	return saveLocked()
}

// DeleteApiKey removes the API key entry with the given ID. Returns nil even if
// the ID is unknown (idempotent), matching the existing DeleteAccount style.
func DeleteApiKey(id string) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	for i, e := range cfg.ApiKeys {
		if e.ID == id {
			cfg.ApiKeys = append(cfg.ApiKeys[:i], cfg.ApiKeys[i+1:]...)
			reindexApiKeysLocked()
			return saveLocked()
		}
	}
	return nil
}

// FindApiKeyByValue returns a copy of the entry whose key matches the given
// value, or nil if no match. The value is hashed and looked up by hash, so
// the work done does not depend on which entry (if any) matches or on how
// much of a key was right.
func FindApiKeyByValue(key string) *ApiKeyEntry {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil || key == "" {
		return nil
	}
	h := HashApiKey(key)
	i, ok := apiKeyIndex[h]
	if !ok || i >= len(cfg.ApiKeys) || cfg.ApiKeys[i].KeyHash != h {
		return nil
	}
	cp := cfg.ApiKeys[i]
	return &cp
}

// HasApiKeys returns true when at least one API key entry is configured.
func HasApiKeys() bool {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if cfg == nil {
		return false
	}
	return len(cfg.ApiKeys) > 0
}

// RecordApiKeyUsage atomically adds tokens and credits to the entry's counters,
// updates LastUsedAt, increments RequestsCount, and persists.
func RecordApiKeyUsage(id string, tokens int64, credits float64) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	for i := range cfg.ApiKeys {
		if cfg.ApiKeys[i].ID == id {
			if tokens > 0 {
				cfg.ApiKeys[i].TokensUsed += tokens
			}
			if credits > 0 {
				cfg.ApiKeys[i].CreditsUsed += credits
			}
			cfg.ApiKeys[i].RequestsCount++
			cfg.ApiKeys[i].LastUsedAt = time.Now().Unix()
			scheduleSaveLocked()
			return nil
		}
	}
	return errors.New("api key not found")
}

// ResetApiKeyUsage clears TokensUsed/CreditsUsed/RequestsCount for the entry.
// LastUsedAt is preserved so operators can still see when the key was last used.
func ResetApiKeyUsage(id string) error {
	cfgLock.Lock()
	defer cfgLock.Unlock()
	if cfg == nil {
		return errors.New("config not initialized")
	}
	for i := range cfg.ApiKeys {
		if cfg.ApiKeys[i].ID == id {
			cfg.ApiKeys[i].TokensUsed = 0
			cfg.ApiKeys[i].CreditsUsed = 0
			cfg.ApiKeys[i].RequestsCount = 0
			return saveLocked()
		}
	}
	return errors.New("api key not found")
}

// GenerateApiKeyValue returns a new random 32-byte hex API key prefixed with "sk-".
func GenerateApiKeyValue() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return "sk-" + hex.EncodeToString(buf)
}

// ApiKeyOverLimit returns (overToken, overCredit) for the entry. Limits with value 0
// are ignored. The function does not lock; callers should pass a copied entry.
func ApiKeyOverLimit(e ApiKeyEntry) (overToken bool, overCredit bool) {
	if e.TokenLimit > 0 && e.TokensUsed >= e.TokenLimit {
		overToken = true
	}
	if e.CreditLimit > 0 && e.CreditsUsed >= e.CreditLimit {
		overCredit = true
	}
	return
}
