package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// API keys: whatever a pre-hashing deployment stored (plaintext or encrypted
// key values), loading through config hashes them and no key value remains in
// the database; export writes the hashes; import never writes key values.
func TestApiKeysHashedAtRest(t *testing.T) {
	db := testDB(t)
	key := storageCodec(t, 4)
	ctx := context.Background()
	enc, err := key.EncryptString("sk-encrypted-value-000000001")
	if err != nil {
		t.Fatal(err)
	}
	// Rows as an older build wrote them: one plaintext and one encrypted key.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO kiro_settings (id, data, updated_at) VALUES (1, '{"password":"pw-12345678"}', now())`); err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{
		"k1": `{"id":"k1","name":"plain","key":"sk-plaintext-value-00000001","enabled":true}`,
		"k2": `{"id":"k2","name":"enc","key":"` + enc + `","enabled":true}`,
	}
	for i, id := range []string{"k1", "k2"} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO kiro_api_keys (id, position, data, updated_at) VALUES ($1, $2, $3, now())`,
			id, i, rows[id]); err != nil {
			t.Fatal(err)
		}
	}

	config.SetDefaultSecretCodec(key)
	t.Cleanup(func() { config.SetDefaultSecretCodec(config.PlaintextSecretCodec()) })
	config.UseStore(NewConfigStore(db))
	t.Cleanup(func() { config.UseStore(nil) })
	if err := config.Load(); err != nil {
		t.Fatal(err)
	}
	stored := rawRows(t, db)
	for _, v := range []string{"sk-plaintext-value-00000001", "sk-encrypted-value-000000001", enc, `"key"`} {
		if strings.Contains(stored, v) {
			t.Fatalf("%s still stored:\n%s", v, stored)
		}
	}
	for _, v := range []string{"sk-plaintext-value-00000001", "sk-encrypted-value-000000001"} {
		if config.FindApiKeyByValue(v) == nil {
			t.Fatalf("%s does not authenticate after migration", v)
		}
	}

	// Export (default and --decrypt) carries hashes and hints, not keys.
	dir := t.TempDir()
	for _, decrypt := range []bool{false, true} {
		out := filepath.Join(dir, fmt.Sprintf("export-%v.json", decrypt))
		if _, err := ExportJSON(ctx, db, out, decrypt); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(out)
		if strings.Contains(string(data), "sk-plaintext-value") || strings.Contains(string(data), "sk-encrypted-value") ||
			!strings.Contains(string(data), config.HashApiKey("sk-plaintext-value-00000001")) {
			t.Fatalf("export (decrypt=%v):\n%s", decrypt, data)
		}
		// Loading an export keeps the keys working.
		back, err := config.ReadJSONFile(out)
		if err != nil || back.ApiKeys[1].KeyHash != config.HashApiKey("sk-encrypted-value-000000001") {
			t.Fatalf("read export: %v %+v", err, back)
		}
	}

	// Importing a pre-hashing config.json never writes key values either.
	src := filepath.Join(dir, "config.json")
	os.WriteFile(src, []byte(`{"password":"pw-12345678","apiKeys":[{"id":"k9","key":"sk-imported-value-000000009","enabled":true}]}`), 0o600)
	if _, err := ImportJSON(ctx, db, src, true); err != nil {
		t.Fatal(err)
	}
	if stored := rawRows(t, db); strings.Contains(stored, "sk-imported-value") || !strings.Contains(stored, config.HashApiKey("sk-imported-value-000000009")) {
		t.Fatalf("import stored:\n%s", stored)
	}
}
