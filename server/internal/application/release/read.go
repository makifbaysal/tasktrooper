package release

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Release, error) {
	return s.store.Get(ctx, id)
}

// ForTask takes repositoryID for symmetry with the rest of the tool surface
// (every other call is scoped to a repository); the store itself resolves
// the newest release for the task without needing it.
func (s *Service) ForTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error) {
	_ = repositoryID
	return s.store.ForTask(ctx, taskID)
}

func (s *Service) List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error) {
	return s.store.List(ctx, f)
}
