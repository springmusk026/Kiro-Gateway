package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"kiro-go/telemetry"
)

// adminSessionsSchema creates the console session table. It is idempotent and
// runs at every start (idempotentSchemas in postgres.go) rather than as a
// numbered migration.
const adminSessionsSchema = `
CREATE TABLE IF NOT EXISTS kiro_admin_sessions (
	id_hash        TEXT PRIMARY KEY,
	password_check TEXT NOT NULL,
	remember       BOOLEAN NOT NULL DEFAULT FALSE,
	created_at     TIMESTAMPTZ NOT NULL,
	last_seen      TIMESTAMPTZ NOT NULL,
	expires_at     TIMESTAMPTZ NOT NULL,
	ip             TEXT NOT NULL DEFAULT '',
	user_agent     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS kiro_admin_sessions_expires_at ON kiro_admin_sessions (expires_at);
`

// SessionStore keeps console sessions (hashed ids) in PostgreSQL.
type SessionStore struct {
	db *DB
}

var _ telemetry.AdminSessionStore = (*SessionStore)(nil)

// NewSessionStore returns the session store, creating its table if needed.
func NewSessionStore(ctx context.Context, db *DB) (*SessionStore, error) {
	if _, err := db.Pool.Exec(ctx, adminSessionsSchema); err != nil {
		return nil, err
	}
	return &SessionStore{db: db}, nil
}

func (s *SessionStore) SaveAdminSession(ctx context.Context, a *telemetry.AdminSession) error {
	_, err := s.db.Pool.Exec(ctx, `INSERT INTO kiro_admin_sessions
		(id_hash, password_check, remember, created_at, last_seen, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id_hash) DO UPDATE SET password_check = EXCLUDED.password_check, remember = EXCLUDED.remember,
		last_seen = EXCLUDED.last_seen, expires_at = EXCLUDED.expires_at`,
		a.IDHash, a.PasswordCheck, a.Remember, a.CreatedAt, a.LastSeen, a.ExpiresAt, a.IP, a.UserAgent)
	return err
}

func (s *SessionStore) LoadAdminSession(ctx context.Context, idHash string) (*telemetry.AdminSession, error) {
	a := &telemetry.AdminSession{IDHash: idHash}
	err := s.db.Pool.QueryRow(ctx, `SELECT password_check, remember, created_at, last_seen, expires_at, ip, user_agent
		FROM kiro_admin_sessions WHERE id_hash = $1`, idHash).
		Scan(&a.PasswordCheck, &a.Remember, &a.CreatedAt, &a.LastSeen, &a.ExpiresAt, &a.IP, &a.UserAgent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *SessionStore) TouchAdminSession(ctx context.Context, idHash string, lastSeen time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE kiro_admin_sessions SET last_seen = $2 WHERE id_hash = $1 AND last_seen < $2`, idHash, lastSeen)
	return err
}

func (s *SessionStore) DeleteAdminSession(ctx context.Context, idHash string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM kiro_admin_sessions WHERE id_hash = $1`, idHash)
	return err
}

func (s *SessionStore) DeleteAllAdminSessions(ctx context.Context) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM kiro_admin_sessions`)
	return err
}

func (s *SessionStore) PurgeAdminSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM kiro_admin_sessions WHERE expires_at < $1`, before)
	return err
}
