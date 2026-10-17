package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AlertIngestTokenStore holds alert ingest tokens by their sha256 only.
// FindByHash and Delete return an error wrapping ErrNotFound when no row
// matches; Delete matches only a token of that repository.
type AlertIngestTokenStore interface {
	Create(ctx context.Context, repositoryID uuid.UUID, label, tokenHash, defaultEnv string) (domain.AlertIngestToken, error)
	List(ctx context.Context, repositoryID uuid.UUID) ([]domain.AlertIngestToken, error)
	Delete(ctx context.Context, repositoryID, id uuid.UUID) error
	FindByHash(ctx context.Context, tokenHash string) (domain.AlertIngestToken, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
}
