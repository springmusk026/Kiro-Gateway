package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"kiro-go/telemetry"
)

const requestSelect = `SELECT l.id, l.ts, l.method, l.path, l.endpoint, l.model, l.account_id, l.account_email,
	l.api_key_id, l.client_ip, l.user_agent, l.stream, l.protocol, l.status, l.http_status, l.error, l.error_type,
	l.input_tokens, l.output_tokens, l.credits, l.duration_ms, l.ttfb_ms, l.attempts, l.request_bytes, l.response_bytes`

func scanRequest(row pgx.Row, extra ...any) (*telemetry.RequestRecord, error) {
	var x telemetry.RequestRecord
	dst := []any{&x.ID, &x.Time, &x.Method, &x.Path, &x.Endpoint, &x.Model, &x.AccountID, &x.AccountEmail,
		&x.APIKeyID, &x.ClientIP, &x.UserAgent, &x.Stream, &x.Protocol, &x.Status, &x.HTTPStatus, &x.Error,
		&x.ErrorType, &x.InputTokens, &x.OutputTokens, &x.Credits, &x.DurationMs, &x.TTFBMs, &x.Attempts,
		&x.RequestBytes, &x.ResponseBytes}
	if err := row.Scan(append(dst, extra...)...); err != nil {
		return nil, err
	}
	return &x, nil
}

func (r *Recorder) QueryRequests(ctx context.Context, f telemetry.LogFilter) ([]telemetry.RequestRecord, error) {
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if !f.Since.IsZero() {
		add("l.ts >= $%d", f.Since)
	}
	if !f.Until.IsZero() {
		add("l.ts <= $%d", f.Until)
	}
	if f.Status != "" {
		add("l.status = $%d", f.Status)
	}
	if f.Endpoint != "" {
		add("l.endpoint = $%d", f.Endpoint)
	}
	if f.Model != "" {
		add("l.model = $%d", f.Model)
	}
	if f.AccountID != "" {
		add("l.account_id = $%d", f.AccountID)
	}
	if f.APIKeyID != "" {
		add("l.api_key_id = $%d", f.APIKeyID)
	}
	if f.ErrorType != "" {
		add("l.error_type = $%d", f.ErrorType)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(l.model ILIKE $%d OR l.error ILIKE $%d OR l.account_email ILIKE $%d OR l.path ILIKE $%d OR l.client_ip ILIKE $%d)", n, n, n, n, n))
	}
	if f.BeforeID != "" {
		// Keyset pagination on (ts, id).
		args = append(args, f.BeforeID)
		n := len(args)
		where = append(where, fmt.Sprintf("(l.ts, l.id) < (SELECT ts, id FROM kiro_request_logs WHERE id = $%d)", n))
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := requestSelect + ` FROM kiro_request_logs l`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY l.ts DESC, l.id DESC LIMIT %d", limit)
	rows, err := r.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.RequestRecord{}
	for rows.Next() {
		x, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

func (r *Recorder) GetRequest(ctx context.Context, id string) (*telemetry.RequestRecord, error) {
	var reqBody, respBody *string
	row := r.db.Pool.QueryRow(ctx, requestSelect+`, b.request_body, b.response_body
		FROM kiro_request_logs l LEFT JOIN kiro_request_bodies b ON b.id = l.id WHERE l.id = $1`, id)
	x, err := scanRequest(row, &reqBody, &respBody)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if reqBody != nil {
		x.RequestBody = *reqBody
	}
	if respBody != nil {
		x.ResponseBody = *respBody
	}
	return x, nil
}

func (r *Recorder) UsageSeries(ctx context.Context, since time.Time, bucket time.Duration) ([]telemetry.UsagePoint, error) {
	if bucket < time.Minute {
		bucket = time.Minute
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT to_timestamp(floor(extract(epoch FROM ts) / $2) * $2) AS b,
		count(*), count(*) FILTER (WHERE status <> 'success'),
		COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0), COALESCE(sum(credits), 0),
		COALESCE(avg(duration_ms) FILTER (WHERE status = 'success'), 0)
		FROM kiro_request_logs WHERE ts >= $1 GROUP BY b ORDER BY b`, since, bucket.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.UsagePoint{}
	for rows.Next() {
		var p telemetry.UsagePoint
		if err := rows.Scan(&p.Time, &p.Requests, &p.Errors, &p.InputTokens, &p.OutputTokens, &p.Credits, &p.AvgLatencyMs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

var usageDimensions = map[string]string{
	"model":    "model",
	"account":  "CASE WHEN account_email <> '' THEN account_email ELSE account_id END",
	"apiKey":   "api_key_id",
	"endpoint": "endpoint",
	"client":   "user_agent",
}

func (r *Recorder) UsageBy(ctx context.Context, dimension string, since time.Time, limit int) ([]telemetry.UsageGroup, error) {
	col, ok := usageDimensions[dimension]
	if !ok {
		return nil, fmt.Errorf("unknown dimension %q", dimension)
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := r.db.Pool.Query(ctx, fmt.Sprintf(`SELECT %s AS k, count(*), count(*) FILTER (WHERE status <> 'success'),
		COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0), COALESCE(sum(credits), 0)
		FROM kiro_request_logs WHERE ts >= $1 GROUP BY k ORDER BY count(*) DESC LIMIT %d`, col, limit), since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.UsageGroup{}
	for rows.Next() {
		var g telemetry.UsageGroup
		if err := rows.Scan(&g.Key, &g.Requests, &g.Errors, &g.InputTokens, &g.OutputTokens, &g.Credits); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Recorder) QueryAudit(ctx context.Context, since time.Time, limit int) ([]telemetry.AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT ts, actor, ip, action, target, detail FROM kiro_audit_events
		WHERE ts >= $1 ORDER BY ts DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.AuditEvent{}
	for rows.Next() {
		var e telemetry.AuditEvent
		var detail []byte
		if err := rows.Scan(&e.Time, &e.Actor, &e.IP, &e.Action, &e.Target, &detail); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &e.Detail)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
