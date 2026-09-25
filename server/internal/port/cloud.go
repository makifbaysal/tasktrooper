package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrUnsupported is returned by a CloudProvider for an operation its API
// cannot answer (e.g. native error grouping); the caller degrades.
var ErrUnsupported = errors.New("not supported by this provider")

// ErrCloudAuth marks a provider refusing the stored credential, so the UI can
// say "reconnect" rather than "something failed".
var ErrCloudAuth = errors.New("the provider rejected the credential")

// ErrCloudWriteDenied is a provider refusing a WRITE the credential can read
// but not perform (a read-only token, a service account without the update
// permission).
var ErrCloudWriteDenied = errors.New("the credential may read but not change this resource")

// ErrVercelUnauthorized is Vercel refusing the stored token ITSELF (a 401 or
// 403), so the vercel adapter can tell a revoked/expired token apart from
// "Vercel is down" without the caller inspecting a status code. The adapter
// satisfies this by implementing errors.Is on its API error type.
var ErrVercelUnauthorized = errors.New("vercel: the stored token was refused")

type CloudAccountStore interface {
	ListCloudAccounts(ctx context.Context) ([]domain.CloudAccount, error)
	GetCloudAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error)
	// CreateCloudAccount encrypts fields; they never come back out except via
	// CloudCredential.
	CreateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error)
	// UpdateCloudAccount rewrites label/meta/status; non-nil fields replace
	// the stored secret.
	UpdateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error)
	DeleteCloudAccount(ctx context.Context, id uuid.UUID) error
	CloudCredential(ctx context.Context, id uuid.UUID) (domain.CloudCredential, error)
}

type EnvironmentStore interface {
	ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error)
	ListAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error)
	GetEnvironment(ctx context.Context, id uuid.UUID) (domain.ComponentEnvironment, error)
	// Upserts by (component, environment).
	SaveEnvironment(ctx context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error)
	DeleteEnvironment(ctx context.Context, id uuid.UUID) error
}

// CloudRollbacker is the optional write capability of a provider that can
// put an earlier deployment back into production without a rebuild. A
// provider without it (or a credential without write access, which answers
// ErrCloudWriteDenied) leaves a release's rollback to the pushed revert.
type CloudRollbacker interface {
	// RollbackTo makes deploymentID the one serving production.
	RollbackTo(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error
	// Promote puts deploymentID into production; on Vercel it is also what
	// re-enables automatic production assignment after a RollbackTo.
	Promote(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, deploymentID string) error
	// Current is the deployment serving production right now.
	Current(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudDeployment, error)
}

// CloudProvider is one provider adapter. Every call gets the decrypted
// credential; adapters keep no account state of their own.
type CloudProvider interface {
	Kind() domain.CloudProviderKind
	// Verify checks the credential and returns the identity to store as Meta.
	Verify(ctx context.Context, cred domain.CloudCredential) (map[string]string, error)
	ListResources(ctx context.Context, cred domain.CloudCredential) ([]domain.CloudResource, error)
	Resource(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error)
	Deployments(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error)
	Logs(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error)
	// Errors returns ErrUnsupported when the provider has no native grouping.
	Errors(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, since time.Time) ([]domain.RuntimeErrorGroup, error)
}
