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

// SkillVectorSource is the skill vectors a catalog shipped with, embedded
// once ahead of time; nil, nil when it shipped none.
type SkillVectorSource interface {
	SkillVectors(ctx context.Context) (*domain.SkillVectorSet, error)
}

// EmbeddingSourceResolver answers which model and which engine this
// install's embedding calls resolve to now. source is one of the
// domain.EmbeddingSource* names, or "" when the engine is not known: then no
// precomputed vector may stand in for a live one.
type EmbeddingSourceResolver interface {
	ResolvedEmbeddingSource(ctx context.Context) (model, source string, err error)
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
