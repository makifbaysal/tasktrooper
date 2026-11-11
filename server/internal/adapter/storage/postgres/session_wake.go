package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SessionWakeStore struct{ pool *DB }

func NewSessionWakeStore(pool *DB) *SessionWakeStore { return &SessionWakeStore{pool: pool} }

const wakeColumns = `id, session_id, kind, ref_id, label, created_at, expires_at`

func (s *SessionWakeStore) Add(ctx context.Context, w domain.SessionWake) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO session_wakes (session_id, kind, ref_id, label, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id, kind, ref_id) DO UPDATE SET label = EXCLUDED.label, expires_at = EXCLUDED.expires_at
	`, w.SessionID, w.Kind, w.RefID, w.Label, w.ExpiresAt)
	if err != nil {
		return fmt.Errorf("add session wake: %w", err)
	}
	return nil
}

func (s *SessionWakeStore) ListOpen(ctx context.Context) ([]domain.SessionWake, error) {
	return s.query(ctx, `SELECT `+wakeColumns+` FROM session_wakes ORDER BY created_at`)
}

func (s *SessionWakeStore) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionWake, error) {
	return s.query(ctx, `SELECT `+wakeColumns+` FROM session_wakes WHERE session_id = $1 ORDER BY created_at`, sessionID)
}

func (s *SessionWakeStore) query(ctx context.Context, sql string, args ...any) ([]domain.SessionWake, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list session wakes: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.SessionWake, error) {
		var w domain.SessionWake
		err := r.Scan(&w.ID, &w.SessionID, &w.Kind, &w.RefID, &w.Label, &w.CreatedAt, &w.ExpiresAt)
		return w, err
	})
}

func (s *SessionWakeStore) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM session_wakes WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("delete session wake: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *SessionWakeStore) DeleteBySession(ctx context.Context, sessionID uuid.UUID) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM session_wakes WHERE session_id = $1`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("delete session wakes: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
