package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// Encryption of credentials at rest.
//
// With KIRO_ENCRYPTION_KEY set (32 bytes, base64 or hex), the stores encrypt
// sensitive fields when they serialize the configuration and decrypt them
// when they load it, so the in-memory configuration is always plaintext.
// Gateway API keys are not encrypted but stored as SHA-256 hashes
// (apikeys.go); an encrypted key value written by an older build is
// decrypted on load and then hashed. Values are AES-256-GCM
// with a random nonce, stored as "enc:v1:" + base64(nonce|ciphertext).
// Plaintext values load fine and are encrypted on the next save; encrypted
// values without the right key are a hard error, never silently dropped.

// EncryptionKeyEnv names the environment variable holding the key.
const EncryptionKeyEnv = "KIRO_ENCRYPTION_KEY"

const encPrefix = "enc:v1:"

var (
	// ErrEncryptionKeyMissing: stored values are encrypted but no key is set.
	ErrEncryptionKeyMissing = errors.New("stored credentials are encrypted but " + EncryptionKeyEnv + " is not set")
	// ErrEncryptionKeyWrong: a stored value cannot be decrypted with the key.
	ErrEncryptionKeyWrong = errors.New("stored credentials cannot be decrypted with " + EncryptionKeyEnv + " (wrong key?)")
)

// IsEncryptedValue reports whether s carries the encrypted-value prefix.
func IsEncryptedValue(s string) bool { return strings.HasPrefix(s, encPrefix) }

// ParseEncryptionKey decodes a 32-byte key given as base64 (standard or URL,
// padded or not) or as 64 hex characters.
func ParseEncryptionKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty key")
	}
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != 32 {
				return nil, fmt.Errorf("key decodes to %d bytes, need 32 (generate one with: openssl rand -base64 32)", len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("key must be 32 bytes as base64 or hex (generate one with: openssl rand -base64 32)")
}

// SecretCodec encrypts and decrypts the sensitive configuration fields. A
// codec without a key passes plaintext through (and refuses encrypted
// values); a raw codec leaves encrypted values untouched on load.
type SecretCodec struct {
	aead cipher.AEAD
	raw  bool
	// err makes every operation fail (an invalid KIRO_ENCRYPTION_KEY), so a
	// misconfigured key never results in plaintext being written.
	err error
}

// NewSecretCodec builds a codec for a 32-byte key.
func NewSecretCodec(key []byte) (*SecretCodec, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretCodec{aead: aead}, nil
}

// PlaintextSecretCodec writes values unencrypted (export --decrypt).
func PlaintextSecretCodec() *SecretCodec { return &SecretCodec{} }

// RawSecretCodec loads encrypted values as-is (they stay ciphertext in
// memory) and, on save, encrypts only remaining plaintext with the default
// key. Used to export a configuration without decrypting it.
func RawSecretCodec() *SecretCodec {
	c := DefaultSecretCodec()
	return &SecretCodec{aead: c.aead, err: c.err, raw: true}
}

// Enabled reports whether the codec encrypts on save.
func (c *SecretCodec) Enabled() bool { return c != nil && c.aead != nil }

// EncryptString encrypts one value. Empty and already encrypted values, and
// every value when the codec has no key, are returned unchanged.
func (c *SecretCodec) EncryptString(s string) (string, error) {
	if c != nil && c.err != nil {
		return "", c.err
	}
	if !c.Enabled() || s == "" || IsEncryptedValue(s) {
		return s, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(s), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptString decrypts one value. plain reports a non-empty value that was
// stored unencrypted.
func (c *SecretCodec) DecryptString(s string) (out string, plain bool, err error) {
	if c != nil && c.err != nil {
		return "", false, c.err
	}
	if !IsEncryptedValue(s) {
		return s, s != "", nil
	}
	if c != nil && c.raw {
		return s, false, nil
	}
	if !c.Enabled() {
		return "", false, ErrEncryptionKeyMissing
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encPrefix))
	ns := c.aead.NonceSize()
	if err != nil || len(data) < ns+c.aead.Overhead() {
		return "", false, ErrEncryptionKeyWrong
	}
	pt, err := c.aead.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", false, ErrEncryptionKeyWrong
	}
	return string(pt), false, nil
}

// Sensitive fields. Keep these lists in sync when adding credentials.
func accountSecrets(a *Account) []*string {
	return []*string{&a.AccessToken, &a.RefreshToken, &a.KiroApiKey, &a.ClientSecret, &a.ProxyURL}
}

func apiKeySecrets(k *ApiKeyEntry) []*string { return []*string{&k.Key} }

func settingsSecrets(c *Config) []*string { return []*string{&c.Password, &c.ApiKey, &c.ProxyURL} }

func (c *SecretCodec) encryptFields(fields []*string) error {
	for _, f := range fields {
		v, err := c.EncryptString(*f)
		if err != nil {
			return err
		}
		*f = v
	}
	return nil
}

func (c *SecretCodec) decryptFields(fields []*string) (plain bool, err error) {
	for _, f := range fields {
		v, p, err := c.DecryptString(*f)
		if err != nil {
			return false, err
		}
		*f = v
		plain = plain || p
	}
	return plain, nil
}

// EncryptAccount returns a copy of a with its credentials encrypted.
func (c *SecretCodec) EncryptAccount(a Account) (Account, error) {
	return a, c.encryptFields(accountSecrets(&a))
}

// DecryptAccount decrypts a's credentials in place.
func (c *SecretCodec) DecryptAccount(a *Account) (plain bool, err error) {
	plain, err = c.decryptFields(accountSecrets(a))
	if err != nil {
		return false, fmt.Errorf("account %s: %w", a.ID, err)
	}
	return plain, nil
}

// EncryptApiKey returns the copy of k that is written to a store. API keys
// are stored as hashes only, so a plaintext key value is replaced by its hash
// and hint here, whatever the caller passed in. An encrypted value (a raw
// export of a configuration that was never migrated) is decrypted and hashed
// when the key is available, and kept encrypted otherwise.
func (c *SecretCodec) EncryptApiKey(k ApiKeyEntry) (ApiKeyEntry, error) {
	if c != nil && c.err != nil {
		return k, c.err
	}
	if IsEncryptedValue(k.Key) && c.Enabled() {
		dec := &SecretCodec{aead: c.aead}
		if v, _, err := dec.DecryptString(k.Key); err == nil {
			k.Key = v
		}
	}
	hashApiKeyEntry(&k)
	return k, nil
}

// DecryptApiKey decrypts k's key value in place.
func (c *SecretCodec) DecryptApiKey(k *ApiKeyEntry) (plain bool, err error) {
	plain, err = c.decryptFields(apiKeySecrets(k))
	if err != nil {
		return false, fmt.Errorf("api key %s: %w", k.ID, err)
	}
	return plain, nil
}

// EncryptSettings encrypts the top-level secrets of cfg in place (not the
// accounts or API keys).
func (c *SecretCodec) EncryptSettings(cfg *Config) error {
	return c.encryptFields(settingsSecrets(cfg))
}

// DecryptSettings decrypts the top-level secrets of cfg in place.
func (c *SecretCodec) DecryptSettings(cfg *Config) (plain bool, err error) {
	plain, err = c.decryptFields(settingsSecrets(cfg))
	if err != nil {
		return false, fmt.Errorf("settings: %w", err)
	}
	return plain, nil
}

// EncryptConfig returns a copy of cfg with every secret encrypted. cfg is
// not modified.
func (c *SecretCodec) EncryptConfig(cfg *Config) (*Config, error) {
	out := *cfg
	if err := c.EncryptSettings(&out); err != nil {
		return nil, err
	}
	if cfg.Accounts != nil {
		out.Accounts = make([]Account, len(cfg.Accounts))
		for i, a := range cfg.Accounts {
			enc, err := c.EncryptAccount(a)
			if err != nil {
				return nil, err
			}
			out.Accounts[i] = enc
		}
	}
	if cfg.ApiKeys != nil {
		out.ApiKeys = make([]ApiKeyEntry, len(cfg.ApiKeys))
		for i, k := range cfg.ApiKeys {
			enc, err := c.EncryptApiKey(k)
			if err != nil {
				return nil, err
			}
			out.ApiKeys[i] = enc
		}
	}
	return &out, nil
}

// DecryptConfig decrypts every secret of cfg in place. plain reports
// whether any secret was stored unencrypted.
func (c *SecretCodec) DecryptConfig(cfg *Config) (plain bool, err error) {
	plain, err = c.DecryptSettings(cfg)
	if err != nil {
		return false, err
	}
	for i := range cfg.Accounts {
		p, err := c.DecryptAccount(&cfg.Accounts[i])
		if err != nil {
			return false, err
		}
		plain = plain || p
	}
	for i := range cfg.ApiKeys {
		p, err := c.DecryptApiKey(&cfg.ApiKeys[i])
		if err != nil {
			return false, err
		}
		plain = plain || p
	}
	return plain, nil
}

var (
	defaultCodecMu  sync.Mutex
	defaultCodec    *SecretCodec
	defaultCodecSet bool
	// plaintextLoaded is set when a store loaded unencrypted secrets while
	// encryption is on, so Load re-saves them encrypted.
	plaintextLoaded atomic.Bool
)

// InitEncryptionFromEnv reads KIRO_ENCRYPTION_KEY and installs the default
// codec. An invalid key is an error (refuse to start); no key disables
// encryption.
func InitEncryptionFromEnv() error {
	defaultCodecMu.Lock()
	defer defaultCodecMu.Unlock()
	raw := strings.TrimSpace(os.Getenv(EncryptionKeyEnv))
	if raw == "" {
		defaultCodec, defaultCodecSet = PlaintextSecretCodec(), true
		return nil
	}
	key, err := ParseEncryptionKey(raw)
	var c *SecretCodec
	if err == nil {
		c, err = NewSecretCodec(key)
	}
	if err != nil {
		err = fmt.Errorf("%s: %w", EncryptionKeyEnv, err)
		defaultCodec, defaultCodecSet = &SecretCodec{err: err}, true
		return err
	}
	defaultCodec, defaultCodecSet = c, true
	return nil
}

// SetDefaultSecretCodec replaces the default codec (tests).
func SetDefaultSecretCodec(c *SecretCodec) {
	defaultCodecMu.Lock()
	defaultCodec, defaultCodecSet = c, c != nil
	defaultCodecMu.Unlock()
}

// DefaultSecretCodec returns the codec from KIRO_ENCRYPTION_KEY (read on
// first use). With an invalid key every encrypt/decrypt fails.
func DefaultSecretCodec() *SecretCodec {
	defaultCodecMu.Lock()
	set := defaultCodecSet
	defaultCodecMu.Unlock()
	if !set {
		_ = InitEncryptionFromEnv()
	}
	defaultCodecMu.Lock()
	defer defaultCodecMu.Unlock()
	if defaultCodec == nil {
		return PlaintextSecretCodec()
	}
	return defaultCodec
}

// EncryptionEnabled reports whether credentials are encrypted at rest.
func EncryptionEnabled() bool { return DefaultSecretCodec().Enabled() }

// NotePlaintextSecrets is called by stores that loaded unencrypted secrets.
func NotePlaintextSecrets() { plaintextLoaded.Store(true) }

func takePlaintextSecrets() bool { return plaintextLoaded.Swap(false) }

// resaveEncryptedLocked writes the configuration again when the store loaded
// plaintext credentials while encryption is on, so they are encrypted at rest
// from now on. Caller holds cfgLock.
func resaveEncryptedLocked() error {
	if !takePlaintextSecrets() || !EncryptionEnabled() {
		return nil
	}
	if err := saveLocked(); err != nil {
		return fmt.Errorf("encrypt stored credentials: %w", err)
	}
	fmt.Fprintln(os.Stderr, "config: encrypted credentials that were stored in plaintext")
	return nil
}
