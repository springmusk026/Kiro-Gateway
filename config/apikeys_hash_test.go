package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfigFile writes a raw config.json for a migration test.
func writeConfigFile(t *testing.T, v interface{}) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertNoKeyAtRest(t *testing.T, path string, values ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if strings.Contains(string(data), v) {
			t.Fatalf("key value %q still stored:\n%s", v, data)
		}
	}
	if strings.Contains(string(data), `"key"`) {
		t.Fatalf("a key field is still stored:\n%s", data)
	}
}

func TestApiKeyMigrationFromPlaintext(t *testing.T) {
	withCodec(t, PlaintextSecretCodec())
	path := writeConfigFile(t, map[string]interface{}{
		"password": "admin-pass-123",
		"apiKeys": []map[string]interface{}{
			{"id": "k1", "name": "gen", "key": "sk-ab12cdef0123456789abcdef9f3c", "enabled": true},
			{"id": "k2", "name": "custom", "key": "my-own-key-1234", "enabled": true},
		},
	})
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	assertNoKeyAtRest(t, path, "sk-ab12cdef0123456789abcdef9f3c", "my-own-key-1234")
	keys := ListApiKeys()
	if keys[0].KeyHint != "sk-ab12…9f3c" || keys[0].KeyHash != HashApiKey("sk-ab12cdef0123456789abcdef9f3c") || keys[0].Key != "" {
		t.Fatalf("unexpected migrated entry: %+v", keys[0])
	}
	for _, v := range []string{"sk-ab12cdef0123456789abcdef9f3c", "my-own-key-1234"} {
		if FindApiKeyByValue(v) == nil {
			t.Fatalf("%s does not authenticate after migration", v)
		}
	}
	// Reloading the migrated file keeps working and changes nothing.
	before, _ := os.ReadFile(path)
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a migrated config was rewritten on load")
	}
	if e := FindApiKeyByValue("my-own-key-1234"); e == nil || e.ID != "k2" {
		t.Fatalf("lookup after reload: %+v", e)
	}
}

func TestApiKeyMigrationFromEncrypted(t *testing.T) {
	codec := testCodec(t, 3)
	enc, err := codec.EncryptString("sk-encrypted-key-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	withCodec(t, codec)
	path := writeConfigFile(t, map[string]interface{}{
		"password": enc, // any encrypted value; settings secrets stay encrypted
		"apiKeys":  []map[string]interface{}{{"id": "k1", "key": enc, "enabled": true}},
	})
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var stored struct {
		ApiKeys []map[string]interface{} `json:"apiKeys"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if _, has := stored.ApiKeys[0]["key"]; has {
		t.Fatalf("encrypted key value still stored: %v", stored.ApiKeys[0])
	}
	if stored.ApiKeys[0]["keyHash"] != HashApiKey("sk-encrypted-key-000000000001") {
		t.Fatalf("hash not stored: %v", stored.ApiKeys[0])
	}
	if FindApiKeyByValue("sk-encrypted-key-000000000001") == nil {
		t.Fatal("migrated encrypted key does not authenticate")
	}
}

func TestApiKeyLookupAndRegenerate(t *testing.T) {
	withCodec(t, PlaintextSecretCodec())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	a, err := AddApiKey(ApiKeyEntry{Name: "a", Key: "sk-aaaaaaaaaaaaaaaaaaaaaaaa", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := AddApiKey(ApiKeyEntry{Name: "b", Key: "sk-bbbbbbbbbbbbbbbbbbbbbbbb", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.Key != "" || a.KeyHash == "" {
		t.Fatalf("AddApiKey returned a key value: %+v", a)
	}
	if e := FindApiKeyByValue("sk-bbbbbbbbbbbbbbbbbbbbbbbb"); e == nil || e.ID != b.ID {
		t.Fatalf("lookup b: %+v", e)
	}
	for _, wrong := range []string{"sk-aaaaaaaaaaaaaaaaaaaaaaa", "sk-aaaaaaaaaaaaaaaaaaaaaaaaa", a.KeyHash, "x"} {
		if FindApiKeyByValue(wrong) != nil {
			t.Fatalf("%q matched", wrong)
		}
	}
	// Collisions are refused, by hash.
	if err := UpdateApiKey(b.ID, ApiKeyEntry{Key: "sk-aaaaaaaaaaaaaaaaaaaaaaaa", Enabled: true}); err == nil {
		t.Fatal("collision accepted")
	}
	// Regenerating replaces the hash: the old key stops working.
	if err := UpdateApiKey(a.ID, ApiKeyEntry{Key: "sk-cccccccccccccccccccccccc", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if FindApiKeyByValue("sk-aaaaaaaaaaaaaaaaaaaaaaaa") != nil {
		t.Fatal("old key still works")
	}
	if e := FindApiKeyByValue("sk-cccccccccccccccccccccccc"); e == nil || e.ID != a.ID {
		t.Fatalf("new key: %+v", e)
	}
	// Deleting shifts positions; the index follows.
	if err := DeleteApiKey(a.ID); err != nil {
		t.Fatal(err)
	}
	if e := FindApiKeyByValue("sk-bbbbbbbbbbbbbbbbbbbbbbbb"); e == nil || e.ID != b.ID {
		t.Fatalf("lookup after delete: %+v", e)
	}
	assertNoKeyAtRest(t, path, "sk-bbbbbbbbbbbbbbbbbbbbbbbb", "sk-cccccccccccccccccccccccc")
}

func TestLegacyApiKeySettingIsHashed(t *testing.T) {
	withCodec(t, PlaintextSecretCodec())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSettings("legacy-key-123", true, ""); err != nil {
		t.Fatal(err)
	}
	if GetApiKey() != "" || FindApiKeyByValue("legacy-key-123") == nil {
		t.Fatal("legacy setting not converted into a hashed key")
	}
	assertNoKeyAtRest(t, path, "legacy-key-123")
}

func TestEncryptApiKeyNeverWritesValue(t *testing.T) {
	for _, c := range []*SecretCodec{PlaintextSecretCodec(), testCodec(t, 5)} {
		out, err := c.EncryptApiKey(ApiKeyEntry{ID: "k", Key: "sk-plain-value-0000000000"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Key != "" || out.KeyHash != HashApiKey("sk-plain-value-0000000000") {
			t.Fatalf("stored entry: %+v", out)
		}
	}
	// An encrypted value is hashed when the key is available.
	c := testCodec(t, 6)
	enc, _ := c.EncryptString("sk-enc-value-000000000000")
	out, err := c.EncryptApiKey(ApiKeyEntry{ID: "k", Key: enc})
	if err != nil || out.Key != "" || out.KeyHash != HashApiKey("sk-enc-value-000000000000") {
		t.Fatalf("encrypted entry: %+v %v", out, err)
	}
}
