package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskReviewStore is the per-reviewer verdict ledger of the quorum review
// columns (domain.QuorumReviewColumn).
type TaskReviewStore interface {
	// TaskSpans lists every column visit of the task, oldest first.
	TaskSpans(ctx context.Context, taskID uuid.UUID) ([]domain.TaskColumnSpan, error)
	// RecordReviewVerdict upserts the reviewer's verdict on its span.
	RecordReviewVerdict(ctx context.Context, v domain.TaskReviewVerdict) error
	ListReviewVerdicts(ctx context.Context, taskID uuid.UUID) ([]domain.TaskReviewVerdict, error)
	// RequiredReviewers is every enabled agent subscribed to column for
	// taskType.
	RequiredReviewers(ctx context.Context, column string, taskType string) ([]domain.ReviewerRef, error)
}
