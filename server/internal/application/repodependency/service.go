// Package repodependency answers "who depends on whom": a repository can
// point at a sub-project of another monorepo, at another repository
// altogether, or at a manually-recorded database. It feeds the project
// overview's architecture view; discovering these edges automatically (code
// scanning, network-call analysis) is out of scope.
package repodependency

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ErrInvalidInput wraps every caller mistake this service rejects before
// touching the store.
var ErrInvalidInput = errors.New("invalid input")

// ErrNotSameProject means a sub_repo dependency named a target repository
// that shares no project with the source — "a sub-project of a monorepo in
// the same project" is the only sub_repo relationship this feature records.
var ErrNotSameProject = errors.New("target repository is not in the same project")

// ErrSubProjectNotFound means the target repository has no sub-project at
// the given path.
var ErrSubProjectNotFound = errors.New("target repository has no sub-project at that path")

// RepositoryResolver reads the repositories a dependency request names.
type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// Service owns every repository's dependency edges.
type Service struct {
	store port.RepoDependencyStore
	repos RepositoryResolver
}

func NewService(store port.RepoDependencyStore, repos RepositoryResolver) *Service {
	return &Service{store: store, repos: repos}
}

// List returns every dependency recorded FROM repositoryID.
func (s *Service) List(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepoDependency, error) {
	deps, err := s.store.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if deps == nil {
		deps = []domain.RepoDependency{}
	}
	return deps, nil
}

// ListForProject splits every dependency touching the project's repositories
// into outgoing (source repo is in the project) and incoming (target repo
// is in the project, source is not).
func (s *Service) ListForProject(ctx context.Context, projectID uuid.UUID) ([]domain.RepoDependency, []domain.RepoDependency, error) {
	outgoing, incoming, err := s.store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	if outgoing == nil {
		outgoing = []domain.RepoDependency{}
	}
	if incoming == nil {
		incoming = []domain.RepoDependency{}
	}
	return outgoing, incoming, nil
}

// Create validates req against repositoryID and, for a sub_repo or repo
// target, against the target repository actually on record, then persists
// the edge.
func (s *Service) Create(ctx context.Context, repositoryID uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	source, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.RepoDependency{}, err
	}
	if err := s.validate(ctx, source, req); err != nil {
		return domain.RepoDependency{}, err
	}
	return s.store.Create(ctx, repositoryID, req)
}

// Update re-validates req against the dependency's own source repository —
// the source never changes on an update — and persists the change.
func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.SaveRepoDependencyRequest) (domain.RepoDependency, error) {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.RepoDependency{}, err
	}
	source, err := s.repos.Get(ctx, existing.RepositoryID)
	if err != nil {
		return domain.RepoDependency{}, err
	}
	if err := s.validate(ctx, source, req); err != nil {
		return domain.RepoDependency{}, err
	}
	return s.store.Update(ctx, id, req)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.Delete(ctx, id)
}

func (s *Service) validate(ctx context.Context, source domain.Repository, req domain.SaveRepoDependencyRequest) error {
	if err := domain.ValidateRepoDependencyRequest(req, source.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	switch req.TargetKind {
	case domain.DependencyTargetRepo:
		if _, err := s.repos.Get(ctx, *req.TargetRepositoryID); err != nil {
			return err
		}
	case domain.DependencyTargetSubRepo:
		target, err := s.repos.Get(ctx, *req.TargetRepositoryID)
		if err != nil {
			return err
		}
		if !sharesAProject(source.ProjectIDs, target.ProjectIDs) {
			return ErrNotSameProject
		}
		if !hasSubProject(target.SubProjects, req.TargetSubProjectPath) {
			return ErrSubProjectNotFound
		}
	}
	return nil
}

func sharesAProject(a, b []uuid.UUID) bool {
	set := make(map[uuid.UUID]bool, len(a))
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if set[id] {
			return true
		}
	}
	return false
}

func hasSubProject(subProjects []domain.RepoSubProject, path string) bool {
	for _, sp := range subProjects {
		if sp.Path == path {
			return true
		}
	}
	return false
}
