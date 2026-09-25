package projectmodel

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func ptr[T any](v T) *T { return &v }

func toUUIDSet(ids []uuid.UUID) map[uuid.UUID]bool {
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// --- fakeStore: port.ProjectModelStore ---

type fakeStore struct {
	mu                  sync.Mutex
	components          map[uuid.UUID]domain.Component
	checks              map[uuid.UUID]domain.ComponentCheck
	links               map[uuid.UUID]domain.ComponentLink
	resources           map[uuid.UUID]domain.SystemResource
	resourcesByIdentity map[string]uuid.UUID
	// resourceAliases mirrors system_resource_aliases: an identity key a merge
	// folded away keeps resolving to its merge target.
	resourceAliases map[string]uuid.UUID
	scans           map[uuid.UUID]domain.ProjectScan
}

var _ port.ProjectModelStore = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		components:          map[uuid.UUID]domain.Component{},
		checks:              map[uuid.UUID]domain.ComponentCheck{},
		links:               map[uuid.UUID]domain.ComponentLink{},
		resources:           map[uuid.UUID]domain.SystemResource{},
		resourcesByIdentity: map[string]uuid.UUID{},
		resourceAliases:     map[string]uuid.UUID{},
		scans:               map[uuid.UUID]domain.ProjectScan{},
	}
}

func (f *fakeStore) seedComponent(c domain.Component) domain.Component {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.components[c.ID] = c
	return c
}

func (f *fakeStore) seedCheck(c domain.ComponentCheck) domain.ComponentCheck {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.checks[c.ID] = c
	return c
}

func (f *fakeStore) seedLink(l domain.ComponentLink) domain.ComponentLink {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	f.links[l.ID] = l
	return l
}

func (f *fakeStore) seedResource(r domain.SystemResource) domain.SystemResource {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	f.resources[r.ID] = r
	f.resourcesByIdentity[r.IdentityKey] = r.ID
	return r
}

func (f *fakeStore) ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Component
	for _, c := range f.components {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (f *fakeStore) ListComponentsForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error) {
	want := toUUIDSet(repositoryIDs)
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Component
	for _, c := range f.components {
		if want[c.RepositoryID] {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) ListAllComponents(ctx context.Context) ([]domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Component
	for _, c := range f.components {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.components[id]
	if !ok {
		return domain.Component{}, port.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) SaveComponent(ctx context.Context, c domain.Component) (domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.components[c.ID] = c
	return c, nil
}

func (f *fakeStore) ListChecks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ComponentCheck
	for _, c := range f.checks {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) GetCheck(ctx context.Context, id uuid.UUID) (domain.ComponentCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.checks[id]
	if !ok {
		return domain.ComponentCheck{}, port.ErrNotFound
	}
	return c, nil
}

func (f *fakeStore) SaveCheck(ctx context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.checks[c.ID] = c
	return c, nil
}

func (f *fakeStore) ListLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ComponentLink
	for _, l := range f.links {
		if l.RepositoryID == repositoryID {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) ListIncomingLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	componentRepo := map[uuid.UUID]uuid.UUID{}
	for _, c := range f.components {
		componentRepo[c.ID] = c.RepositoryID
	}
	var out []domain.ComponentLink
	for _, l := range f.links {
		if l.ToComponentID != nil && componentRepo[*l.ToComponentID] == repositoryID {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) ListLinksForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.ComponentLink, error) {
	want := toUUIDSet(repositoryIDs)
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ComponentLink
	for _, l := range f.links {
		if want[l.RepositoryID] {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeStore) GetLink(ctx context.Context, id uuid.UUID) (domain.ComponentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.links[id]
	if !ok {
		return domain.ComponentLink{}, port.ErrNotFound
	}
	return l, nil
}

func (f *fakeStore) SaveLink(ctx context.Context, l domain.ComponentLink) (domain.ComponentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	f.links[l.ID] = l
	return l, nil
}

func (f *fakeStore) DeleteLink(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.links, id)
	return nil
}

func (f *fakeStore) GetResource(ctx context.Context, id uuid.UUID) (domain.SystemResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.resources[id]
	if !ok {
		return domain.SystemResource{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) ListResources(ctx context.Context, ids []uuid.UUID) ([]domain.SystemResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.SystemResource, 0, len(ids))
	for _, id := range ids {
		if r, ok := f.resources[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) EnsureResource(ctx context.Context, r domain.SystemResource) (domain.SystemResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if targetID, ok := f.resourceAliases[r.IdentityKey]; ok {
		return f.resources[targetID], nil
	}
	if id, ok := f.resourcesByIdentity[r.IdentityKey]; ok {
		existing := f.resources[id]
		existing.Kind, existing.Vendor, existing.Details = r.Kind, r.Vendor, r.Details
		if !existing.NameLocked {
			existing.Name = r.Name
		}
		f.resources[id] = existing
		return existing, nil
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	f.resources[r.ID] = r
	f.resourcesByIdentity[r.IdentityKey] = r.ID
	return r, nil
}

func (f *fakeStore) RenameResource(ctx context.Context, id uuid.UUID, name string) (domain.SystemResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.resources[id]
	if !ok {
		return domain.SystemResource{}, port.ErrNotFound
	}
	r.Name = name
	r.NameLocked = true
	f.resources[id] = r
	return r, nil
}

func (f *fakeStore) MergeResources(ctx context.Context, sourceID, targetID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	source, ok := f.resources[sourceID]
	if !ok {
		return port.ErrNotFound
	}
	if _, ok := f.resources[targetID]; !ok {
		return port.ErrNotFound
	}
	for id, l := range f.links {
		if l.ToResourceID != nil && *l.ToResourceID == sourceID {
			target := targetID
			l.ToResourceID = &target
			f.links[id] = l
		}
	}
	for key, rid := range f.resourceAliases {
		if rid == sourceID {
			f.resourceAliases[key] = targetID
		}
	}
	f.resourceAliases[source.IdentityKey] = targetID
	delete(f.resources, sourceID)
	delete(f.resourcesByIdentity, source.IdentityKey)
	return nil
}

func (f *fakeStore) CreateScan(ctx context.Context, sc domain.ProjectScan) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sc.ID == uuid.Nil {
		sc.ID = uuid.New()
	}
	if sc.Events == nil {
		sc.Events = []domain.ScanEvent{}
	}
	f.scans[sc.ID] = sc
	return sc, nil
}

func (f *fakeStore) UpdateScan(ctx context.Context, sc domain.ProjectScan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.scans[sc.ID]; !ok {
		return port.ErrNotFound
	}
	f.scans[sc.ID] = sc
	return nil
}

func (f *fakeStore) GetScan(ctx context.Context, id uuid.UUID) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc, ok := f.scans[id]
	if !ok {
		return domain.ProjectScan{}, port.ErrNotFound
	}
	return sc, nil
}

func (f *fakeStore) LatestScan(ctx context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest domain.ProjectScan
	found := false
	for _, sc := range f.scans {
		if sc.RepositoryID != repositoryID {
			continue
		}
		if !found || sc.StartedAt.After(latest.StartedAt) {
			latest, found = sc, true
		}
	}
	if !found {
		return domain.ProjectScan{}, port.ErrNotFound
	}
	latest.Result = nil
	return latest, nil
}

func (f *fakeStore) FailInterruptedScans(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for id, sc := range f.scans {
		if sc.Status == domain.ScanQueued || sc.Status == domain.ScanRunning {
			sc.Status = domain.ScanFailed
			f.scans[id] = sc
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) ApplyReconcile(ctx context.Context, r port.ModelReconcile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range r.SaveComponents {
		f.components[c.ID] = c
	}
	for _, id := range r.DeleteComponents {
		delete(f.components, id)
	}
	for _, c := range r.SaveChecks {
		f.checks[c.ID] = c
	}
	for _, id := range r.DeleteChecks {
		delete(f.checks, id)
	}
	for _, l := range r.SaveLinks {
		f.links[l.ID] = l
	}
	for _, id := range r.DeleteLinks {
		delete(f.links, id)
	}
	return nil
}

// --- fakeRepos: RepositoryReader (+ LegacyProjector target) ---

type fakeRepos struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.Repository
}

var _ RepositoryReader = (*fakeRepos)(nil)

func newFakeRepos(repos ...domain.Repository) *fakeRepos {
	m := map[uuid.UUID]domain.Repository{}
	for _, r := range repos {
		m[r.ID] = r
	}
	return &fakeRepos{byID: m}
}

func (f *fakeRepos) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return domain.Repository{}, port.ErrNotFound
	}
	return r, nil
}

func (f *fakeRepos) List(ctx context.Context) ([]domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Repository, 0, len(f.byID))
	for _, r := range f.byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeRepos) set(r domain.Repository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[r.ID] = r
}

// --- fakeProjector: LegacyProjector ---

// fakeProjector counts calls per method so tests can assert project() writes
// only when something actually changed.
type fakeProjector struct {
	repos *fakeRepos

	mu                        sync.Mutex
	updateMetaCalls           int
	updateSubProjectsCalls    int
	updateMobilePlatformCalls int
	updateAppIdentityCalls    int
	updateBuildTargetsCalls   int
	updateQualityGatesCalls   int
	lastQualityGatesCoverage  domain.QualityGate
	lastQualityGatesMutation  domain.QualityGate
}

var _ LegacyProjector = (*fakeProjector)(nil)

func (f *fakeProjector) UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string) (domain.Repository, error) {
	f.mu.Lock()
	f.updateMetaCalls++
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	if kind != nil {
		r.Kind = *kind
	}
	if subRepoKinds != nil {
		r.SubRepoKinds = *subRepoKinds
	}
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) UpdateQualityGates(ctx context.Context, id uuid.UUID, coverage, mutation domain.QualityGate) (domain.Repository, error) {
	f.mu.Lock()
	f.updateQualityGatesCalls++
	f.lastQualityGatesCoverage = coverage
	f.lastQualityGatesMutation = mutation
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.RequireOverallCoverage = coverage.Enabled
	r.CoverageThreshold = coverage.Threshold
	r.MutationEnabled = mutation.Enabled
	r.MutationThreshold = mutation.Threshold
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) qualityGatesCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updateQualityGatesCalls
}

func (f *fakeProjector) UpdateSubProjects(ctx context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	f.mu.Lock()
	f.updateSubProjectsCalls++
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.SubProjects = subProjects
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) UpdateMobilePlatform(ctx context.Context, id uuid.UUID, platform string) (domain.Repository, error) {
	f.mu.Lock()
	f.updateMobilePlatformCalls++
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.MobilePlatform = platform
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) UpdateDetectedAppIdentity(ctx context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	f.mu.Lock()
	f.updateAppIdentityCalls++
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.DetectedAppIdentity = identity
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) UpdateDetectedBuildTargets(ctx context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	f.mu.Lock()
	f.updateBuildTargetsCalls++
	f.mu.Unlock()
	r, err := f.repos.Get(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.DetectedBuildTargets = targets
	f.repos.set(r)
	return r, nil
}

func (f *fakeProjector) callCounts() (meta, subProjects, mobilePlatform, appIdentity, buildTargets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updateMetaCalls, f.updateSubProjectsCalls, f.updateMobilePlatformCalls, f.updateAppIdentityCalls, f.updateBuildTargetsCalls
}

// --- fakeProjects: ProjectLister ---

type fakeProjects struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.InitiativeProject
}

var _ ProjectLister = (*fakeProjects)(nil)

func newFakeProjects(projects ...domain.InitiativeProject) *fakeProjects {
	m := map[uuid.UUID]domain.InitiativeProject{}
	for _, p := range projects {
		m[p.ID] = p
	}
	return &fakeProjects{byID: m}
}

func (f *fakeProjects) List(ctx context.Context) ([]domain.InitiativeProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.InitiativeProject, 0, len(f.byID))
	for _, p := range f.byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeProjects) Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return domain.InitiativeProject{}, port.ErrNotFound
	}
	return p, nil
}

// --- fakePipelines: PipelineJobWriter ---

type fakePipelines struct {
	mu           sync.Mutex
	byRepo       map[uuid.UUID][]domain.RepositoryPipelineJob
	replaceCalls int
}

var _ PipelineJobWriter = (*fakePipelines)(nil)

func newFakePipelines() *fakePipelines {
	return &fakePipelines{byRepo: map[uuid.UUID][]domain.RepositoryPipelineJob{}}
}

func (f *fakePipelines) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.RepositoryPipelineJob(nil), f.byRepo[repositoryID]...), nil
}

func (f *fakePipelines) ReplaceForRepository(ctx context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replaceCalls++
	out := make([]domain.RepositoryPipelineJob, len(jobs))
	for i, j := range jobs {
		if j.ID == uuid.Nil {
			j.ID = uuid.New()
		}
		j.RepositoryID = repositoryID
		out[i] = j
	}
	f.byRepo[repositoryID] = out
	return append([]domain.RepositoryPipelineJob(nil), out...), nil
}

func (f *fakePipelines) replaceCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replaceCalls
}

// --- fakeScanner: Scanner ---

// fakeScanner lets a test control exactly when a scan's Scan call returns:
// block, if set, is read from before returning, so StartScan's single-flight
// window can be observed deterministically instead of via a sleep.
type fakeScanner struct {
	mu      sync.Mutex
	result  domain.ScanResult
	err     error
	events  []domain.ScanEvent
	block   chan struct{}
	started chan struct{}
	calls   int
}

var _ Scanner = (*fakeScanner)(nil)

func (f *fakeScanner) Scan(ctx context.Context, root string, emit func(domain.ScanEvent)) (domain.ScanResult, error) {
	f.mu.Lock()
	f.calls++
	started := f.started
	block := f.block
	events := f.events
	result, err := f.result, f.err
	f.mu.Unlock()

	if started != nil {
		close(started)
	}
	for _, ev := range events {
		emit(ev)
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return domain.ScanResult{}, ctx.Err()
		}
	}
	return result, err
}

func (f *fakeScanner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// --- fakeEnvironmentStore: port.EnvironmentStore ---

type fakeEnvironmentStore struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.ComponentEnvironment
}

func newFakeEnvironmentStore() *fakeEnvironmentStore {
	return &fakeEnvironmentStore{byID: map[uuid.UUID]domain.ComponentEnvironment{}}
}

var _ port.EnvironmentStore = (*fakeEnvironmentStore)(nil)

func (f *fakeEnvironmentStore) ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ComponentEnvironment
	for _, e := range f.byID {
		if e.RepositoryID == repositoryID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeEnvironmentStore) ListAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.ComponentEnvironment, 0, len(f.byID))
	for _, e := range f.byID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeEnvironmentStore) GetEnvironment(ctx context.Context, id uuid.UUID) (domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.byID[id]
	if !ok {
		return domain.ComponentEnvironment{}, port.ErrNotFound
	}
	return e, nil
}

func (f *fakeEnvironmentStore) SaveEnvironment(ctx context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.byID[e.ID] = e
	return e, nil
}

func (f *fakeEnvironmentStore) DeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeEnvironmentStore) seed(e domain.ComponentEnvironment) domain.ComponentEnvironment {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.byID[e.ID] = e
	return e
}

// --- fakeDeployMatcher: DeployMatcher ---

type deployMatchCall struct {
	repositoryID uuid.UUID
	result       domain.ScanResult
}

type fakeDeployMatcher struct {
	mu    sync.Mutex
	calls []deployMatchCall
	err   error
}

var _ DeployMatcher = (*fakeDeployMatcher)(nil)

func (f *fakeDeployMatcher) MatchScan(ctx context.Context, repositoryID uuid.UUID, result domain.ScanResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deployMatchCall{repositoryID, result})
	return f.err
}

func (f *fakeDeployMatcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}
