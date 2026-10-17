package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// AIJobStore persists the release engineer's suggestion jobs (migrations 191,
// 192).
type AIJobStore struct {
	pool *DB
}

func NewAIJobStore(pool *DB) *AIJobStore {
	return &AIJobStore{pool: pool}
}

var _ port.AIJobStore = (*AIJobStore)(nil)

const aiJobCols = `id, kind, repository_id, component_id, environment, status, session_id, result_summary, error,
	created, created_at, started_at, updated_at, finished_at, COALESCE(owner, '')`

func scanAIJob(row pgx.Row) (domain.AIJob, error) {
	var j domain.AIJob
	var createdJSON []byte
	if err := row.Scan(&j.ID, &j.Kind, &j.RepositoryID, &j.ComponentID, &j.Environment, &j.Status, &j.SessionID,
		&j.ResultSummary, &j.Error, &createdJSON, &j.CreatedAt, &j.StartedAt, &j.UpdatedAt, &j.FinishedAt, &j.Owner); err != nil {
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

// CreateAIJob leans on uq_ai_jobs_running (migration 192): of two requests
// racing, one inserts and the other reads the row it inserted. Should that
// row end between the two statements, the insert is tried once more.
func (s *AIJobStore) CreateAIJob(ctx context.Context, j domain.AIJob) (domain.AIJob, bool, error) {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	var owner *string
	if j.Owner != "" {
		owner = &j.Owner
	}
	for attempt := 0; ; attempt++ {
		out, err := scanAIJob(s.pool.QueryRow(ctx, `
			INSERT INTO ai_jobs (id, kind, repository_id, component_id, environment, status, owner)
			VALUES ($1,$2,$3,$4,$5,'running',$6)
			ON CONFLICT DO NOTHING
			RETURNING `+aiJobCols, j.ID, j.Kind, j.RepositoryID, j.ComponentID, j.Environment, owner))
		if err == nil {
			return out, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.AIJob{}, false, fmt.Errorf("create ai job: %w", err)
		}
		running, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs
			WHERE status = 'running' AND kind = $1 AND repository_id = $2 AND component_id IS NOT DISTINCT FROM $3
			  AND environment = $4
			ORDER BY created_at DESC LIMIT 1`, j.Kind, j.RepositoryID, j.ComponentID, j.Environment))
		if err == nil {
			return running, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) || attempt > 0 {
			return domain.AIJob{}, false, errorDefStoreErr("create ai job: read the running one", err)
		}
	}
}

func (s *AIJobStore) GetAIJob(ctx context.Context, id uuid.UUID) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs WHERE id = $1`, id))
	return j, errorDefStoreErr("get ai job", err)
}

func (s *AIJobStore) GetAIJobBySession(ctx context.Context, sessionID uuid.UUID) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `SELECT `+aiJobCols+` FROM ai_jobs WHERE session_id = $1`, sessionID))
	return j, errorDefStoreErr("get ai job by session", err)
}

func (s *AIJobStore) SetAIJobSession(ctx context.Context, id, sessionID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `UPDATE ai_jobs SET session_id = $2, updated_at = now() WHERE id = $1`, id, sessionID); err != nil {
		return fmt.Errorf("set ai job session: %w", err)
	}
	return nil
}

func (s *AIJobStore) StartAIJob(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE ai_jobs SET started_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running' AND started_at IS NULL`, id)
	if err != nil {
		return false, fmt.Errorf("start ai job: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *AIJobStore) AppendAIJobItem(ctx context.Context, id uuid.UUID, item domain.AIJobItem) error {
	raw, err := json.Marshal([]domain.AIJobItem{item})
	if err != nil {
		return fmt.Errorf("append ai job item: %w", err)
	}
	same, err := json.Marshal([]map[string]any{{"kind": item.Kind, "id": item.ID}})
	if err != nil {
		return fmt.Errorf("append ai job item: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE ai_jobs SET created = created || $2::jsonb, updated_at = now()
		WHERE id = $1 AND status = 'running' AND NOT created @> $3::jsonb`, id, raw, same); err != nil {
		return fmt.Errorf("append ai job item: %w", err)
	}
	return nil
}

func (s *AIJobStore) FinishAIJob(ctx context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) (domain.AIJob, error) {
	j, err := scanAIJob(s.pool.QueryRow(ctx, `
		UPDATE ai_jobs SET status = $2, result_summary = $3, error = $4, finished_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running'
		RETURNING `+aiJobCols, id, status, summary, errMsg))
	return j, errorDefStoreErr("finish ai job", err)
}

func (s *AIJobStore) FailStaleAIJobs(ctx context.Context, startedBefore, queuedBefore time.Time, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_jobs SET status = 'failed', error = $3, finished_at = now(), updated_at = now()
		WHERE status = 'running'
		  AND ((started_at IS NOT NULL AND started_at < $1) OR (started_at IS NULL AND created_at < $2))`,
		startedBefore, queuedBefore, reason)
	if err != nil {
		return 0, fmt.Errorf("fail stale ai jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *AIJobStore) FailRunningAIJobs(ctx context.Context, owner string, before time.Time, reason string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_jobs SET status = 'failed', error = $3, finished_at = now(), updated_at = now()
		WHERE status = 'running' AND owner IS DISTINCT FROM $1 AND created_at <= $2`, owner, before, reason)
	if err != nil {
		return 0, fmt.Errorf("fail running ai jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
