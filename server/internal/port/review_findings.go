package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReviewFindingStore is task_review_findings and task_review_flag_acks
// (migration 202): what reviewers reported, round by round, and which flags a
// person has marked as looked at.
type ReviewFindingStore interface {
	Insert(ctx context.Context, rows []domain.TaskReviewFinding) error
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskReviewFinding, error)
	ListAcks(ctx context.Context, taskID uuid.UUID) ([]domain.ReviewFlagAck, error)
	// Ack is idempotent: the first person to mark a flag keeps the credit.
	Ack(ctx context.Context, ack domain.ReviewFlagAck) (domain.ReviewFlagAck, error)
}
