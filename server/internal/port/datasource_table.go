package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TableQuerier is a kind that answers a per-entity table (one row per queue,
// partition or consumer) for the queues view. Secrets are passed as for
// MetricsQuerier.Series; an empty result is not an error.
type TableQuerier interface {
	Table(ctx context.Context, ds domain.DataSource, secrets map[string]string, q domain.TableQuery) (domain.TableResult, error)
}

// QueueDiscoverer is a broker kind that lists the consumers on its broker,
// for the detector to offer the ones naming a component as its queue
// bindings. The kind caps the list itself.
type QueueDiscoverer interface {
	DiscoverQueues(ctx context.Context, ds domain.DataSource, secrets map[string]string) ([]domain.QueueTarget, error)
}
