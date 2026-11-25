package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/springmusk026/Kiro-Gateway/telemetry"
)

// requestConversationSchema adds the upstream conversation id to request
// logs. It runs at every start (idempotentSchemas in postgres.go); the
// column check avoids taking a table lock once the column exists. Both
// indexes are partial or narrow so request inserts stay cheap.
const requestConversationSchema = `
DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'kiro_request_logs' AND column_name = 'conversation_id') THEN
		ALTER TABLE kiro_request_logs ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';
	END IF;
END $$;
CREATE INDEX IF NOT EXISTS kiro_request_logs_conversation_ts ON kiro_request_logs (conversation_id, ts) WHERE conversation_id <> '';
CREATE INDEX IF NOT EXISTS kiro_request_logs_api_key_ts ON kiro_request_logs (api_key_id, ts DESC) WHERE api_key_id <> '';
`

// conversationRequestsCap bounds the timeline of one conversation.
const conversationRequestsCap = 1000

// QueryConversations lists conversations, most recently active first.
func (r *Recorder) QueryConversations(ctx context.Context, f telemetry.ConversationFilter) ([]telemetry.ConversationSummary, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	args := []any{}
	where := []string{"l.conversation_id <> ''"}
	if !f.Since.IsZero() {
		args = append(args, f.Since)
		where = append(where, fmt.Sprintf("l.ts >= $%d", len(args)))
	}
	var having []string
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		n := len(args)
		having = append(having, fmt.Sprintf("(l.conversation_id ILIKE $%d OR bool_or(l.model ILIKE $%d) OR bool_or(l.account_email ILIKE $%d))", n, n, n))
	}
	if f.BeforeID != "" {
		// Keyset pagination on (last seen, id). A conversation's last request
		// does not depend on the window's lower bound.
		args = append(args, f.BeforeID)
		n := len(args)
		having = append(having, fmt.Sprintf("(max(l.ts), l.conversation_id) < (SELECT max(ts), $%d::text FROM kiro_request_logs WHERE conversation_id = $%d)", n, n))
	}
	q := `SELECT l.conversation_id, min(l.ts), max(l.ts), count(*),
		COALESCE(array_agg(DISTINCT l.model) FILTER (WHERE l.model <> ''), '{}'),
		COALESCE(array_agg(DISTINCT l.account_id) FILTER (WHERE l.account_id <> ''), '{}'),
		COALESCE(array_agg(DISTINCT l.api_key_id) FILTER (WHERE l.api_key_id <> ''), '{}'),
		COALESCE((array_agg(l.user_agent ORDER BY l.ts))[1], ''),
		COALESCE(sum(l.input_tokens), 0), COALESCE(sum(l.output_tokens), 0), COALESCE(sum(l.cache_read_tokens), 0),
		COALESCE(sum(l.credits), 0),
		count(*) FILTER (WHERE l.status = 'error'), count(*) FILTER (WHERE l.status = 'cancelled')
		FROM kiro_request_logs l WHERE ` + strings.Join(where, " AND ") + ` GROUP BY l.conversation_id`
	if len(having) > 0 {
		q += " HAVING " + strings.Join(having, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY max(l.ts) DESC, l.conversation_id DESC LIMIT %d", limit)
	rows, err := r.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.ConversationSummary{}
	for rows.Next() {
		var c telemetry.ConversationSummary
		if err := rows.Scan(&c.ID, &c.FirstSeen, &c.LastSeen, &c.Requests, &c.Models, &c.Accounts, &c.APIKeys, &c.UserAgent,
			&c.InputTokens, &c.OutputTokens, &c.CacheRead, &c.Credits, &c.Errors, &c.Cancelled); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConversationRequests returns a conversation's requests in order (without bodies).
func (r *Recorder) ConversationRequests(ctx context.Context, id string, limit int) ([]telemetry.RequestRecord, error) {
	if limit <= 0 || limit > conversationRequestsCap {
		limit = conversationRequestsCap
	}
	rows, err := r.db.Pool.Query(ctx, requestSelect+` FROM kiro_request_logs l
		WHERE l.conversation_id = $1 AND l.conversation_id <> '' ORDER BY l.ts, l.id LIMIT $2`, id, limit)
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

// KeyDailyUsage sums one API key's requests per local day since a time.
func (r *Recorder) KeyDailyUsage(ctx context.Context, keyID string, since time.Time, loc *time.Location) ([]telemetry.KeyUsageDay, error) {
	if loc == nil {
		loc = time.UTC
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT to_char(ts AT TIME ZONE $3, 'YYYY-MM-DD') AS d, count(*),
		count(*) FILTER (WHERE status = 'error'), COALESCE(sum(input_tokens), 0), COALESCE(sum(output_tokens), 0),
		COALESCE(sum(credits), 0)
		FROM kiro_request_logs WHERE api_key_id = $1 AND api_key_id <> '' AND ts >= $2 GROUP BY d ORDER BY d`,
		keyID, since, loc.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []telemetry.KeyUsageDay{}
	for rows.Next() {
		var d telemetry.KeyUsageDay
		if err := rows.Scan(&d.Day, &d.Requests, &d.Errors, &d.InputTokens, &d.OutputTokens, &d.Credits); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ExportRequests streams matching records, newest first, without bodies.
func (r *Recorder) ExportRequests(ctx context.Context, f telemetry.LogFilter, max int, fn func(*telemetry.RequestRecord) error) error {
	if max <= 0 {
		max = countCap
	}
	f.BeforeID = ""
	where, args := requestWhere(f)
	q := requestSelect + ` FROM kiro_request_logs l`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY l.ts DESC, l.id DESC LIMIT %d", max)
	rows, err := r.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanRequest(rows)
		if err != nil {
			return err
		}
		if err := fn(x); err != nil {
			return err
		}
	}
	return rows.Err()
}
