package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type EnvRequirementStore interface {
	ListEnvRequirements(ctx context.Context, repositoryID uuid.UUID) ([]domain.EnvRequirement, error)
	// UpsertEnvRequirement keys on (repository, component, name). An agent
	// row never overwrites a human one; the stored row is returned either way.
	UpsertEnvRequirement(ctx context.Context, req domain.EnvRequirement) (domain.EnvRequirement, error)
}
