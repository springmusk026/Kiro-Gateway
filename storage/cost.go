package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/springmusk026/Kiro-Gateway/pricing"
)

// requestCostSchema adds the API list-price equivalent of each request
// (pricing.Cost at the time it was recorded). Runs at every start; the column
// check avoids a table lock once it exists.
const requestCostSchema = `
DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'kiro_request_logs' AND column_name = 'cost_usd') THEN
		ALTER TABLE kiro_request_logs ADD COLUMN cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
	END IF;
END $$;
`

// costExpr is the SQL form of pricing.Price.Cost for one model's prices.
func costExpr(p pricing.Price, args *[]any) string {
	arg := func(v float64) string {
		*args = append(*args, v)
		return fmt.Sprintf("$%d::float8", len(*args))
	}
	rate := func(in, out, cr, cw float64) string {
		if cr == 0 {
			cr = in
		}
		if cw == 0 {
			cw = in
		}
		return fmt.Sprintf("(greatest(input_tokens - cache_read_tokens - cache_write_tokens, 0) * %s + output_tokens * %s + cache_read_tokens * %s + cache_write_tokens * %s) / 1e6",
			arg(in), arg(out), arg(cr), arg(cw))
	}
	expr := rate(p.Input, p.Output, p.CacheRead, p.CacheWrite)
	if len(p.Tiers) == 0 {
		return expr
	}
	var b strings.Builder
	b.WriteString("CASE")
	for i := len(p.Tiers) - 1; i >= 0; i-- {
		t := p.Tiers[i]
		*args = append(*args, t.Above)
		fmt.Fprintf(&b, " WHEN input_tokens > $%d THEN %s", len(*args), rate(t.Input, t.Output, t.CacheRead, t.CacheWrite))
	}
	fmt.Fprintf(&b, " ELSE %s END", expr)
	return b.String()
}

// BackfillCosts prices successful requests recorded without a list-price
// cost (before pricing existed, or while prices were unavailable). It returns
// the number of rows updated.
func (r *Recorder) BackfillCosts(ctx context.Context) (int64, error) {
	if pricing.Current() == nil {
		return 0, nil
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT DISTINCT model FROM kiro_request_logs
		WHERE cost_usd = 0 AND status = 'success' AND (input_tokens > 0 OR output_tokens > 0) AND model <> ''`)
	if err != nil {
		return 0, err
	}
	var models []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			models = append(models, m)
		}
	}
	rows.Close()
	var total int64
	for _, model := range models {
		priced, ok := pricing.Lookup(model)
		if !ok {
			continue
		}
		args := []any{}
		expr := costExpr(priced.Price, &args)
		args = append(args, model)
		tag, err := r.db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE kiro_request_logs SET cost_usd = %s
			WHERE model = $%d AND cost_usd = 0 AND status = 'success' AND (input_tokens > 0 OR output_tokens > 0)`, expr, len(args)), args...)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
	}
	return total, nil
}
