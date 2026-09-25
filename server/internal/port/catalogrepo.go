package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CatalogRepoReader turns the external agent catalog (a git clone or a local
// directory) into domain definitions.
type CatalogRepoReader interface {
	ReadCatalog(ctx context.Context) ([]domain.UpstreamAgent, string, error)
}

// CatalogSyncStore is the last-sync state and the pending-changes ledger a
// catalog sync reads and writes.
type CatalogSyncStore interface {
	GetCatalogSyncState(ctx context.Context) (domain.CatalogSyncState, error)
	SaveCatalogSyncState(ctx context.Context, state domain.CatalogSyncState) error
	AppendCatalogPending(ctx context.Context, pending domain.CatalogPending) error
	ListCatalogPending(ctx context.Context) ([]domain.CatalogPending, error)
	DeleteCatalogPending(ctx context.Context, id uuid.UUID) error
}
