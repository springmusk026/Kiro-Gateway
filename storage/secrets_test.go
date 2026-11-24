package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/springmusk026/Kiro-Gateway/config"
)

func storageCodec(t *testing.T, seed byte) *config.SecretCodec {
	t.Helper()
	c, err := config.NewSecretCodec(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rawRows returns every stored settings/account/key document as text.
func rawRows(t *testing.T, db *DB) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{`SELECT data::text FROM kiro_settings`, `SELECT data::text FROM kiro_accounts`, `SELECT data::text FROM kiro_api_keys`} {
		rows, err := db.Pool.Query(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			rows.Scan(&s)
			b.WriteString(s)
			b.WriteByte('\n')
		}
		rows.Close()
	}
	return b.String()
}

func TestConfigStoreEncryption(t *testing.T) {
	db := testDB(t)
	secrets := []string{`"t1"`, `"r1"`, `"sk-1"`, `"pw-12345678"`}

	// Plaintext rows from a deployment without a key.
	plain := NewConfigStore(db)
	plain.Codec = config.PlaintextSecretCodec()
	if err := plain.Save(sampleConfig()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawRows(t, db), `"r1"`) {
		t.Fatal("expected plaintext rows")
	}

	// With a key: plaintext loads, and the next save encrypts every row.
	key := storageCodec(t, 9)
	s := NewConfigStore(db)
	s.Codec = key
	c, err := s.Load()
	if err != nil || c.Accounts[0].RefreshToken != "r1" || c.ApiKeys[0].Key != "sk-1" {
		t.Fatalf("load plaintext: %v %+v", err, c)
	}
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	stored := rawRows(t, db)
	for _, sec := range secrets {
		if strings.Contains(stored, sec) {
			t.Fatalf("plaintext %s still stored:\n%s", sec, stored)
		}
	}
	if !strings.Contains(stored, "enc:v1:") || !strings.Contains(stored, `"a@x.io"`) {
		t.Fatalf("unexpected stored rows:\n%s", stored)
	}

	// Unchanged config: no rewrite despite the random nonce.
	before := stored
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	if rawRows(t, db) != before {
		t.Fatal("unchanged config was rewritten")
	}

	// Round trip with the same key in a fresh store.
	s2 := NewConfigStore(db)
	s2.Codec = key
	back, err := s2.Load()
	if err != nil || back.Accounts[0].AccessToken != "t1" || back.Password != "pw-12345678" {
		t.Fatalf("round trip: %v %+v", err, back)
	}

	// Wrong or missing key: refuse to load.
	wrong := NewConfigStore(db)
	wrong.Codec = storageCodec(t, 8)
	if _, err := wrong.Load(); !errors.Is(err, config.ErrEncryptionKeyWrong) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := plain.Load(); !errors.Is(err, config.ErrEncryptionKeyMissing) {
		t.Fatalf("missing key: %v", err)
	}

	// Export keeps ciphertext by default and writes plaintext with decrypt.
	config.SetDefaultSecretCodec(key)
	t.Cleanup(func() { config.SetDefaultSecretCodec(config.PlaintextSecretCodec()) })
	dir := t.TempDir()
	encOut, plainOut := filepath.Join(dir, "enc.json"), filepath.Join(dir, "plain.json")
	if _, err := ExportJSON(context.Background(), db, encOut, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportJSON(context.Background(), db, plainOut, true); err != nil {
		t.Fatal(err)
	}
	encData, _ := os.ReadFile(encOut)
	plainData, _ := os.ReadFile(plainOut)
	if strings.Contains(string(encData), `"r1"`) || !strings.Contains(string(encData), "enc:v1:") {
		t.Fatalf("default export leaked plaintext:\n%s", encData)
	}
	if !strings.Contains(string(plainData), `"r1"`) || strings.Contains(string(plainData), "enc:v1:") {
		t.Fatalf("--decrypt export not plaintext:\n%s", plainData)
	}
	if fi, _ := os.Stat(encOut); fi.Mode().Perm() != 0o600 {
		t.Fatalf("export mode %v", fi.Mode().Perm())
	}
	// The encrypted export loads with the key.
	got, err := config.ReadJSONFile(encOut)
	if err != nil || got.Accounts[0].RefreshToken != "r1" {
		t.Fatalf("read encrypted export: %v", err)
	}
}
