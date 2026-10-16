package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type AlertIngestTokenStore struct {
	pool *DB
}

func NewAlertIngestTokenStore(pool *DB) *AlertIngestTokenStore {
	return &AlertIngestTokenStore{pool: pool}
}

const alertTokenCols = `id, repository_id, label, default_env, created_at, last_used_at`

func scanAlertToken(row pgx.Row) (domain.AlertIngestToken, error) {
	var t domain.AlertIngestToken
	err := row.Scan(&t.ID, &t.RepositoryID, &t.Label, &t.DefaultEnv, &t.CreatedAt, &t.LastUsedAt)
	return t, err
}

func (s *AlertIngestTokenStore) Create(ctx context.Context, repositoryID uuid.UUID, label, tokenHash, defaultEnv string) (domain.AlertIngestToken, error) {
	t, err := scanAlertToken(s.pool.QueryRow(ctx, `
		INSERT INTO alert_ingest_tokens (repository_id, label, token_hash, default_env)
		VALUES ($1, $2, $3, $4)
		RETURNING `+alertTokenCols, repositoryID, label, tokenHash, defaultEnv))
	if err != nil {
		return domain.AlertIngestToken{}, fmt.Errorf("create alert ingest token: %w", err)
	}
	return t, nil
}

func (s *AlertIngestTokenStore) List(ctx context.Context, repositoryID uuid.UUID) ([]domain.AlertIngestToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+alertTokenCols+` FROM alert_ingest_tokens
		WHERE repository_id = $1
		ORDER BY created_at DESC, id`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list alert ingest tokens: %w", err)
	}
	defer rows.Close()
	out := []domain.AlertIngestToken{}
	for rows.Next() {
		t, err := scanAlertToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert ingest token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *AlertIngestTokenStore) Delete(ctx context.Context, repositoryID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_ingest_tokens WHERE id = $1 AND repository_id = $2`, id, repositoryID)
	if err != nil {
		return fmt.Errorf("delete alert ingest token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete alert ingest token: %w", port.ErrNotFound)
	}
	return nil
}

func (s *AlertIngestTokenStore) FindByHash(ctx context.Context, tokenHash string) (domain.AlertIngestToken, error) {
	t, err := scanAlertToken(s.pool.QueryRow(ctx, `
		SELECT `+alertTokenCols+` FROM alert_ingest_tokens WHERE token_hash = $1`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AlertIngestToken{}, fmt.Errorf("find alert ingest token: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.AlertIngestToken{}, fmt.Errorf("find alert ingest token: %w", err)
	}
	return t, nil
}

func (s *AlertIngestTokenStore) MarkUsed(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE alert_ingest_tokens SET last_used_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("mark alert ingest token used: %w", err)
	}
	return nil
}
