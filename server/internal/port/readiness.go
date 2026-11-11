package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReadinessStore keeps a repository's checklist choices: which steps the
// person skipped, which board task a step opened, and whether the card is
// hidden. Everything else on the checklist is derived from live data.
type ReadinessStore interface {
	List(ctx context.Context, repositoryID uuid.UUID) ([]domain.ReadinessStepState, error)
	SetSkipped(ctx context.Context, repositoryID uuid.UUID, stepID string, skipped bool) error
	SetTask(ctx context.Context, repositoryID uuid.UUID, stepID string, taskID uuid.UUID) error
}
