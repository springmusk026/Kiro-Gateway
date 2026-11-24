package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
)

// setupMigration writes config.json + two stored responses into a temp data
// dir and boots the config package on the database the way main does.
func setupMigration(t *testing.T, db *DB, src *config.Config) *JSONMigrator {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(src)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "responses"), 0o755)
	for _, id := range []string{"resp_1", "resp_2"} {
		os.WriteFile(filepath.Join(dir, "responses", id+".json"), []byte(`{"id":"`+id+`","stored_at":1700000000}`), 0o600)
	}
	os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o600)

	cs := NewConfigStore(db)
	config.UseStore(cs)
	t.Cleanup(func() { config.UseStore(nil) })
	if err := config.Init(path); err != nil {
		t.Fatal(err)
	}
	return &JSONMigrator{DB: db, ConfigPath: path, Reload: config.Load}
}

func TestJSONMigrator_PendingImportAndDelete(t *testing.T) {
	db := testDB(t)
	m := setupMigration(t, db, sampleConfig())
	ctx := context.Background()

	st, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "pending" || st.JSONAccounts != 2 || st.JSONKeys != 1 || st.JSONResponses != 2 || st.MissingAccounts != 2 {
		t.Fatalf("status = %+v", st)
	}

	res, err := m.Run(ctx, "import", true)
	if err != nil {
		t.Fatalf("run: %v (%+v)", err, res)
	}
	if !res.Verified || len(res.Deleted) != 3 || res.Status.State != "none" {
		t.Fatalf("result = %+v status=%+v", res, res.Status)
	}
	if _, err := os.Stat(m.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("config.json should be gone")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(m.ConfigPath), "keep.txt")); err != nil {
		t.Fatal("unrelated files must survive")
	}
	if got := config.GetPassword(); got != "pw-12345678" {
		t.Fatalf("settings not imported, password=%q", got)
	}
	snap, _ := config.Snapshot()
	if len(snap.Accounts) != 2 || len(snap.ApiKeys) != 1 {
		t.Fatalf("in-memory config not reloaded: %d accounts %d keys", len(snap.Accounts), len(snap.ApiKeys))
	}
	// The reloaded store must keep persisting (stale hash cache would skip rows).
	reloaded, _ := NewConfigStore(db).Load()
	if len(reloaded.Accounts) != 2 {
		t.Fatalf("db accounts = %d", len(reloaded.Accounts))
	}
}

func TestJSONMigrator_DivergedRefusesDeleteThenMerges(t *testing.T) {
	db := testDB(t)
	src := sampleConfig()
	// The database already has a1 (and a newer account a9) but not a2 / responses.
	pre := sampleConfig()
	pre.Accounts = []config.Account{src.Accounts[0], {ID: "a9", Email: "new@x.io"}}
	pre.Password = "db-password-1"
	if err := NewConfigStore(db).Save(pre); err != nil {
		t.Fatal(err)
	}
	m := setupMigration(t, db, src)
	ctx := context.Background()

	st, _ := m.Status(ctx)
	if st.State != "diverged" || st.MissingAccounts != 1 || st.MissingKeys != 0 || st.MissingResponses != 2 {
		t.Fatalf("status = %+v", st)
	}
	if _, err := m.Run(ctx, "delete", false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("delete must refuse unverified data, got %v", err)
	}
	if _, err := os.Stat(m.ConfigPath); err != nil {
		t.Fatal("config.json must still exist after a refused delete")
	}

	res, err := m.Run(ctx, "import", false)
	if err != nil || res.ImportedAccounts != 1 || res.ImportedResponses != 2 || res.Status.State != "migrated" {
		t.Fatalf("merge: %v %+v %+v", err, res, res.Status)
	}
	if config.GetPassword() != "db-password-1" {
		t.Fatal("merge must not overwrite database settings")
	}
	snap, _ := config.Snapshot()
	if len(snap.Accounts) != 3 {
		t.Fatalf("accounts after merge = %d", len(snap.Accounts))
	}
	if _, err := m.Run(ctx, "delete", false); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(ctx); st.State != "none" {
		t.Fatalf("after delete: %+v", st)
	}
}

func TestJSONMigrator_ExpiredResponsesAreNotMissing(t *testing.T) {
	db := testDB(t)
	m := setupMigration(t, db, sampleConfig())
	m.ResponseTTL = 24 * time.Hour // the fixture files are from 2023
	ctx := context.Background()
	if _, err := m.Run(ctx, "import", false); err != nil {
		t.Fatal(err)
	}
	st, _ := m.Status(ctx)
	if st.State != "migrated" || st.MissingResponses != 0 || st.JSONResponses != 2 {
		t.Fatalf("expired response files must not block the migration: %+v", st)
	}
	res, err := m.Run(ctx, "delete", false)
	if err != nil || len(res.Deleted) != 3 {
		t.Fatalf("delete: %v %+v", err, res)
	}
}
