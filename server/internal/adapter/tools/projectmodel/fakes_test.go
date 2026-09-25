package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeStore is a stateful in-memory port.ProjectModelStore, trimmed down from
// the projectmodel package's own b2Store test fake: enough of the same
// contract for the tools to exercise a real Service instead of mocking it.
type fakeStore struct {
	mu              sync.Mutex
	components      map[uuid.UUID]domain.Component
	checks          map[uuid.UUID]domain.ComponentCheck
	links           map[uuid.UUID]domain.ComponentLink
	resources       map[uuid.UUID]domain.SystemResource
	resourceAliases map[string]uuid.UUID
	scans           map[uuid.UUID]domain.ProjectScan
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		resourceAliases: map[string]uuid.UUID{},
		components:      map[uuid.UUID]domain.Component{},
		checks:          map[uuid.UUID]domain.ComponentCheck{},
		links:           map[uuid.UUID]domain.ComponentLink{},
		resources:       map[uuid.UUID]domain.SystemResource{},
		scans:           map[uuid.UUID]domain.ProjectScan{},
	}
}

var _ port.ProjectModelStore = (*fakeStore)(nil)

func (s *fakeStore) ListComponents(_ context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Component
	for _, c := range s.components {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (s *fakeStore) ListComponentsForRepositories(_ context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := toIDSet(repositoryIDs)
	var out []domain.Component
	for _, c := range s.components {
		if want[c.RepositoryID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *fakeStore) ListAllComponents(_ context.Context) ([]domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Component, 0, len(s.components))
	for _, c := range s.components {
		out = append(out, c)
	}
	return out, nil
}

func (s *fakeStore) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.components[id]
	if !ok {
		return domain.Component{}, fmt.Errorf("component %s: %w", id, port.ErrNotFound)
	}
	return c, nil
}

func (s *fakeStore) SaveComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	s.components[c.ID] = c
	return c, nil
}

func (s *fakeStore) ListChecks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ComponentCheck
	for _, c := range s.checks {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JobKey < out[j].JobKey })
	return out, nil
}

func (s *fakeStore) GetCheck(_ context.Context, id uuid.UUID) (domain.ComponentCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checks[id]
	if !ok {
		return domain.ComponentCheck{}, fmt.Errorf("check %s: %w", id, port.ErrNotFound)
	}
	return c, nil
}

func (s *fakeStore) SaveCheck(_ context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	s.checks[c.ID] = c
	return c, nil
}

func (s *fakeStore) ListLinks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ComponentLink
	for _, l := range s.links {
		if l.RepositoryID == repositoryID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *fakeStore) ListIncomingLinks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ComponentLink
	for _, l := range s.links {
		if l.ToComponentID == nil {
			continue
		}
		target, ok := s.components[*l.ToComponentID]
		if !ok || target.RepositoryID != repositoryID {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func (s *fakeStore) ListLinksForRepositories(_ context.Context, repositoryIDs []uuid.UUID) ([]domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := toIDSet(repositoryIDs)
	var out []domain.ComponentLink
	for _, l := range s.links {
		if want[l.RepositoryID] {
			out = append(out, l)
		}
	}
	return out, nil
}

func (s *fakeStore) GetLink(_ context.Context, id uuid.UUID) (domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[id]
	if !ok {
		return domain.ComponentLink{}, fmt.Errorf("link %s: %w", id, port.ErrNotFound)
	}
	return l, nil
}

func (s *fakeStore) SaveLink(_ context.Context, l domain.ComponentLink) (domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	s.links[l.ID] = l
	return l, nil
}

func (s *fakeStore) DeleteLink(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, id)
	return nil
}

func (s *fakeStore) GetResource(_ context.Context, id uuid.UUID) (domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resources[id]
	if !ok {
		return domain.SystemResource{}, fmt.Errorf("resource %s: %w", id, port.ErrNotFound)
	}
	return r, nil
}

func (s *fakeStore) ListResources(_ context.Context, ids []uuid.UUID) ([]domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := toIDSet(ids)
	var out []domain.SystemResource
	for _, r := range s.resources {
		if want[r.ID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fakeStore) EnsureResource(_ context.Context, r domain.SystemResource) (domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if targetID, ok := s.resourceAliases[r.IdentityKey]; ok {
		return s.resources[targetID], nil
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	s.resources[r.ID] = r
	return r, nil
}

func (s *fakeStore) RenameResource(_ context.Context, id uuid.UUID, name string) (domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resources[id]
	if !ok {
		return domain.SystemResource{}, fmt.Errorf("resource %s: %w", id, port.ErrNotFound)
	}
	r.Name = name
	r.NameLocked = true
	s.resources[id] = r
	return r, nil
}

func (s *fakeStore) MergeResources(_ context.Context, sourceID, targetID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.resources[sourceID]
	if !ok {
		return fmt.Errorf("resource %s: %w", sourceID, port.ErrNotFound)
	}
	if _, ok := s.resources[targetID]; !ok {
		return fmt.Errorf("resource %s: %w", targetID, port.ErrNotFound)
	}
	for id, l := range s.links {
		if l.ToResourceID != nil && *l.ToResourceID == sourceID {
			target := targetID
			l.ToResourceID = &target
			s.links[id] = l
		}
	}
	if s.resourceAliases == nil {
		s.resourceAliases = map[string]uuid.UUID{}
	}
	for key, rid := range s.resourceAliases {
		if rid == sourceID {
			s.resourceAliases[key] = targetID
		}
	}
	s.resourceAliases[source.IdentityKey] = targetID
	delete(s.resources, sourceID)
	return nil
}

func (s *fakeStore) CreateScan(_ context.Context, scan domain.ProjectScan) (domain.ProjectScan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scan.ID == uuid.Nil {
		scan.ID = uuid.New()
	}
	s.scans[scan.ID] = scan
	return scan, nil
}

func (s *fakeStore) UpdateScan(_ context.Context, scan domain.ProjectScan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scans[scan.ID] = scan
	return nil
}

func (s *fakeStore) GetScan(_ context.Context, id uuid.UUID) (domain.ProjectScan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scan, ok := s.scans[id]
	if !ok {
		return domain.ProjectScan{}, fmt.Errorf("scan %s: %w", id, port.ErrNotFound)
	}
	return scan, nil
}

func (s *fakeStore) LatestScan(_ context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest domain.ProjectScan
	found := false
	for _, scan := range s.scans {
		if scan.RepositoryID != repositoryID {
			continue
		}
		if !found || scan.StartedAt.After(latest.StartedAt) {
			latest, found = scan, true
		}
	}
	if !found {
		return domain.ProjectScan{}, fmt.Errorf("repository %s has no scans: %w", repositoryID, port.ErrNotFound)
	}
	return latest, nil
}

func (s *fakeStore) FailInterruptedScans(_ context.Context) (int, error) { return 0, nil }

func (s *fakeStore) ApplyReconcile(_ context.Context, _ port.ModelReconcile) error { return nil }

func toIDSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// fakeRepos is the RepositoryReader fake.
type fakeRepos struct {
	mu    sync.Mutex
	repos map[uuid.UUID]domain.Repository
}

func newFakeRepos() *fakeRepos { return &fakeRepos{repos: map[uuid.UUID]domain.Repository{}} }

func (r *fakeRepos) put(repo domain.Repository) domain.Repository {
	r.mu.Lock()
	defer r.mu.Unlock()
	if repo.ID == uuid.Nil {
		repo.ID = uuid.New()
	}
	r.repos[repo.ID] = repo
	return repo
}

func (r *fakeRepos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo, ok := r.repos[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("repository %s: %w", id, port.ErrNotFound)
	}
	return repo, nil
}

func (r *fakeRepos) List(_ context.Context) ([]domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Repository, 0, len(r.repos))
	for _, repo := range r.repos {
		out = append(out, repo)
	}
	return out, nil
}
