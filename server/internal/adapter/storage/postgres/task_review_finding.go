package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type ReviewFindingStore struct{ pool *DB }

func NewReviewFindingStore(pool *DB) *ReviewFindingStore { return &ReviewFindingStore{pool: pool} }

var _ port.ReviewFindingStore = (*ReviewFindingStore)(nil)

func (s *ReviewFindingStore) Insert(ctx context.Context, rows []domain.TaskReviewFinding) error {
	for _, r := range rows {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO task_review_findings
				(task_id, kind, gate, agent_id, agent_name, head_sha, fingerprint, file, line, severity, category,
				 message, confidence, human_review_reason, question, scope, reason, status, criterion_id, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
			r.TaskID, r.Kind, r.Gate, r.AgentID, r.AgentName, r.HeadSHA, r.Fingerprint, r.File, r.Line, r.Severity,
			r.Category, r.Message, r.Confidence, r.HumanReviewReason, r.Question, r.Scope, r.Reason, r.Status, r.CriterionID, r.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert review finding: %w", err)
		}
	}
	return nil
}

func (s *ReviewFindingStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskReviewFinding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, kind, gate, agent_id, agent_name, head_sha, fingerprint, file, line, severity, category,
		       message, confidence, human_review_reason, question, scope, reason, status, criterion_id, created_at
		FROM task_review_findings WHERE task_id = $1 ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list review findings: %w", err)
	}
	defer rows.Close()
	out := []domain.TaskReviewFinding{}
	for rows.Next() {
		var r domain.TaskReviewFinding
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Kind, &r.Gate, &r.AgentID, &r.AgentName, &r.HeadSHA, &r.Fingerprint,
			&r.File, &r.Line, &r.Severity, &r.Category, &r.Message, &r.Confidence, &r.HumanReviewReason,
			&r.Question, &r.Scope, &r.Reason, &r.Status, &r.CriterionID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ReviewFindingStore) ListAcks(ctx context.Context, taskID uuid.UUID) ([]domain.ReviewFlagAck, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT task_id, flag_key, reviewed_by, reviewed_at FROM task_review_flag_acks WHERE task_id = $1`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list review flag acks: %w", err)
	}
	defer rows.Close()
	out := []domain.ReviewFlagAck{}
	for rows.Next() {
		var a domain.ReviewFlagAck
		if err := rows.Scan(&a.TaskID, &a.FlagKey, &a.ReviewedBy, &a.ReviewedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *ReviewFindingStore) Ack(ctx context.Context, ack domain.ReviewFlagAck) (domain.ReviewFlagAck, error) {
	var out domain.ReviewFlagAck
	err := s.pool.QueryRow(ctx, `
		INSERT INTO task_review_flag_acks (task_id, flag_key, reviewed_by, reviewed_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (task_id, flag_key) DO UPDATE SET flag_key = EXCLUDED.flag_key
		RETURNING task_id, flag_key, reviewed_by, reviewed_at`,
		ack.TaskID, ack.FlagKey, ack.ReviewedBy, ack.ReviewedAt).
		Scan(&out.TaskID, &out.FlagKey, &out.ReviewedBy, &out.ReviewedAt)
	if err != nil {
		return out, fmt.Errorf("ack review flag: %w", err)
	}
	return out, nil
}
