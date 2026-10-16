package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrDataSourceQuery marks a query the source refused as malformed (PromQL
// that does not parse), as opposed to the source failing.
var ErrDataSourceQuery = errors.New("the data source rejected the query")

// ErrStaleDataSource is RecordVerification finding the source written since
// the snapshot the verify ran against.
var ErrStaleDataSource = errors.New("the data source changed since it was read")

// DataSourceKindAdapter is one connection type of the data source registry.
// What else a kind can do is the optional interfaces below, detected by type
// assertion; Capabilities is what the UI shows, not what the service trusts.
// Every call gets the source's non-secret config and its decrypted secrets;
// adapters keep no source state of their own, and no secret value may appear
// in a returned error.
type DataSourceKindAdapter interface {
	Kind() domain.DataSourceKind
	Schema() domain.DataSourceSchema
	Capabilities() []domain.Capability
	Verify(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSourceInfo, error)
}

type MetricsQuerier interface {
	Series(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error)
}

// Discoverer lists what a source holds. The adapter caps the list itself and
// says so through Inventory.Truncated.
type Discoverer interface {
	Discover(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.Inventory, error)
}

// MetricDiscoverer answers whether a source holds one metric, for a kind
// whose Discover stops short on a large account: detection asks it about a
// recipe's detect metric the truncated inventory lacks.
type MetricDiscoverer interface {
	DiscoverMetric(ctx context.Context, ds domain.DataSource, secrets map[string]string, metric string) (bool, error)
}

type DataSourceStore interface {
	ListDataSources(ctx context.Context) ([]domain.DataSource, error)
	GetDataSource(ctx context.Context, id uuid.UUID) (domain.DataSource, error)
	// CreateDataSource encrypts secrets; they never come back out except via
	// DataSourceSecrets. SecretsSet is derived from the keys of secrets.
	CreateDataSource(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSource, error)
	// UpdateDataSource rewrites everything but the id and created_at, and
	// always moves updated_at forward; a non-nil secrets replaces the whole
	// stored set, nil keeps it.
	UpdateDataSource(ctx context.Context, ds domain.DataSource, secrets map[string]string) (domain.DataSource, error)
	// RecordVerification writes only the verify outcome, and only while
	// updated_at still equals expectedUpdatedAt: a verify that ran against a
	// config a newer write replaced answers ErrStaleDataSource and changes
	// nothing. It leaves updated_at alone, so two verifies of one config do
	// not invalidate each other.
	RecordVerification(ctx context.Context, id uuid.UUID, v domain.DataSourceVerification, expectedUpdatedAt time.Time) (domain.DataSource, error)
	DeleteDataSource(ctx context.Context, id uuid.UUID) error
	DataSourceSecrets(ctx context.Context, id uuid.UUID) (map[string]string, error)
}

// CloudAccountReader is how data sources reach a connected cloud account: a
// source of a kind that reads one (gcp_monitoring, aws_cloudwatch) stores only
// the account id, and the kind reads the credential when it calls. A missing
// account is ErrNotFound.
type CloudAccountReader interface {
	GetCloudAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error)
	CloudCredential(ctx context.Context, id uuid.UUID) (domain.CloudCredential, error)
}

// ObservabilityRecipeStore holds the recipes a person added beside the
// embedded catalog.
type ObservabilityRecipeStore interface {
	ListUserRecipes(ctx context.Context) ([]domain.Recipe, error)
}
