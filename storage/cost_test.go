package storage

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/springmusk026/Kiro-Gateway/pricing"
	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

func TestRequestCostRecordedSummedAndBackfilled(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	pricing.Set(&pricing.Catalog{Models: map[string]pricing.Model{
		"claude-sonnet-5-5": {ID: "claude-sonnet-5-5", Provider: "anthropic", Price: pricing.Price{Input: 2, Output: 10, CacheRead: 0.1}},
		"claude-haiku-5-5": {ID: "claude-haiku-5-5", Provider: "anthropic", Price: pricing.Price{Input: 0.1, Output: 0.5,
			Tiers: []pricing.Tier{{Above: 100_000, Input: 0.5, Output: 2.5}}}},
	}})
	t.Cleanup(func() { pricing.Set(nil) })

	r := &Recorder{db: db}
	now := time.Now()
	// Recorded with a cost (as the gateway does).
	r.writeRequests([]*telemetry.RequestRecord{{ID: "p1", Time: now, Model: "claude-sonnet-5.5", Status: "success",
		InputTokens: 1_000_000, OutputTokens: 100_000, CostUSD: 3}})
	// Older rows without one: a priced model (both tiers), an unpriced model, an error.
	for _, row := range []struct {
		id, model, status string
		in, out, cr       int
	}{
		{"o1", "claude-sonnet-5.5", "success", 1_000_000, 0, 500_000},
		{"o2", "claude-haiku-5.5", "success", 200_000, 1_000_000, 0},
		{"o3", "claude-haiku-5.5", "success", 50_000, 0, 0},
		{"o4", "auto", "success", 10, 10, 0},
		{"o5", "claude-sonnet-5.5", "error", 1_000, 0, 0},
	} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO kiro_request_logs (id, ts, model, status, input_tokens, output_tokens, cache_read_tokens)
			VALUES ($1, now(), $2, $3, $4, $5, $6)`, row.id, row.model, row.status, row.in, row.out, row.cr); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.BackfillCosts(ctx)
	if err != nil || n != 3 {
		t.Fatalf("backfill: n=%d err=%v", n, err)
	}
	want := map[string]float64{
		"p1": 3,
		"o1": 0.5*2 + 0.5*0.1, // 500k uncached + 500k cache reads
		"o2": 0.2*0.5 + 1*2.5, // above the 100k tier
		"o3": 0.05 * 0.1,
		"o4": 0, "o5": 0,
	}
	for id, w := range want {
		var got float64
		if err := db.Pool.QueryRow(ctx, `SELECT cost_usd FROM kiro_request_logs WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if math.Abs(got-w) > 1e-9 {
			t.Errorf("%s cost = %v, want %v", id, got, w)
		}
		if id == "o2" { // must agree with the Go formula
			gc, _ := pricing.Cost("claude-haiku-5.5", pricing.Usage{Input: 200_000, Output: 1_000_000})
			if math.Abs(got-gc) > 1e-9 {
				t.Errorf("SQL %v != Go %v", got, gc)
			}
		}
	}
	if n, _ := r.BackfillCosts(ctx); n != 0 {
		t.Fatalf("second backfill updated %d rows", n)
	}

	rep, err := r.Analytics(ctx, telemetry.AnalyticsQuery{From: now.Add(-time.Hour), To: now.Add(time.Minute), Bucket: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	total := 3 + want["o1"] + want["o2"] + want["o3"]
	if math.Abs(rep.Summary.CostUSD-total) > 1e-9 {
		t.Fatalf("summary cost = %v, want %v", rep.Summary.CostUSD, total)
	}
	var series float64
	for _, p := range rep.Series {
		series += p.CostUSD
	}
	if math.Abs(series-total) > 1e-9 || len(rep.Breakdowns["model"]) == 0 || rep.Breakdowns["model"][0].CostUSD == 0 {
		t.Fatalf("series=%v breakdown=%+v", series, rep.Breakdowns["model"])
	}
	got, err := r.QueryRequests(ctx, telemetry.LogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range got {
		if x.ID == "p1" && x.CostUSD != 3 {
			t.Fatalf("request log cost = %v", x.CostUSD)
		}
	}
}
