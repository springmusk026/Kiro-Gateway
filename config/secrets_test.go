package config

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCodec(t *testing.T, seed byte) *SecretCodec {
	t.Helper()
	c, err := NewSecretCodec(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// withCodec installs c as the default codec for the test.
func withCodec(t *testing.T, c *SecretCodec) {
	t.Helper()
	SetDefaultSecretCodec(c)
	plaintextLoaded.Store(false)
	t.Cleanup(func() { SetDefaultSecretCodec(PlaintextSecretCodec()) })
}

func secretSample() *Config {
	return &Config{
		Password: "admin-pass-123",
		Port:     8080,
		ProxyURL: "http://user:pw@proxy:3128",
		Accounts: []Account{{ID: "a1", Email: "a@x.io", AccessToken: "at-1", RefreshToken: "rt-1", ClientSecret: "cs-1", KiroApiKey: "ksk_1", Enabled: true}},
		ApiKeys:  []ApiKeyEntry{{ID: "k1", Key: "sk-secret", Enabled: true}},
	}
}

func TestParseEncryptionKey(t *testing.T) {
	raw := bytes.Repeat([]byte{7}, 32)
	for _, s := range []string{
		base64.StdEncoding.EncodeToString(raw),
		base64.RawURLEncoding.EncodeToString(raw),
		hex.EncodeToString(raw),
		"  " + base64.StdEncoding.EncodeToString(raw) + "\n",
	} {
		got, err := ParseEncryptionKey(s)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"", "short", base64.StdEncoding.EncodeToString(raw[:16])} {
		if _, err := ParseEncryptionKey(s); err == nil {
			t.Fatalf("%q accepted", s)
		}
	}
}

func TestSecretCodecRoundTrip(t *testing.T) {
	c := testCodec(t, 1)
	src := secretSample()
	enc, err := c.EncryptConfig(src)
	if err != nil {
		t.Fatal(err)
	}
	if src.Accounts[0].AccessToken != "at-1" || src.Password != "admin-pass-123" {
		t.Fatal("EncryptConfig modified its input")
	}
	for _, v := range []string{enc.Password, enc.ProxyURL, enc.Accounts[0].AccessToken, enc.Accounts[0].RefreshToken,
		enc.Accounts[0].ClientSecret, enc.Accounts[0].KiroApiKey} {
		if !IsEncryptedValue(v) {
			t.Fatalf("value not encrypted: %q", v)
		}
	}
	// API keys are stored as hashes, never as (encrypted) values.
	if enc.ApiKeys[0].Key != "" || enc.ApiKeys[0].KeyHash != HashApiKey("sk-secret") {
		t.Fatalf("api key stored as %+v", enc.ApiKeys[0])
	}
	if enc.Accounts[0].Email != "a@x.io" || enc.ApiKeys[0].ID != "k1" {
		t.Fatal("non-secret fields changed")
	}
	// Same plaintext encrypts differently each time (random nonce).
	again, _ := c.EncryptString("at-1")
	if again == enc.Accounts[0].AccessToken {
		t.Fatal("nonce reuse")
	}
	plain, err := c.DecryptConfig(enc)
	if err != nil || plain {
		t.Fatalf("decrypt: plain=%v err=%v", plain, err)
	}
	if enc.Accounts[0].RefreshToken != "rt-1" || enc.Password != "admin-pass-123" {
		t.Fatalf("round trip mismatch: %+v", enc)
	}
}

func TestSecretCodecWrongOrMissingKey(t *testing.T) {
	enc, _ := testCodec(t, 1).EncryptString("secret")
	if _, _, err := testCodec(t, 2).DecryptString(enc); !errors.Is(err, ErrEncryptionKeyWrong) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, _, err := PlaintextSecretCodec().DecryptString(enc); !errors.Is(err, ErrEncryptionKeyMissing) {
		t.Fatalf("missing key: %v", err)
	}
	if _, _, err := testCodec(t, 1).DecryptString("enc:v1:!!!"); !errors.Is(err, ErrEncryptionKeyWrong) {
		t.Fatalf("garbage: %v", err)
	}
	// Raw codec leaves ciphertext alone.
	if out, _, err := (&SecretCodec{raw: true}).DecryptString(enc); err != nil || out != enc {
		t.Fatalf("raw: %q %v", out, err)
	}
}

func TestJSONStoreEncryptsAndMigratesPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// A plaintext config written by an older build, world-readable.
	if err := (&JSONStore{Path: path, Codec: PlaintextSecretCodec()}).Save(secretSample()); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o644)

	withCodec(t, testCodec(t, 3))
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if got := Get().Accounts[0].RefreshToken; got != "rt-1" {
		t.Fatalf("in-memory value should be plaintext, got %q", got)
	}
	data, _ := os.ReadFile(path)
	for _, secret := range []string{"rt-1", "at-1", "sk-secret", "admin-pass-123", "cs-1", "ksk_1", "user:pw"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("plaintext %q still on disk after load:\n%s", secret, data)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v, want 0600", fi.Mode().Perm())
	}
	// Reload with the same key works; lookups see plaintext.
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if FindApiKeyByValue("sk-secret") == nil {
		t.Fatal("API key lookup failed after decrypting")
	}

	// Wrong key: refuse to load rather than dropping accounts.
	withCodec(t, testCodec(t, 4))
	if err := Init(path); !errors.Is(err, ErrEncryptionKeyWrong) {
		t.Fatalf("wrong key: %v", err)
	}
	// No key: refuse too.
	withCodec(t, PlaintextSecretCodec())
	if err := Init(path); !errors.Is(err, ErrEncryptionKeyMissing) {
		t.Fatalf("missing key: %v", err)
	}
}

func TestJSONStoreRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "config.json")
	if err := (&JSONStore{Path: real}).Save(secretSample()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := (&JSONStore{Path: link}).Load(); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("load through symlink: %v", err)
	}
	if err := (&JSONStore{Path: link}).Save(secretSample()); err == nil {
		t.Fatal("save through symlink accepted")
	}
}

func TestInvalidKeyNeverWritesPlaintext(t *testing.T) {
	t.Setenv(EncryptionKeyEnv, "not-a-valid-key")
	if err := InitEncryptionFromEnv(); err == nil {
		t.Fatal("invalid key accepted")
	}
	t.Cleanup(func() { SetDefaultSecretCodec(PlaintextSecretCodec()) })
	path := filepath.Join(t.TempDir(), "config.json")
	if err := (&JSONStore{Path: path}).Save(secretSample()); err == nil {
		t.Fatal("save with an invalid key succeeded")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file written")
	}
}
