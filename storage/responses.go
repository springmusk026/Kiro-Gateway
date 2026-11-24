package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ResponseStore keeps stored /v1/responses objects in kiro_responses.
type ResponseStore struct{ db *DB }

func NewResponseStore(db *DB) *ResponseStore { return &ResponseStore{db: db} }

var ErrNotFound = errors.New("not found")

func (s *ResponseStore) SaveResponse(ctx context.Context, id string, doc []byte, storedAt time.Time) error {
	_, err := s.db.Pool.Exec(ctx, `INSERT INTO kiro_responses (id, data, stored_at) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, stored_at = EXCLUDED.stored_at`, id, doc, storedAt)
	return err
}

func (s *ResponseStore) LoadResponse(ctx context.Context, id string) ([]byte, time.Time, error) {
	var doc []byte
	var at time.Time
	err := s.db.Pool.QueryRow(ctx, `SELECT data, stored_at FROM kiro_responses WHERE id = $1`, id).Scan(&doc, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, ErrNotFound
	}
	return doc, at, err
}

// PurgeResponses deletes expired objects and keeps at most keepMax newest.
func (s *ResponseStore) PurgeResponses(ctx context.Context, olderThan time.Time, keepMax int) (int, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM kiro_responses WHERE stored_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	if keepMax > 0 {
		tag, err = s.db.Pool.Exec(ctx, `DELETE FROM kiro_responses WHERE id IN (
			SELECT id FROM kiro_responses ORDER BY stored_at DESC OFFSET $1)`, keepMax)
		if err != nil {
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}
