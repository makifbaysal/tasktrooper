package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskReviewStore struct {
	pool *DB
}

func NewTaskReviewStore(pool *DB) *TaskReviewStore {
	return &TaskReviewStore{pool: pool}
}

var _ port.TaskReviewStore = (*TaskReviewStore)(nil)

func (s *TaskReviewStore) TaskSpans(ctx context.Context, taskID uuid.UUID) ([]domain.TaskColumnSpan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, repository_id, board_column, agent_id,
		       entered_at, left_at, duration_seconds, visit_no
		FROM task_column_spans
		WHERE task_id = $1
		ORDER BY entered_at, visit_no
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("task spans: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskColumnSpan
	for rows.Next() {
		var sp domain.TaskColumnSpan
		if err := rows.Scan(&sp.ID, &sp.TaskID, &sp.RepositoryID, &sp.BoardColumn, &sp.AgentID,
			&sp.EnteredAt, &sp.LeftAt, &sp.DurationSeconds, &sp.VisitNo); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *TaskReviewStore) RecordReviewVerdict(ctx context.Context, v domain.TaskReviewVerdict) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_review_verdicts (task_id, span_id, agent_id, agent_name, verdict)
		VALUES ($1, $2, $3, COALESCE((SELECT name FROM agents WHERE id = $3), $4), $5)
		ON CONFLICT (span_id, agent_id) DO UPDATE
		SET verdict = EXCLUDED.verdict, decided_at = now()
	`, v.TaskID, v.SpanID, v.AgentID, v.AgentName, v.Verdict)
	if err != nil {
		return fmt.Errorf("record review verdict: %w", err)
	}
	return nil
}

func (s *TaskReviewStore) ListReviewVerdicts(ctx context.Context, taskID uuid.UUID) ([]domain.TaskReviewVerdict, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT task_id, span_id, agent_id, agent_name, verdict, decided_at
		FROM task_review_verdicts
		WHERE task_id = $1
		ORDER BY decided_at
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list review verdicts: %w", err)
	}
	defer rows.Close()
	var out []domain.TaskReviewVerdict
	for rows.Next() {
		var v domain.TaskReviewVerdict
		if err := rows.Scan(&v.TaskID, &v.SpanID, &v.AgentID, &v.AgentName, &v.Verdict, &v.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *TaskReviewStore) RequiredReviewers(ctx context.Context, column string, taskType string) ([]domain.ReviewerRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name
		FROM agent_column_subscriptions s
		JOIN agents a ON a.id = s.agent_id
		WHERE s.column_slug = $1
		  AND (s.task_type_filter IS NULL OR $2 = ANY(s.task_type_filter))
		  AND a.enabled
		ORDER BY a.name
	`, column, taskType)
	if err != nil {
		return nil, fmt.Errorf("required reviewers: %w", err)
	}
	defer rows.Close()
	var out []domain.ReviewerRef
	for rows.Next() {
		var r domain.ReviewerRef
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
