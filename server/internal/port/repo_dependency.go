package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// RepoDependencyStore persists repository_dependencies: one edge from a
// repository to a sub-project, another repository, or a manually-recorded
// database.
type RepoDependencyStore interface {
	ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error)
	// ListByProject splits every dependency touching a project's repositories
	// into outgoing (source repo is in the project) and incoming (target repo
	// is in the project, source is not).
	ListByProject(ctx context.Context, projectID uuid.UUID) (outgoing []domain.RepoDependency, incoming []domain.RepoDependency, err error)
	// Get returns an error wrapping ErrNotFound when the id has no row.
	Get(ctx context.Context, id uuid.UUID) (domain.RepoDependency, error)
	Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error)
	// Update applies req. A "" or masked DatabaseSecret leaves the stored
	// secret untouched.
	Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// SetCipher injects the cipher derived at boot, before the process
	// environment is scrubbed of MCP_SECRETS_KEY — the same reason
	// RepositoryStore.SetCipher exists.
	SetCipher(c *secrets.Cipher, err error)
}
