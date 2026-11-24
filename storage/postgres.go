// Package storage provides the PostgreSQL backend: configuration (settings,
// accounts, API keys), stored responses, request logs and audit events.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the connection pool shared by all PostgreSQL-backed stores.
type DB struct {
	Pool *pgxpool.Pool
}

// Open connects, verifies the connection and applies pending migrations.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.MaxConns < 8 {
		cfg.MaxConns = 8
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	db := &DB{Pool: pool}
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() { db.Pool.Close() }

// migrations are applied in order, each exactly once, inside a transaction.
var migrations = []string{
	// 1: configuration, stored responses, request logs, audit events.
	`
CREATE TABLE kiro_settings (
	id         SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
	data       JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE kiro_accounts (
	id         TEXT PRIMARY KEY,
	position   INT NOT NULL,
	email      TEXT NOT NULL DEFAULT '',
	enabled    BOOLEAN NOT NULL DEFAULT TRUE,
	data       JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE kiro_api_keys (
	id         TEXT PRIMARY KEY,
	position   INT NOT NULL,
	data       JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE kiro_responses (
	id        TEXT PRIMARY KEY,
	data      JSONB NOT NULL,
	stored_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX kiro_responses_stored_at ON kiro_responses (stored_at);
CREATE TABLE kiro_request_logs (
	id             TEXT PRIMARY KEY,
	ts             TIMESTAMPTZ NOT NULL,
	method         TEXT NOT NULL DEFAULT '',
	path           TEXT NOT NULL DEFAULT '',
	endpoint       TEXT NOT NULL DEFAULT '',
	model          TEXT NOT NULL DEFAULT '',
	account_id     TEXT NOT NULL DEFAULT '',
	account_email  TEXT NOT NULL DEFAULT '',
	api_key_id     TEXT NOT NULL DEFAULT '',
	client_ip      TEXT NOT NULL DEFAULT '',
	user_agent     TEXT NOT NULL DEFAULT '',
	stream         BOOLEAN NOT NULL DEFAULT FALSE,
	protocol       TEXT NOT NULL DEFAULT '',
	status         TEXT NOT NULL DEFAULT '',
	http_status    INT NOT NULL DEFAULT 0,
	error          TEXT NOT NULL DEFAULT '',
	error_type     TEXT NOT NULL DEFAULT '',
	input_tokens   INT NOT NULL DEFAULT 0,
	output_tokens  INT NOT NULL DEFAULT 0,
	credits        DOUBLE PRECISION NOT NULL DEFAULT 0,
	duration_ms    BIGINT NOT NULL DEFAULT 0,
	ttfb_ms        BIGINT NOT NULL DEFAULT 0,
	attempts       INT NOT NULL DEFAULT 0,
	request_bytes  BIGINT NOT NULL DEFAULT 0,
	response_bytes BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX kiro_request_logs_ts ON kiro_request_logs (ts DESC);
CREATE INDEX kiro_request_logs_account_ts ON kiro_request_logs (account_id, ts DESC);
CREATE INDEX kiro_request_logs_model_ts ON kiro_request_logs (model, ts DESC);
CREATE INDEX kiro_request_logs_status_ts ON kiro_request_logs (status, ts DESC);
CREATE TABLE kiro_request_bodies (
	id            TEXT PRIMARY KEY REFERENCES kiro_request_logs (id) ON DELETE CASCADE,
	request_body  TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT ''
);
CREATE TABLE kiro_audit_events (
	id     BIGSERIAL PRIMARY KEY,
	ts     TIMESTAMPTZ NOT NULL,
	actor  TEXT NOT NULL DEFAULT '',
	ip     TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	target TEXT NOT NULL DEFAULT '',
	detail JSONB
);
CREATE INDEX kiro_audit_events_ts ON kiro_audit_events (ts DESC);
`,
	// 2: upstream prompt-cache accounting per request.
	`
ALTER TABLE kiro_request_logs ADD COLUMN cache_read_tokens INT NOT NULL DEFAULT 0;
ALTER TABLE kiro_request_logs ADD COLUMN cache_write_tokens INT NOT NULL DEFAULT 0;
`,
}

// idempotentSchemas run after the numbered migrations on every start, under
// the same lock. They only use IF NOT EXISTS, so they need no version number
// and cannot collide with numbered migrations added on other branches.
var idempotentSchemas = []struct{ name, sql string }{
	{"admin sessions", adminSessionsSchema},
}

func (db *DB) migrate(ctx context.Context) error {
	if _, err := db.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS kiro_schema_migrations (
		version    INT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	// Serialize concurrent starters (e.g. two replicas booting together).
	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(727101)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727101)`)

	var current int
	if err := conn.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM kiro_schema_migrations`).Scan(&current); err != nil {
		return err
	}
	for i := current; i < len(migrations); i++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, migrations[i]); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kiro_schema_migrations (version) VALUES ($1)`, i+1); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	for _, s := range idempotentSchemas {
		if _, err := conn.Exec(ctx, s.sql); err != nil {
			return fmt.Errorf("apply schema %q: %w", s.name, err)
		}
	}
	return nil
}
