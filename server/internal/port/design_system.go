package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type DesignSystemStore interface {
	// Create writes a new version for the proposal's target, numbered one past
	// the target's highest.
	Create(ctx context.Context, ds domain.DesignSystem) (domain.DesignSystem, error)
	UpdateContent(ctx context.Context, ds domain.DesignSystem) (domain.DesignSystem, error)
	Get(ctx context.Context, id uuid.UUID) (domain.DesignSystem, error)
	ListForProject(ctx context.Context, projectID uuid.UUID) ([]domain.DesignSystem, error)
	ListForRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DesignSystem, error)
	ListBySourceTask(ctx context.Context, taskID uuid.UUID) ([]domain.DesignSystem, error)
	// Approve marks the version approved and the target's previously approved
	// version superseded, in one transaction.
	Approve(ctx context.Context, id uuid.UUID) (domain.DesignSystem, error)

	CreateRequest(ctx context.Context, req domain.DesignSystemRequest) (domain.DesignSystemRequest, error)
	LatestRequestForProject(ctx context.Context, projectID uuid.UUID) (*domain.DesignSystemRequest, error)
	LatestRequestForRepository(ctx context.Context, repositoryID uuid.UUID) (*domain.DesignSystemRequest, error)

	// RepositoryBaseProject is the project a repository explicitly builds on;
	// nil when none was chosen.
	RepositoryBaseProject(ctx context.Context, repositoryID uuid.UUID) (*uuid.UUID, error)
	SetRepositoryBaseProject(ctx context.Context, repositoryID uuid.UUID, projectID *uuid.UUID) error
}
