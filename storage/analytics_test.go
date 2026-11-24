package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"kiro-go/telemetry"
)

func TestAnalyticsReport(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	ins := func(id string, ago time.Duration, model, acc, ua, status string, http, in, out, cr, cw int, credits float64, dur, ttfb int64, attempts int) {
		_, err := db.Pool.Exec(ctx, `INSERT INTO kiro_request_logs (id, ts, model, account_id, account_email, api_key_id, user_agent, endpoint,
			protocol, status, http_status, error_type, error, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, credits,
			duration_ms, ttfb_ms, attempts, stream)
			VALUES ($1, $2, $3, $4, $4 || '@x.io', 'k1', $5, 'claude', 'responses', $6, $7, CASE WHEN $6 = 'success' THEN '' ELSE 'quota' END,
			CASE WHEN $6 = 'success' THEN '' ELSE 'limit reached' END, $8, $9, $10, $11, $12, $13, $14, $15, true)`,
			id, now.Add(-ago), model, acc, ua, status, http, in, out, cr, cw, credits, dur, ttfb, attempts)
		if err != nil {
			t.Fatal(err)
		}
	}
	ins("r1", 10*time.Minute, "claude-sonnet-4.5", "a1", "claude-cli/1.0.80 (external, cli)", "success", 200, 1000, 100, 800, 0, 0.5, 2000, 400, 1)
	ins("r2", 20*time.Minute, "claude-sonnet-4.5", "a1", "claude-cli/1.0.80 (external, cli)", "success", 200, 2000, 200, 0, 1500, 1.0, 4000, 600, 2)
	ins("r3", 30*time.Minute, "claude-opus-4.5", "a2", "codex_cli_rs/0.50.0", "error", 429, 0, 0, 0, 0, 0, 100, 0, 3)
	ins("old", 30*time.Hour, "claude-opus-4.5", "a2", "curl/8.0", "success", 200, 10, 10, 0, 0, 0.1, 1000, 100, 1) // previous window

	rep, err := (&Recorder{db: db}).Analytics(ctx, telemetry.AnalyticsQuery{
		From: now.Add(-24 * time.Hour), To: now.Add(time.Minute), Bucket: time.Hour, Location: time.UTC,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Summary
	if s.Requests != 3 || s.Errors != 1 || s.Throttled != 1 || s.InputTokens != 3000 || s.CacheRead != 800 || s.CacheWrite != 1500 {
		t.Fatalf("summary = %+v", s)
	}
	if s.Retried != 2 || s.Accounts != 2 || s.Models != 2 || s.P50LatencyMs != 3000 {
		t.Fatalf("summary details = %+v", s)
	}
	if rep.Previous.Requests != 1 {
		t.Fatalf("previous window = %+v", rep.Previous)
	}
	clients := map[string]int64{}
	for _, g := range rep.Breakdowns["client"] {
		clients[g.Key] = g.Requests
	}
	if clients["Claude Code"] != 2 || clients["Codex"] != 1 {
		t.Fatalf("client families = %v", clients)
	}
	if acc := rep.Breakdowns["account"]; len(acc) == 0 || acc[0].Label != "a1@x.io" {
		t.Fatalf("account labels = %+v", acc)
	}
	if len(rep.Errors) != 1 || rep.Errors[0].Status != 429 || rep.Errors[0].Type != "quota" {
		t.Fatalf("errors = %+v", rep.Errors)
	}
	if len(rep.Slowest) != 3 || rep.Slowest[0].ID != "r2" {
		t.Fatalf("slowest = %+v", rep.Slowest)
	}
	var heat int64
	for _, c := range rep.Heatmap {
		heat += c.Requests
	}
	if heat != 3 || len(rep.Series) == 0 {
		t.Fatalf("heatmap total %d, series %d", heat, len(rep.Series))
	}

	// Filters apply to every part.
	f, err := (&Recorder{db: db}).Analytics(ctx, telemetry.AnalyticsQuery{
		From: now.Add(-24 * time.Hour), To: now.Add(time.Minute), Bucket: time.Hour, Model: "claude-opus-4.5",
	})
	if err != nil || f.Summary.Requests != 1 || len(f.Breakdowns["model"]) != 1 {
		t.Fatalf("filtered: %v %+v", err, f.Summary)
	}
}

func TestCountRequests(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	for i := 0; i < 7; i++ {
		status := "success"
		if i%3 == 0 {
			status = "error"
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO kiro_request_logs (id, ts, model, status) VALUES ($1, now() - make_interval(mins => $2), 'm', $3)`,
			fmt.Sprintf("c%d", i), i, status); err != nil {
			t.Fatal(err)
		}
	}
	r := &Recorder{db: db}
	n, capped, err := r.CountRequests(ctx, telemetry.LogFilter{})
	if err != nil || n != 7 || capped {
		t.Fatalf("all: n=%d capped=%v err=%v", n, capped, err)
	}
	n, _, _ = r.CountRequests(ctx, telemetry.LogFilter{Status: "error", BeforeID: "c1"})
	if n != 3 {
		t.Fatalf("errors (cursor ignored) = %d, want 3", n)
	}
	page, err := r.QueryRequests(ctx, telemetry.LogFilter{Limit: 3, BeforeID: "c2"})
	if err != nil || len(page) != 3 || page[0].ID != "c3" {
		t.Fatalf("keyset page = %v %v", page, err)
	}
}
