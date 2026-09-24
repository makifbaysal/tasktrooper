package projectmodel

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// GetComponent and ComponentByPath back repository.Service's ComponentResolver:
// validating a task's component_id belongs to its repository and is active.

func (s *Service) GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error) {
	comp, err := s.store.GetComponent(ctx, id)
	if err != nil {
		return domain.Component{}, fmt.Errorf("get component: %w", err)
	}
	return comp, nil
}

func (s *Service) ComponentByPath(ctx context.Context, repositoryID uuid.UUID, path string) (domain.Component, error) {
	norm, err := domain.NormalizeComponentPath(path)
	if err != nil {
		return domain.Component{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return domain.Component{}, fmt.Errorf("component by path: %w", err)
	}
	if comp, ok := activeComponentByPath(components, norm); ok {
		return comp, nil
	}
	return domain.Component{}, fmt.Errorf("%w: no active component at path %q", ErrInvalidInput, norm)
}
