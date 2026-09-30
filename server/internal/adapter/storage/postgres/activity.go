package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ActivityStore struct {
	pool *DB
}

func NewActivityStore(pool *DB) *ActivityStore {
	return &ActivityStore{pool: pool}
}

func (a *ActivityStore) CreateRun(ctx context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error) {
	var run domain.SessionRun
	err := a.pool.QueryRow(ctx, `
		INSERT INTO session_runs (session_id, request_id, model, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id, session_id, request_id, status, model, started_at, completed_at
	`, sessionID, requestID, model).Scan(
		&run.ID, &run.SessionID, &run.RequestID, &run.Status, &run.Model, &run.StartedAt, &run.CompletedAt,
	)
	if err != nil {
		return domain.SessionRun{}, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (a *ActivityStore) CompleteRun(ctx context.Context, runID uuid.UUID, status string) error {
	_, err := a.pool.Exec(ctx, `
		UPDATE session_runs SET
			status = CASE WHEN session_runs.status = 'cancelled' THEN 'cancelled' ELSE $2 END,
			completed_at = now()
		WHERE id = $1
	`, runID, status)
	return err
}

func (a *ActivityStore) CancelRun(ctx context.Context, runID uuid.UUID) (bool, error) {
	var id uuid.UUID
	err := a.pool.QueryRow(ctx, `
		UPDATE session_runs SET status = 'cancelled', completed_at = now()
		WHERE id = $1 AND status = 'running'
		RETURNING id
	`, runID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("cancel run: %w", err)
	}
	return true, nil
}

func (a *ActivityStore) RunStatus(ctx context.Context, runID uuid.UUID) (string, error) {
	var status string
	err := a.pool.QueryRow(ctx, `SELECT status FROM session_runs WHERE id = $1`, runID).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("run status: %w", err)
	}
	return status, nil
}

func (a *ActivityStore) AppendStep(ctx context.Context, runID uuid.UUID, stepType string, payload []byte) error {
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte("{}")
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO session_steps (run_id, step_type, payload) VALUES ($1, $2, $3)
	`, runID, stepType, payload)
	return err
}

func (a *ActivityStore) ListRunsBySession(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.SessionRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := a.pool.Query(ctx, `
		SELECT id, session_id, request_id, status, model, started_at, completed_at
		FROM session_runs WHERE session_id = $1 ORDER BY started_at DESC LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRuns(rows)
}

func (a *ActivityStore) ListStepsByRun(ctx context.Context, runID uuid.UUID) ([]domain.SessionStep, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT id, run_id, step_type, payload, created_at
		FROM session_steps WHERE run_id = $1 ORDER BY created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var steps []domain.SessionStep
	for rows.Next() {
		var s domain.SessionStep
		if err := rows.Scan(&s.ID, &s.RunID, &s.StepType, &s.Payload, &s.CreatedAt); err != nil {
			return nil, err
		}
		if len(s.Payload) == 0 || string(s.Payload) == "null" {
			s.Payload = json.RawMessage("{}")
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

func (a *ActivityStore) ListActiveRuns(ctx context.Context) ([]domain.SessionRun, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT sr.id, sr.session_id, sr.request_id, sr.status, sr.model, sr.started_at, sr.completed_at,
			s.agent_id, s.title
		FROM session_runs sr
		LEFT JOIN sessions s ON s.id = sr.session_id
		WHERE sr.status = 'running' ORDER BY sr.started_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []domain.SessionRun
	for rows.Next() {
		var r domain.SessionRun
		var title *string
		if err := rows.Scan(&r.ID, &r.SessionID, &r.RequestID, &r.Status, &r.Model, &r.StartedAt, &r.CompletedAt,
			&r.AgentID, &title); err != nil {
			return nil, err
		}
		if title != nil {
			r.Title = *title
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func scanRuns(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.SessionRun, error) {
	var runs []domain.SessionRun
	for rows.Next() {
		var r domain.SessionRun
		if err := rows.Scan(&r.ID, &r.SessionID, &r.RequestID, &r.Status, &r.Model, &r.StartedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
