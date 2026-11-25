package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/config"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// Tests run against a real PostgreSQL when KIRO_TEST_DATABASE_URL is set,
// e.g. postgres://kiro@localhost:55432/kirogo_test?sslmode=disable
func testDB(t *testing.T) *DB {
	t.Helper()
	url := os.Getenv("KIRO_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("KIRO_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `TRUNCATE kiro_settings, kiro_accounts, kiro_api_keys, kiro_responses, kiro_request_logs, kiro_request_bodies, kiro_audit_events`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func sampleConfig() *config.Config {
	return &config.Config{
		Password: "pw-12345678",
		Port:     8080,
		Host:     "0.0.0.0",
		Accounts: []config.Account{
			{ID: "a1", Email: "a@x.io", Enabled: true, AccessToken: "t1", RefreshToken: "r1", RequestCount: 1},
			{ID: "a2", Email: "b@x.io", Enabled: false, AccessToken: "t2"},
		},
		ApiKeys:       []config.ApiKeyEntry{{ID: "k1", Name: "laptop", Key: "sk-1", Enabled: true}},
		ModelMappings: []config.ModelMapping{{Source: "fast", Target: "claude-haiku-4.5"}},
	}
}

func TestConfigStoreRoundTripAndDiff(t *testing.T) {
	db := testDB(t)
	s := NewConfigStore(db)
	if c, err := s.Load(); err != nil || c != nil {
		t.Fatalf("empty db should load nil, got %v %v", c, err)
	}
	c := sampleConfig()
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := NewConfigStore(db).Load()
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 2 || got.Accounts[0].ID != "a1" || got.Accounts[1].ID != "a2" || got.ApiKeys[0].Key != "" || got.ApiKeys[0].KeyHash != config.HashApiKey("sk-1") ||
		got.Password != "pw-12345678" || got.ModelMappings[0].Target != "claude-haiku-4.5" || got.Accounts[0].RefreshToken != "r1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Change one account: only that row's updated_at moves.
	var before time.Time
	db.Pool.QueryRow(context.Background(), `SELECT updated_at FROM kiro_accounts WHERE id='a2'`).Scan(&before)
	time.Sleep(20 * time.Millisecond)
	c.Accounts[0].RequestCount = 2
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	db.Pool.QueryRow(context.Background(), `SELECT updated_at FROM kiro_accounts WHERE id='a2'`).Scan(&after)
	if !after.Equal(before) {
		t.Fatal("unchanged account row was rewritten")
	}

	// Delete an account and a key.
	c.Accounts = c.Accounts[:1]
	c.ApiKeys = nil
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	got, _ = NewConfigStore(db).Load()
	if len(got.Accounts) != 1 || len(got.ApiKeys) != 0 || got.Accounts[0].RequestCount != 2 {
		t.Fatalf("after delete: %+v", got)
	}
}

func TestImportAndExportJSON(t *testing.T) {
	db := testDB(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(sampleConfig())
	os.WriteFile(cfgPath, data, 0o600)
	os.MkdirAll(filepath.Join(dir, "responses"), 0o755)
	os.WriteFile(filepath.Join(dir, "responses", "resp_abc.json"), []byte(`{"id":"resp_abc","stored_at":1700000000,"output":[]}`), 0o600)

	res, err := ImportJSON(context.Background(), db, cfgPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accounts != 2 || res.APIKeys != 1 || res.Responses != 1 {
		t.Fatalf("import result %+v", res)
	}
	if _, err := ImportJSON(context.Background(), db, cfgPath, false); err != ErrDatabaseNotEmpty {
		t.Fatalf("second import must refuse, got %v", err)
	}
	if _, err := ImportJSON(context.Background(), db, cfgPath, true); err != nil {
		t.Fatalf("forced import: %v", err)
	}
	doc, _, err := NewResponseStore(db).LoadResponse(context.Background(), "resp_abc")
	if err != nil || len(doc) == 0 {
		t.Fatalf("response not imported: %v", err)
	}
	out := filepath.Join(dir, "export.json")
	if _, err := ExportJSON(context.Background(), db, out, false); err != nil {
		t.Fatal(err)
	}
	back, err := config.ReadJSONFile(out)
	if err != nil || len(back.Accounts) != 2 || back.Accounts[0].RefreshToken != "r1" {
		t.Fatalf("export mismatch: %+v %v", back, err)
	}
}

func TestRecorderWritesAsyncAndQueries(t *testing.T) {
	db := testDB(t)
	r := NewRecorder(db, nil)
	now := time.Now()
	for i := 0; i < 1200; i++ {
		status := "success"
		if i%10 == 0 {
			status = "error"
		}
		r.RecordRequest(&telemetry.RequestRecord{
			ID: fmt.Sprintf("req-%04d", i), Time: now.Add(time.Duration(i) * time.Millisecond), Endpoint: "claude",
			Model: []string{"claude-sonnet-4.5", "claude-opus-4.7"}[i%2], Status: status, InputTokens: 10, OutputTokens: 5,
			Error: "bad \xff utf8 \x00 nul", RequestBody: map[bool]string{true: "{\"x\":1}"}[i == 7],
			FallbackModel: map[bool]string{true: "backup-model"}[i == 7],
		})
	}
	r.RecordAudit(&telemetry.AuditEvent{Time: now, Actor: "admin", Action: "settings.update"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.Close(ctx)
	st := r.SinkStats()
	if st.Written != 1201 || st.Failed != 0 {
		t.Fatalf("stats %+v", st)
	}
	page, err := r.QueryRequests(ctx, telemetry.LogFilter{Limit: 100})
	if err != nil || len(page) != 100 || page[0].ID != "req-1199" {
		t.Fatalf("page1: %d %v first=%v", len(page), err, page)
	}
	page2, _ := r.QueryRequests(ctx, telemetry.LogFilter{Limit: 100, BeforeID: page[99].ID})
	if len(page2) != 100 || page2[0].ID != "req-1099" {
		t.Fatalf("keyset pagination broken: %v", page2[0].ID)
	}
	errs, _ := r.QueryRequests(ctx, telemetry.LogFilter{Status: "error", Limit: 1000})
	if len(errs) != 120 {
		t.Fatalf("error filter: %d", len(errs))
	}
	one, err := r.GetRequest(ctx, "req-0007")
	if err != nil || one.RequestBody != `{"x":1}` || one.FallbackModel != "backup-model" {
		t.Fatalf("get with body: %+v %v", one, err)
	}
	by, _ := r.UsageBy(ctx, "model", now.Add(-time.Hour), 10)
	if len(by) != 2 || by[0].Requests != 600 {
		t.Fatalf("usage by model: %+v", by)
	}
	series, _ := r.UsageSeries(ctx, now.Add(-time.Hour), time.Minute)
	var total int64
	for _, p := range series {
		total += p.Requests
	}
	if total != 1200 {
		t.Fatalf("series total %d", total)
	}
	audit, _ := r.QueryAudit(ctx, now.Add(-time.Hour), 10)
	if len(audit) != 1 || audit[0].Action != "settings.update" {
		t.Fatalf("audit %+v", audit)
	}
}

func TestRecorderNeverBlocksWhenDatabaseIsGone(t *testing.T) {
	db := testDB(t)
	r := NewRecorder(db, nil)
	db.Pool.Close() // simulate an unavailable database
	start := time.Now()
	for i := 0; i < recorderQueue+5000; i++ {
		r.RecordRequest(&telemetry.RequestRecord{ID: fmt.Sprint(i), Time: time.Now()})
	}
	if el := time.Since(start); el > 500*time.Millisecond {
		t.Fatalf("recording blocked for %s", el)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Close(ctx)
	if st := r.SinkStats(); st.Dropped+st.Failed == 0 || st.Written != 0 {
		t.Fatalf("records must be dropped or counted as failed, never block: %+v", st)
	}
}
