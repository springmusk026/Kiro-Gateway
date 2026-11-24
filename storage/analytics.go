package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// Analytics aggregates the request history for one window: a summary (and the
// same summary for the preceding window, for deltas), a time series,
// breakdowns by every recorded dimension, error classes, a weekday × hour
// heatmap and the slowest requests. Everything is computed in PostgreSQL; the
// queries run concurrently on the pool.

// clientFamilyExpr turns a user agent into a readable client name.
const clientFamilyExpr = `CASE
	WHEN user_agent = '' THEN 'Unknown'
	WHEN user_agent ILIKE 'claude-cli%' OR user_agent ILIKE '%claude-code%' THEN 'Claude Code'
	WHEN user_agent ILIKE '%codex%' THEN 'Codex'
	WHEN user_agent ILIKE '%cline%' THEN 'Cline'
	WHEN user_agent ILIKE '%roo%code%' OR user_agent ILIKE 'roo-%' THEN 'Roo Code'
	WHEN user_agent ILIKE '%continue%' THEN 'Continue'
	WHEN user_agent ILIKE '%cursor%' THEN 'Cursor'
	WHEN user_agent ILIKE '%litellm%' THEN 'LiteLLM'
	WHEN user_agent ILIKE 'Anthropic/%' OR user_agent ILIKE '%anthropic-sdk%' THEN 'Anthropic SDK'
	WHEN user_agent ILIKE 'OpenAI/%' OR user_agent ILIKE '%openai-%' THEN 'OpenAI SDK'
	WHEN user_agent ILIKE 'curl/%' THEN 'curl'
	WHEN user_agent ILIKE 'python-requests%' OR user_agent ILIKE 'python-httpx%' OR user_agent ILIKE 'aiohttp%' THEN 'Python HTTP'
	WHEN user_agent ILIKE 'Mozilla/%' THEN 'Browser'
	ELSE split_part(split_part(user_agent, ' ', 1), '/', 1)
END`

// analyticsDimensions maps breakdown names to SQL expressions.
var analyticsDimensions = map[string]string{
	"model":    "model",
	"account":  "account_id",
	"apiKey":   "api_key_id",
	"client":   clientFamilyExpr,
	"endpoint": "endpoint",
	"protocol": "protocol",
	"status":   "http_status::text",
}

const (
	throttledExpr = `http_status IN (429, 503, 529)`
	latencyAggs   = `COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY ttfb_ms) FILTER (WHERE status = 'success' AND ttfb_ms > 0), 0)`
	tokenAggs = `COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0),
		COALESCE(sum(cache_read_tokens), 0), COALESCE(sum(cache_write_tokens), 0), COALESCE(sum(credits), 0)`
)

type analyticsWhere struct {
	sql  string
	args []any
}

// where builds the shared filter; $1/$2 are always the window bounds.
func buildAnalyticsWhere(q telemetry.AnalyticsQuery, from, to time.Time) analyticsWhere {
	w := analyticsWhere{sql: "ts >= $1 AND ts < $2", args: []any{from, to}}
	add := func(col, v string) {
		if v == "" {
			return
		}
		w.args = append(w.args, v)
		w.sql += fmt.Sprintf(" AND %s = $%d", col, len(w.args))
	}
	add("model", q.Model)
	add("account_id", q.Account)
	add("api_key_id", q.APIKey)
	add("endpoint", q.Endpoint)
	return w
}

func (r *Recorder) Analytics(ctx context.Context, q telemetry.AnalyticsQuery) (*telemetry.AnalyticsReport, error) {
	if !q.To.After(q.From) {
		return nil, fmt.Errorf("invalid window")
	}
	if q.Bucket < time.Minute {
		q.Bucket = time.Minute
	}
	if q.Location == nil {
		q.Location = time.UTC
	}
	span := q.To.Sub(q.From)
	cur := buildAnalyticsWhere(q, q.From, q.To)
	prev := buildAnalyticsWhere(q, q.From.Add(-span), q.From)

	rep := &telemetry.AnalyticsReport{
		From: q.From, To: q.To, BucketSec: int64(q.Bucket.Seconds()),
		Breakdowns: map[string][]telemetry.AnalyticsGroup{},
	}
	g, gctx := errgroup.WithContext(ctx)
	// Leave pool connections for the request recorder.
	g.SetLimit(4)
	g.Go(func() error { return r.analyticsSummary(gctx, cur, &rep.Summary) })
	g.Go(func() error { return r.analyticsSummary(gctx, prev, &rep.Previous) })
	g.Go(func() error {
		s, err := r.analyticsSeries(gctx, cur, q.Bucket)
		rep.Series = s
		return err
	})
	g.Go(func() error {
		e, err := r.analyticsErrors(gctx, cur)
		rep.Errors = e
		return err
	})
	g.Go(func() error {
		h, err := r.analyticsHeatmap(gctx, cur, q.Location)
		rep.Heatmap = h
		return err
	})
	g.Go(func() error {
		s, err := r.analyticsSlowest(gctx, cur)
		rep.Slowest = s
		return err
	})
	groups := make(map[string]*[]telemetry.AnalyticsGroup, len(analyticsDimensions))
	for name := range analyticsDimensions {
		out := &[]telemetry.AnalyticsGroup{}
		groups[name] = out
		name := name
		g.Go(func() error {
			res, err := r.analyticsGroup(gctx, cur, name, 25)
			*out = res
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	for name, out := range groups {
		rep.Breakdowns[name] = *out
	}
	return rep, nil
}

func (r *Recorder) analyticsSummary(ctx context.Context, w analyticsWhere, s *telemetry.AnalyticsSummary) error {
	return r.db.Pool.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE status = 'error'),
		count(*) FILTER (WHERE status = 'cancelled'),
		count(*) FILTER (WHERE status = 'error' AND http_status BETWEEN 400 AND 498 AND http_status <> 429),
		count(*) FILTER (WHERE `+throttledExpr+`),
		`+tokenAggs+`,
		COALESCE(avg(duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status = 'success'), 0),
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY ttfb_ms) FILTER (WHERE status = 'success' AND ttfb_ms > 0), 0),
		COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY ttfb_ms) FILTER (WHERE status = 'success' AND ttfb_ms > 0), 0),
		COALESCE(avg(attempts) FILTER (WHERE attempts > 0), 0),
		count(*) FILTER (WHERE attempts > 1),
		count(*) FILTER (WHERE stream),
		COALESCE(sum(request_bytes), 0), COALESCE(sum(response_bytes), 0),
		count(DISTINCT account_id) FILTER (WHERE account_id <> ''),
		count(DISTINCT api_key_id) FILTER (WHERE api_key_id <> ''),
		count(DISTINCT model) FILTER (WHERE model <> ''),
		COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY output_tokens * 1000.0 / GREATEST(duration_ms - ttfb_ms, 1))
			FILTER (WHERE status = 'success' AND output_tokens > 0 AND duration_ms > 0), 0)
		FROM kiro_request_logs WHERE `+w.sql, w.args...).Scan(
		&s.Requests, &s.Errors, &s.Cancelled, &s.ClientErrors, &s.Throttled,
		&s.InputTokens, &s.OutputTokens, &s.CacheRead, &s.CacheWrite, &s.Credits,
		&s.AvgLatencyMs, &s.P50LatencyMs, &s.P95LatencyMs, &s.P99LatencyMs, &s.P50TTFBMs, &s.P95TTFBMs,
		&s.AvgAttempts, &s.Retried, &s.Streamed, &s.RequestBytes, &s.ResponseBytes,
		&s.Accounts, &s.APIKeys, &s.Models, &s.OutputTokensPS)
}

func (r *Recorder) analyticsSeries(ctx context.Context, w analyticsWhere, bucket time.Duration) ([]telemetry.AnalyticsPoint, error) {
	args := append(append([]any{}, w.args...), bucket.Seconds())
	n := len(args)
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf(`SELECT to_timestamp(floor(extract(epoch FROM ts) / $%d) * $%d) AS b,
		count(*), count(*) FILTER (WHERE status = 'error'), count(*) FILTER (WHERE status = 'cancelled'), count(*) FILTER (WHERE %s),
		%s, %s
		FROM kiro_request_logs WHERE %s GROUP BY b ORDER BY b`, n, n, throttledExpr, tokenAggs, latencyAggs, w.sql), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AnalyticsPoint{}
	for rows.Next() {
		var p telemetry.AnalyticsPoint
		if err := rows.Scan(&p.Time, &p.Requests, &p.Errors, &p.Cancelled, &p.Throttled,
			&p.InputTokens, &p.OutputTokens, &p.CacheRead, &p.CacheWrite, &p.Credits,
			&p.P50LatencyMs, &p.P95LatencyMs, &p.P50TTFBMs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Recorder) analyticsGroup(ctx context.Context, w analyticsWhere, dimension string, limit int) ([]telemetry.AnalyticsGroup, error) {
	col := analyticsDimensions[dimension]
	label := "''"
	if dimension == "account" {
		label = "max(account_email)"
	}
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf(`SELECT %s AS k, %s,
		count(*), count(*) FILTER (WHERE status = 'error'), count(*) FILTER (WHERE %s),
		%s, %s,
		COALESCE(avg(attempts) FILTER (WHERE attempts > 0), 0), max(ts)
		FROM kiro_request_logs WHERE %s GROUP BY k ORDER BY count(*) DESC LIMIT %d`,
		col, label, throttledExpr, tokenAggs, latencyAggs, w.sql, limit), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AnalyticsGroup{}
	for rows.Next() {
		var g telemetry.AnalyticsGroup
		if err := rows.Scan(&g.Key, &g.Label, &g.Requests, &g.Errors, &g.Throttled,
			&g.InputTokens, &g.OutputTokens, &g.CacheRead, &g.CacheWrite, &g.Credits,
			&g.P50LatencyMs, &g.P95LatencyMs, &g.P50TTFBMs, &g.AvgAttempts, &g.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Recorder) analyticsErrors(ctx context.Context, w analyticsWhere) ([]telemetry.AnalyticsError, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT COALESCE(NULLIF(error_type, ''), 'unknown'), http_status, count(*),
		(array_agg(left(error, 300) ORDER BY ts DESC))[1], max(ts)
		FROM kiro_request_logs WHERE status = 'error' AND `+w.sql+`
		GROUP BY 1, 2 ORDER BY count(*) DESC LIMIT 30`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AnalyticsError{}
	for rows.Next() {
		var e telemetry.AnalyticsError
		if err := rows.Scan(&e.Type, &e.Status, &e.Count, &e.Sample, &e.LastSeen); err != nil {
			return nil, err
		}
		e.Sample = strings.TrimSpace(e.Sample)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Recorder) analyticsHeatmap(ctx context.Context, w analyticsWhere, loc *time.Location) ([]telemetry.AnalyticsHeatCell, error) {
	args := append(append([]any{}, w.args...), loc.String())
	n := len(args)
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf(`SELECT extract(dow FROM ts AT TIME ZONE $%d)::int AS d,
		extract(hour FROM ts AT TIME ZONE $%d)::int AS h, count(*), count(*) FILTER (WHERE status = 'error')
		FROM kiro_request_logs WHERE %s GROUP BY d, h`, n, n, w.sql), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AnalyticsHeatCell{}
	for rows.Next() {
		var c telemetry.AnalyticsHeatCell
		if err := rows.Scan(&c.Weekday, &c.Hour, &c.Requests, &c.Errors); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Recorder) analyticsSlowest(ctx context.Context, w analyticsWhere) ([]telemetry.AnalyticsSlow, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT id, ts, model, endpoint, status, duration_ms, ttfb_ms, output_tokens, attempts
		FROM kiro_request_logs WHERE `+w.sql+` ORDER BY duration_ms DESC LIMIT 10`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AnalyticsSlow{}
	for rows.Next() {
		var s telemetry.AnalyticsSlow
		if err := rows.Scan(&s.ID, &s.Time, &s.Model, &s.Endpoint, &s.Status, &s.DurationMs, &s.TTFBMs, &s.Output, &s.Attempts); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
