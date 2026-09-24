package projectmodel

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// b2Store is a stateful in-memory port.ProjectModelStore. The package's own
// scan/reconcile machinery runs against it unmodified, so these tests exercise
// the same store contract the real Postgres adapter promises.
type b2Store struct {
	mu         sync.Mutex
	components map[uuid.UUID]domain.Component
	checks     map[uuid.UUID]domain.ComponentCheck
	links      map[uuid.UUID]domain.ComponentLink
	resources  map[uuid.UUID]domain.SystemResource
	notes      map[uuid.UUID]domain.ProjectNote
	scans      map[uuid.UUID]domain.ProjectScan
}

func newB2Store() *b2Store {
	return &b2Store{
		components: map[uuid.UUID]domain.Component{},
		checks:     map[uuid.UUID]domain.ComponentCheck{},
		links:      map[uuid.UUID]domain.ComponentLink{},
		resources:  map[uuid.UUID]domain.SystemResource{},
		notes:      map[uuid.UUID]domain.ProjectNote{},
		scans:      map[uuid.UUID]domain.ProjectScan{},
	}
}

var _ port.ProjectModelStore = (*b2Store)(nil)

// --- components ---

func (s *b2Store) ListComponents(_ context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
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

func (s *b2Store) ListComponentsForRepositories(_ context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error) {
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

func (s *b2Store) ListAllComponents(_ context.Context) ([]domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Component, 0, len(s.components))
	for _, c := range s.components {
		out = append(out, c)
	}
	return out, nil
}

func (s *b2Store) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.components[id]
	if !ok {
		return domain.Component{}, fmt.Errorf("component %s: %w", id, port.ErrNotFound)
	}
	return c, nil
}

func (s *b2Store) SaveComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	s.components[c.ID] = c
	return c, nil
}

// --- checks ---

func (s *b2Store) ListChecks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentCheck, error) {
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

func (s *b2Store) GetCheck(_ context.Context, id uuid.UUID) (domain.ComponentCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checks[id]
	if !ok {
		return domain.ComponentCheck{}, fmt.Errorf("check %s: %w", id, port.ErrNotFound)
	}
	return c, nil
}

func (s *b2Store) SaveCheck(_ context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	s.checks[c.ID] = c
	return c, nil
}

// --- links ---

func (s *b2Store) ListLinks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
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

func (s *b2Store) ListIncomingLinks(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
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

func (s *b2Store) ListLinksForRepositories(_ context.Context, repositoryIDs []uuid.UUID) ([]domain.ComponentLink, error) {
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

func (s *b2Store) GetLink(_ context.Context, id uuid.UUID) (domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[id]
	if !ok {
		return domain.ComponentLink{}, fmt.Errorf("link %s: %w", id, port.ErrNotFound)
	}
	return l, nil
}

func (s *b2Store) SaveLink(_ context.Context, l domain.ComponentLink) (domain.ComponentLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	now := time.Now().UTC()
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	l.UpdatedAt = now
	s.links[l.ID] = l
	return l, nil
}

func (s *b2Store) DeleteLink(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.links[id]; !ok {
		return fmt.Errorf("link %s: %w", id, port.ErrNotFound)
	}
	delete(s.links, id)
	return nil
}

// --- resources ---

func (s *b2Store) GetResource(_ context.Context, id uuid.UUID) (domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resources[id]
	if !ok {
		return domain.SystemResource{}, fmt.Errorf("resource %s: %w", id, port.ErrNotFound)
	}
	return r, nil
}

func (s *b2Store) ListResources(_ context.Context, ids []uuid.UUID) ([]domain.SystemResource, error) {
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

func (s *b2Store) EnsureResource(_ context.Context, r domain.SystemResource) (domain.SystemResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.resources {
		if existing.IdentityKey == r.IdentityKey {
			existing.Kind, existing.Vendor, existing.Name, existing.Details = r.Kind, r.Vendor, r.Name, r.Details
			existing.UpdatedAt = time.Now().UTC()
			s.resources[existing.ID] = existing
			return existing, nil
		}
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	now := time.Now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now
	s.resources[r.ID] = r
	return r, nil
}

// --- notes ---

func (s *b2Store) ListNotes(_ context.Context, repositoryID uuid.UUID) ([]domain.ProjectNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.ProjectNote
	for _, n := range s.notes {
		if n.RepositoryID == repositoryID {
			out = append(out, n)
		}
	}
	return out, nil
}

func (s *b2Store) GetNote(_ context.Context, id uuid.UUID) (domain.ProjectNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	if !ok {
		return domain.ProjectNote{}, fmt.Errorf("note %s: %w", id, port.ErrNotFound)
	}
	return n, nil
}

func (s *b2Store) SaveNote(_ context.Context, n domain.ProjectNote) (domain.ProjectNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.notes {
		if existing.RepositoryID == n.RepositoryID && sameComponentScope(existing.ComponentID, n.ComponentID) && existing.Topic == n.Topic {
			n.ID = id
			n.CreatedAt = existing.CreatedAt
			n.UpdatedAt = time.Now().UTC()
			s.notes[id] = n
			return n, nil
		}
	}
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	now := time.Now().UTC()
	n.CreatedAt, n.UpdatedAt = now, now
	s.notes[n.ID] = n
	return n, nil
}

func sameComponentScope(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *b2Store) DeleteNote(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[id]; !ok {
		return fmt.Errorf("note %s: %w", id, port.ErrNotFound)
	}
	delete(s.notes, id)
	return nil
}

func (s *b2Store) MarkNotesStale(_ context.Context, repositoryID uuid.UUID, changedPaths []string) ([]domain.ProjectNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := map[string]bool{}
	for _, p := range changedPaths {
		changed[p] = true
	}
	var out []domain.ProjectNote
	for id, n := range s.notes {
		if n.RepositoryID != repositoryID || n.Stale {
			continue
		}
		touched := false
		for _, ev := range n.Evidence {
			if changed[ev.Path] {
				touched = true
				break
			}
		}
		if !touched {
			continue
		}
		n.Stale = true
		s.notes[id] = n
		out = append(out, n)
	}
	return out, nil
}

// --- scans ---

func (s *b2Store) CreateScan(_ context.Context, scan domain.ProjectScan) (domain.ProjectScan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scan.ID == uuid.Nil {
		scan.ID = uuid.New()
	}
	if scan.Events == nil {
		scan.Events = []domain.ScanEvent{}
	}
	s.scans[scan.ID] = scan
	return scan, nil
}

func (s *b2Store) UpdateScan(_ context.Context, scan domain.ProjectScan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scans[scan.ID]; !ok {
		return fmt.Errorf("scan %s: %w", scan.ID, port.ErrNotFound)
	}
	s.scans[scan.ID] = scan
	return nil
}

func (s *b2Store) GetScan(_ context.Context, id uuid.UUID) (domain.ProjectScan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scan, ok := s.scans[id]
	if !ok {
		return domain.ProjectScan{}, fmt.Errorf("scan %s: %w", id, port.ErrNotFound)
	}
	return scan, nil
}

func (s *b2Store) LatestScan(_ context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error) {
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
	latest.Result = nil
	return latest, nil
}

func (s *b2Store) FailInterruptedScans(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, scan := range s.scans {
		if scan.Status == domain.ScanQueued || scan.Status == domain.ScanRunning {
			scan.Status = domain.ScanFailed
			scan.Error = "interrupted"
			finishedAt := time.Now().UTC()
			scan.FinishedAt = &finishedAt
			s.scans[id] = scan
			n++
		}
	}
	return n, nil
}

// --- reconcile ---

func (s *b2Store) ApplyReconcile(_ context.Context, r port.ModelReconcile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range r.SaveComponents {
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		now := time.Now().UTC()
		if c.CreatedAt.IsZero() {
			c.CreatedAt = now
		}
		c.UpdatedAt = now
		s.components[c.ID] = c
	}
	for _, id := range r.DeleteComponents {
		delete(s.components, id)
		for checkID, c := range s.checks {
			if c.ComponentID == id {
				delete(s.checks, checkID)
			}
		}
		for linkID, l := range s.links {
			if l.FromComponentID == id {
				delete(s.links, linkID)
			}
		}
	}
	for _, c := range r.SaveChecks {
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		now := time.Now().UTC()
		if c.CreatedAt.IsZero() {
			c.CreatedAt = now
		}
		c.UpdatedAt = now
		s.checks[c.ID] = c
	}
	for _, id := range r.DeleteChecks {
		delete(s.checks, id)
	}
	for _, l := range r.SaveLinks {
		if l.ID == uuid.Nil {
			l.ID = uuid.New()
		}
		now := time.Now().UTC()
		if l.CreatedAt.IsZero() {
			l.CreatedAt = now
		}
		l.UpdatedAt = now
		s.links[l.ID] = l
	}
	for _, id := range r.DeleteLinks {
		delete(s.links, id)
	}
	return nil
}

func toIDSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// b2Repos is both the RepositoryReader and the LegacyProjector: both act on
// the same in-memory repository row, the way the real repository store and
// its legacy-projection methods act on the same table.
type b2Repos struct {
	mu    sync.Mutex
	repos map[uuid.UUID]domain.Repository
}

func newB2Repos() *b2Repos { return &b2Repos{repos: map[uuid.UUID]domain.Repository{}} }

func (r *b2Repos) put(repo domain.Repository) domain.Repository {
	r.mu.Lock()
	defer r.mu.Unlock()
	if repo.ID == uuid.Nil {
		repo.ID = uuid.New()
	}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = time.Now().UTC()
	}
	r.repos[repo.ID] = repo
	return repo
}

func (r *b2Repos) get(id uuid.UUID) domain.Repository {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.repos[id]
}

func (r *b2Repos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo, ok := r.repos[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("repository %s: %w", id, port.ErrNotFound)
	}
	return repo, nil
}

func (r *b2Repos) List(_ context.Context) ([]domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Repository, 0, len(r.repos))
	for _, repo := range r.repos {
		out = append(out, repo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *b2Repos) UpdateMeta(_ context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo := r.repos[id]
	if kind != nil {
		repo.Kind = *kind
	}
	if subRepoKinds != nil {
		repo.SubRepoKinds = *subRepoKinds
	}
	if autoReleaseOnDone != nil {
		repo.AutoReleaseOnDone = *autoReleaseOnDone
	}
	r.repos[id] = repo
	return repo, nil
}

func (r *b2Repos) UpdateSubProjects(_ context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo := r.repos[id]
	repo.SubProjects = subProjects
	r.repos[id] = repo
	return repo, nil
}

func (r *b2Repos) UpdateMobilePlatform(_ context.Context, id uuid.UUID, platform string) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo := r.repos[id]
	repo.MobilePlatform = platform
	r.repos[id] = repo
	return repo, nil
}

func (r *b2Repos) UpdateDetectedAppIdentity(_ context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo := r.repos[id]
	repo.DetectedAppIdentity = identity
	r.repos[id] = repo
	return repo, nil
}

func (r *b2Repos) UpdateDetectedBuildTargets(_ context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo := r.repos[id]
	repo.DetectedBuildTargets = targets
	r.repos[id] = repo
	return repo, nil
}

// b2Pipelines is the PipelineJobWriter fake; project() reads/writes it on
// every edit that touches components or checks.
type b2Pipelines struct {
	mu   sync.Mutex
	jobs map[uuid.UUID][]domain.RepositoryPipelineJob
}

func newB2Pipelines() *b2Pipelines {
	return &b2Pipelines{jobs: map[uuid.UUID][]domain.RepositoryPipelineJob{}}
}

func (p *b2Pipelines) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]domain.RepositoryPipelineJob(nil), p.jobs[repositoryID]...), nil
}

func (p *b2Pipelines) ReplaceForRepository(_ context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs[repositoryID] = append([]domain.RepositoryPipelineJob(nil), jobs...)
	return p.jobs[repositoryID], nil
}

func (p *b2Pipelines) seed(repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs[repositoryID] = jobs
}

// b2Legacy is the port.LegacyModelSource fake: the pre-scan rows a real
// repository would still be carrying at migration time.
type b2Legacy struct {
	mu       sync.Mutex
	deps     map[uuid.UUID][]domain.LegacyDependency
	sections map[uuid.UUID][]domain.LegacyAgentSection
}

func newB2Legacy() *b2Legacy {
	return &b2Legacy{deps: map[uuid.UUID][]domain.LegacyDependency{}, sections: map[uuid.UUID][]domain.LegacyAgentSection{}}
}

func (l *b2Legacy) seedDependency(repositoryID uuid.UUID, dep domain.LegacyDependency) domain.LegacyDependency {
	l.mu.Lock()
	defer l.mu.Unlock()
	if dep.ID == uuid.Nil {
		dep.ID = uuid.New()
	}
	dep.RepositoryID = repositoryID
	l.deps[repositoryID] = append(l.deps[repositoryID], dep)
	return dep
}

func (l *b2Legacy) seedSection(repositoryID uuid.UUID, sec domain.LegacyAgentSection) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sections[repositoryID] = append(l.sections[repositoryID], sec)
}

func (l *b2Legacy) ListLegacyDependencies(_ context.Context, repositoryID uuid.UUID) ([]domain.LegacyDependency, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.LegacyDependency(nil), l.deps[repositoryID]...), nil
}

func (l *b2Legacy) ListLegacyAgentSections(_ context.Context, repositoryID uuid.UUID) ([]domain.LegacyAgentSection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.LegacyAgentSection(nil), l.sections[repositoryID]...), nil
}

var _ port.LegacyModelSource = (*b2Legacy)(nil)

// b2Scanner is the Scanner fake: every test that needs a scan to run
// supplies a fixed ScanResult instead of touching a real working copy.
type b2Scanner struct {
	mu        sync.Mutex
	result    domain.ScanResult
	err       error
	calls     int
	roots     []string
	resultFor map[string]domain.ScanResult
}

func (s *b2Scanner) Scan(_ context.Context, root string, emit func(domain.ScanEvent)) (domain.ScanResult, error) {
	s.mu.Lock()
	s.calls++
	s.roots = append(s.roots, root)
	result, err := s.result, s.err
	if r, ok := s.resultFor[root]; ok {
		result = r
	}
	s.mu.Unlock()
	if emit != nil {
		emit(domain.ScanEvent{Stage: domain.ScanStageInventory, Done: true, At: time.Now().UTC()})
	}
	return result, err
}

func (s *b2Scanner) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *b2Scanner) callOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.roots...)
}

func (s *b2Scanner) setResultFor(root string, result domain.ScanResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resultFor == nil {
		s.resultFor = map[string]domain.ScanResult{}
	}
	s.resultFor[root] = result
}

// b2Loop is the AgentLoop fake.
type b2Loop struct {
	mu    sync.Mutex
	calls [][]domain.Message
	fn    func(ctx context.Context, messages []domain.Message) (domain.AgentResponse, error)
}

func (l *b2Loop) Run(ctx context.Context, messages []domain.Message, _ string, _ domain.LLMProviderType, _ domain.ToolPolicy, _ ...agent.RunOption) (domain.AgentResponse, error) {
	l.mu.Lock()
	l.calls = append(l.calls, messages)
	fn := l.fn
	l.mu.Unlock()
	if fn != nil {
		return fn(ctx, messages)
	}
	return domain.AgentResponse{}, nil
}

func (l *b2Loop) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

// b2Agents is the AgentGetter fake.
type b2Agents struct {
	agent domain.Agent
}

func (a *b2Agents) GetAgent(_ context.Context, id uuid.UUID) (domain.Agent, error) {
	if a.agent.ID == id {
		return a.agent, nil
	}
	return domain.Agent{}, fmt.Errorf("agent %s: %w", id, port.ErrNotFound)
}

// b2Roles is the minimal port.RoleResolver fake: only AgentForPurpose is
// exercised by the notes pass, the rest satisfy the interface.
type b2Roles struct {
	agentID *uuid.UUID
}

func (r *b2Roles) AgentForRole(_ context.Context, _ uuid.UUID, _ string) (*uuid.UUID, error) {
	return nil, nil
}

func (r *b2Roles) AgentForPurpose(_ context.Context, _ domain.RolePurposeKey, _ string) (*uuid.UUID, error) {
	return r.agentID, nil
}

func (r *b2Roles) AgentArea(_ context.Context, _ uuid.UUID) string { return "" }

func (r *b2Roles) AssigneeForNewTask(_ context.Context, _ domain.TaskType, _ string, requested *uuid.UUID) (*uuid.UUID, error) {
	return requested, nil
}

var _ port.RoleResolver = (*b2Roles)(nil)

// newB2Service wires a Service against every b2 fake, with the projector and
// pipeline writer project() needs on every component/check edit; tests that
// need the agent notes pass call SetAgentLoop themselves.
func newB2Service(t interface{ Helper() }) (*Service, *b2Store, *b2Repos, *b2Pipelines, *b2Legacy, *b2Scanner) {
	t.Helper()
	store := newB2Store()
	repos := newB2Repos()
	pipelines := newB2Pipelines()
	legacy := newB2Legacy()
	scanner := &b2Scanner{}

	svc := NewService(Deps{
		Store:     store,
		Repos:     repos,
		Projector: repos,
		Pipelines: pipelines,
		Scanner:   scanner,
		Legacy:    legacy,
	})
	return svc, store, repos, pipelines, legacy, scanner
}
