package release

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Release, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Release{}, err
	}
	return s.Annotate(ctx, r), nil
}

// ForTask takes repositoryID for symmetry with the rest of the tool surface
// (every other call is scoped to a repository); the store itself resolves
// the newest release for the task without needing it.
func (s *Service) ForTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.Release, error) {
	_ = repositoryID
	r, err := s.store.ForTask(ctx, taskID)
	if err != nil {
		return domain.Release{}, err
	}
	return s.Annotate(ctx, r), nil
}

func (s *Service) List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error) {
	releases, err := s.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	return s.annotateAll(ctx, releases), nil
}
