package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// AIJobStore persists the release engineer's suggestion jobs (migration 191).
type AIJobStore struct {
	pool *DB
}

func NewAIJobStore(pool *DB) *AIJobStore {
	return &AIJobStore{pool: pool}
}

var _ port.AIJobStore = (*AIJobStore)(nil)

const aiJobCols = `id, kind, repository_id, component_id, environment, status, session_id, result_summary, error,
	created, created_at, updated_at, finished_at`

func scanAIJob(row pgx.Row) (domain.AIJob, error) {
	var j domain.AIJob
	var createdJSON []byte
	if err := row.Scan(&j.ID, &j.Kind, &j.RepositoryID, &j.ComponentID, &j.Environment, &j.Status, &j.SessionID,
		&j.ResultSummary, &j.Error, &createdJSON, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt); err != nil {
		return domain.AIJob{}, err
	}
	if err := json.Unmarshal(createdJSON, &j.Created); err != nil {
		return domain.AIJob{}, fmt.Errorf("unmarshal ai job items: %w", err)
	}
	if j.Created == nil {
		j.Created = []domain.AIJobItem{}
	}
	return j, nil
}

func (s *AIJobStore) CreateAIJob(ctx context.Context, j domain.AIJob) (domain.AIJob, error) {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	out, err := scanAIJob(s.pool.QueryRow(ctx, `
		INSERT INTO ai_jobs (id, kind, repository_id, component_id, environment, status)
		VALUES ($1,$2,$3,$4,$5,'running')
		RETURNING `+aiJobCols, j.ID, j.Kind, j.RepositoryID, j.ComponentID, j.Environment))
	if err != nil {
		return domain.AIJob{}, fmt.Errorf("create ai job: %w", err)
	}
	return out, nil
}

func (s *AIJobStore) GetAIJob(ctx context.Context, id uuid.UUID) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs WHERE id = $1`, id))
	return j, errorDefStoreErr("get ai job", err)
}

func (s *AIJobStore) GetAIJobBySession(ctx context.Context, sessionID uuid.UUID) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs WHERE session_id = $1`, sessionID))
	return j, errorDefStoreErr("get ai job by session", err)
}

func (s *AIJobStore) RunningAIJob(ctx context.Context, kind domain.AIJobKind, repositoryID uuid.UUID, componentID *uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs
		WHERE status = 'running' AND kind = $1 AND repository_id = $2 AND component_id IS NOT DISTINCT FROM $3
		  AND environment = $4
		ORDER BY created_at DESC LIMIT 1`, kind, repositoryID, componentID, env))
	return j, errorDefStoreErr("running ai job", err)
}

func (s *AIJobStore) SetAIJobSession(ctx context.Context, id, sessionID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE ai_jobs SET session_id = $2, updated_at = now() WHERE id = $1`, id, sessionID); err != nil {
		return fmt.Errorf("set ai job session: %w", err)
	}
	return nil
}

func (s *AIJobStore) AppendAIJobItem(ctx context.Context, id uuid.UUID, item domain.AIJobItem) error {
	raw, err := json.Marshal([]domain.AIJobItem{item})
	if err != nil {
		return fmt.Errorf("append ai job item: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE ai_jobs SET created = created || $2::jsonb, updated_at = now() WHERE id = $1`,
		id, raw); err != nil {
		return fmt.Errorf("append ai job item: %w", err)
	}
	return nil
}

func (s *AIJobStore) FinishAIJob(ctx context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `
		UPDATE ai_jobs SET status = $2, result_summary = $3, error = $4, finished_at = now(), updated_at = now()
		WHERE id = $1
		RETURNING `+aiJobCols, id, status, summary, errMsg))
	return j, errorDefStoreErr("finish ai job", err)
}

func (s *AIJobStore) FailRunningAIJobs(ctx context.Context, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_jobs SET status = 'failed', error = $1, finished_at = now(), updated_at = now()
		WHERE status = 'running'`, reason)
	if err != nil {
		return 0, fmt.Errorf("fail running ai jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
