package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ReadinessStore struct {
	pool *DB
}

func NewReadinessStore(pool *DB) *ReadinessStore {
	return &ReadinessStore{pool: pool}
}

func (s *ReadinessStore) List(ctx context.Context, repositoryID uuid.UUID) ([]domain.ReadinessStepState, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT step_id, skipped, task_id FROM repository_readiness_steps
		WHERE repository_id = $1 ORDER BY step_id`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list readiness steps: %w", err)
	}
	defer rows.Close()
	out := []domain.ReadinessStepState{}
	for rows.Next() {
		var st domain.ReadinessStepState
		if err := rows.Scan(&st.StepID, &st.Skipped, &st.TaskID); err != nil {
			return nil, fmt.Errorf("scan readiness step: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *ReadinessStore) SetSkipped(ctx context.Context, repositoryID uuid.UUID, stepID string, skipped bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO repository_readiness_steps (repository_id, step_id, skipped)
		VALUES ($1, $2, $3)
		ON CONFLICT (repository_id, step_id) DO UPDATE SET skipped = EXCLUDED.skipped, updated_at = now()`,
		repositoryID, stepID, skipped)
	if err != nil {
		return fmt.Errorf("set readiness skipped: %w", err)
	}
	return nil
}

func (s *ReadinessStore) SetTask(ctx context.Context, repositoryID uuid.UUID, stepID string, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO repository_readiness_steps (repository_id, step_id, task_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (repository_id, step_id) DO UPDATE SET task_id = EXCLUDED.task_id, updated_at = now()`,
		repositoryID, stepID, taskID)
	if err != nil {
		return fmt.Errorf("set readiness task: %w", err)
	}
	return nil
}
