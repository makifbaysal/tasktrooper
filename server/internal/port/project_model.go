package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ComponentStore interface {
	ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)
	// Feeds the project map and the cross-repository matcher in one query.
	ListComponentsForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error)
	ListAllComponents(ctx context.Context) ([]domain.Component, error)
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
	SaveComponent(ctx context.Context, c domain.Component) (domain.Component, error)
}

type CheckStore interface {
	ListChecks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentCheck, error)
	GetCheck(ctx context.Context, id uuid.UUID) (domain.ComponentCheck, error)
	SaveCheck(ctx context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error)
}

type LinkStore interface {
	// Outgoing edges of the repository's components.
	ListLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error)
	// Edges from other repositories into this repository's components.
	ListIncomingLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error)
	ListLinksForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.ComponentLink, error)
	GetLink(ctx context.Context, id uuid.UUID) (domain.ComponentLink, error)
	SaveLink(ctx context.Context, l domain.ComponentLink) (domain.ComponentLink, error)
	DeleteLink(ctx context.Context, id uuid.UUID) error
}

type ResourceStore interface {
	GetResource(ctx context.Context, id uuid.UUID) (domain.SystemResource, error)
	ListResources(ctx context.Context, ids []uuid.UUID) ([]domain.SystemResource, error)
	// Upserts by IdentityKey and returns the stored row (with its ID). An
	// identity key that resolves through system_resource_aliases returns that
	// alias's resource unchanged instead of upserting, so a rescan can never
	// resurrect a resource a human merged away.
	EnsureResource(ctx context.Context, r domain.SystemResource) (domain.SystemResource, error)
	// MergeResources moves every link and alias of source onto target, records
	// source's identity key as an alias of target, and deletes source, all in
	// one transaction. Returns ErrNotFound if either id does not exist.
	MergeResources(ctx context.Context, sourceID, targetID uuid.UUID) error
	// RenameResource sets name and locks it against EnsureResource overwriting
	// it on a later rescan.
	RenameResource(ctx context.Context, id uuid.UUID, name string) (domain.SystemResource, error)
}

type NoteStore interface {
	ListNotes(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProjectNote, error)
	GetNote(ctx context.Context, id uuid.UUID) (domain.ProjectNote, error)
	// Upserts by (repository, component, topic).
	SaveNote(ctx context.Context, n domain.ProjectNote) (domain.ProjectNote, error)
	DeleteNote(ctx context.Context, id uuid.UUID) error
	// Flags notes whose evidence paths appear in changedPaths and returns them.
	MarkNotesStale(ctx context.Context, repositoryID uuid.UUID, changedPaths []string) ([]domain.ProjectNote, error)
}

type ScanStore interface {
	CreateScan(ctx context.Context, s domain.ProjectScan) (domain.ProjectScan, error)
	// Rewrites status, stage, events, result, review count, error and
	// finished_at.
	UpdateScan(ctx context.Context, s domain.ProjectScan) error
	GetScan(ctx context.Context, id uuid.UUID) (domain.ProjectScan, error)
	// The newest scan of the repository, without its Result.
	LatestScan(ctx context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error)
	// Marks scans left queued/running by a previous process as failed.
	FailInterruptedScans(ctx context.Context) (int, error)
}

// ModelReconcile is one scan's worth of writes, applied atomically so a
// half-applied scan never leaves checks pointing at a deleted component.
// New rows carry IDs minted by the caller so links can reference components
// created in the same batch.
type ModelReconcile struct {
	RepositoryID     uuid.UUID
	SaveComponents   []domain.Component
	DeleteComponents []uuid.UUID
	SaveChecks       []domain.ComponentCheck
	DeleteChecks     []uuid.UUID
	SaveLinks        []domain.ComponentLink
	DeleteLinks      []uuid.UUID
}

type ModelReconciler interface {
	ApplyReconcile(ctx context.Context, r ModelReconcile) error
}

// ProjectModelStore is the whole persistence surface of the project model;
// consumers depend on the narrow interfaces above.
type ProjectModelStore interface {
	ComponentStore
	CheckStore
	LinkStore
	ResourceStore
	NoteStore
	ScanStore
	ModelReconciler
}

// LegacyModelSource reads the tables the project model replaced, once per
// repository, so the human's earlier settings carry over as overrides.
type LegacyModelSource interface {
	ListLegacyDependencies(ctx context.Context, repositoryID uuid.UUID) ([]domain.LegacyDependency, error)
	ListLegacyAgentSections(ctx context.Context, repositoryID uuid.UUID) ([]domain.LegacyAgentSection, error)
}
