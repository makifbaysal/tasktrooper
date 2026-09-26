package cloud_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func ptr[T any](v T) *T { return &v }

// --- fakeAccounts: port.CloudAccountStore ---

type fakeAccounts struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]domain.CloudAccount
	fields  map[uuid.UUID]map[string]string
	creates int
	updates int
	deletes int
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{byID: map[uuid.UUID]domain.CloudAccount{}, fields: map[uuid.UUID]map[string]string{}}
}

var _ port.CloudAccountStore = (*fakeAccounts)(nil)

func (f *fakeAccounts) ListCloudAccounts(ctx context.Context) ([]domain.CloudAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.CloudAccount, 0, len(f.byID))
	for _, a := range f.byID {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeAccounts) GetCloudAccount(ctx context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return domain.CloudAccount{}, port.ErrNotFound
	}
	return a, nil
}

func (f *fakeAccounts) CreateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if len(fields) == 0 {
		return domain.CloudAccount{}, errors.New("fields required")
	}
	if acct.ID == uuid.Nil {
		acct.ID = uuid.New()
	}
	acct.CreatedAt, acct.UpdatedAt = time.Now(), time.Now()
	f.byID[acct.ID] = acct
	f.fields[acct.ID] = fields
	return acct, nil
}

func (f *fakeAccounts) UpdateCloudAccount(ctx context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	if _, ok := f.byID[acct.ID]; !ok {
		return domain.CloudAccount{}, port.ErrNotFound
	}
	if fields != nil {
		f.fields[acct.ID] = fields
	}
	f.byID[acct.ID] = acct
	return acct, nil
}

func (f *fakeAccounts) DeleteCloudAccount(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	if _, ok := f.byID[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.byID, id)
	delete(f.fields, id)
	return nil
}

func (f *fakeAccounts) CloudCredential(ctx context.Context, id uuid.UUID) (domain.CloudCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return domain.CloudCredential{}, port.ErrNotFound
	}
	return domain.CloudCredential{AccountID: id, Provider: a.Provider, Meta: a.Meta, Fields: f.fields[id]}, nil
}

func (f *fakeAccounts) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

// --- fakeEnvironments: port.EnvironmentStore ---

type fakeEnvironments struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]domain.ComponentEnvironment
	saves int
}

func newFakeEnvironments() *fakeEnvironments {
	return &fakeEnvironments{byID: map[uuid.UUID]domain.ComponentEnvironment{}}
}

var _ port.EnvironmentStore = (*fakeEnvironments)(nil)

func (f *fakeEnvironments) ListEnvironments(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
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

func (f *fakeEnvironments) ListAllEnvironments(ctx context.Context) ([]domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.ComponentEnvironment, 0, len(f.byID))
	for _, e := range f.byID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeEnvironments) GetEnvironment(ctx context.Context, id uuid.UUID) (domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.byID[id]
	if !ok {
		return domain.ComponentEnvironment{}, port.ErrNotFound
	}
	return e, nil
}

func (f *fakeEnvironments) SaveEnvironment(ctx context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	for id, existing := range f.byID {
		if existing.ComponentID == e.ComponentID && existing.Environment == e.Environment && id != e.ID {
			e.ID = id
			e.CreatedAt = existing.CreatedAt
			break
		}
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Status == "" {
		e.Status = domain.LinkSuggested
	}
	if e.Source == "" {
		e.Source = domain.LinkSourceScan
	}
	if e.Confidence == "" {
		e.Confidence = domain.ConfidenceLow
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.UpdatedAt = time.Now()
	f.byID[e.ID] = e
	return e, nil
}

func (f *fakeEnvironments) DeleteEnvironment(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return port.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeEnvironments) seed(e domain.ComponentEnvironment) domain.ComponentEnvironment {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.byID[e.ID] = e
	return e
}

func (f *fakeEnvironments) saveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves
}

// --- fakeComponents: port.ComponentStore ---

type fakeComponents struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.Component
}

func newFakeComponents() *fakeComponents {
	return &fakeComponents{byID: map[uuid.UUID]domain.Component{}}
}

var _ port.ComponentStore = (*fakeComponents)(nil)

func (f *fakeComponents) ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Component
	for _, c := range f.byID {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (f *fakeComponents) ListComponentsForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error) {
	want := map[uuid.UUID]bool{}
	for _, id := range repositoryIDs {
		want[id] = true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Component
	for _, c := range f.byID {
		if want[c.RepositoryID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeComponents) ListAllComponents(ctx context.Context) ([]domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Component, 0, len(f.byID))
	for _, c := range f.byID {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeComponents) GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return domain.Component{}, port.ErrNotFound
	}
	return c, nil
}

func (f *fakeComponents) SaveComponent(ctx context.Context, c domain.Component) (domain.Component, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeComponents) seed(c domain.Component) domain.Component {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.Status == "" {
		c.Status = domain.ComponentStatusActive
	}
	f.byID[c.ID] = c
	return c
}

// --- fakeScans: port.ScanStore ---

type fakeScans struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.ProjectScan
}

func newFakeScans() *fakeScans {
	return &fakeScans{byID: map[uuid.UUID]domain.ProjectScan{}}
}

var _ port.ScanStore = (*fakeScans)(nil)

func (f *fakeScans) CreateScan(ctx context.Context, sc domain.ProjectScan) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sc.ID == uuid.Nil {
		sc.ID = uuid.New()
	}
	f.byID[sc.ID] = sc
	return sc, nil
}

func (f *fakeScans) UpdateScan(ctx context.Context, sc domain.ProjectScan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[sc.ID] = sc
	return nil
}

func (f *fakeScans) GetScan(ctx context.Context, id uuid.UUID) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc, ok := f.byID[id]
	if !ok {
		return domain.ProjectScan{}, port.ErrNotFound
	}
	return sc, nil
}

func (f *fakeScans) LatestScan(ctx context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest domain.ProjectScan
	found := false
	for _, sc := range f.byID {
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
	return latest, nil
}

func (f *fakeScans) FailInterruptedScans(ctx context.Context) (int, error) { return 0, nil }

func (f *fakeScans) seed(sc domain.ProjectScan) domain.ProjectScan {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sc.ID == uuid.Nil {
		sc.ID = uuid.New()
	}
	f.byID[sc.ID] = sc
	return sc
}

// --- fakeRepos: cloud.Repos ---

type fakeRepos struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.Repository
}

func newFakeRepos(repos ...domain.Repository) *fakeRepos {
	m := map[uuid.UUID]domain.Repository{}
	for _, r := range repos {
		m[r.ID] = r
	}
	return &fakeRepos{byID: m}
}

var _ cloud.Repos = (*fakeRepos)(nil)

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

// --- fakeDeployTargets: port.DeployTargetStore ---

type fakeDeployTargets struct {
	mu   sync.Mutex
	byID map[string]domain.DeployTarget
}

func newFakeDeployTargets() *fakeDeployTargets {
	return &fakeDeployTargets{byID: map[string]domain.DeployTarget{}}
}

var _ port.DeployTargetStore = (*fakeDeployTargets)(nil)

func deployTargetKey(repositoryID uuid.UUID, subProjectPath, env string) string {
	return repositoryID.String() + "|" + subProjectPath + "|" + env
}

func (f *fakeDeployTargets) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.DeployTarget
	for _, t := range f.byID {
		if t.RepositoryID == repositoryID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDeployTargets) ListAll(ctx context.Context) ([]domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.DeployTarget, 0, len(f.byID))
	for _, t := range f.byID {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeDeployTargets) Get(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.byID[deployTargetKey(repositoryID, subProjectPath, env)]
	if !ok {
		return domain.DeployTarget{}, port.ErrNotFound
	}
	return t, nil
}

func (f *fakeDeployTargets) Save(ctx context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.byID[deployTargetKey(t.RepositoryID, t.SubProjectPath, t.Env)] = t
	return t, nil
}

func (f *fakeDeployTargets) Delete(ctx context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, deployTargetKey(repositoryID, subProjectPath, env))
	return nil
}

func (f *fakeDeployTargets) seed(t domain.DeployTarget) domain.DeployTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.byID[deployTargetKey(t.RepositoryID, t.SubProjectPath, t.Env)] = t
	return t
}

// --- fakeTasks: cloud.TaskCreator ---

type fakeTasks struct {
	mu    sync.Mutex
	calls []domain.CreateBoardTaskRequest
	repos []uuid.UUID
}

var _ cloud.TaskCreator = (*fakeTasks)(nil)

func (f *fakeTasks) CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	f.repos = append(f.repos, repositoryID)
	return domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Title: req.Title, TaskType: req.TaskType, Description: req.Description, ComponentID: req.ComponentID}, nil
}

func (f *fakeTasks) lastCall() (domain.CreateBoardTaskRequest, uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1], f.repos[len(f.repos)-1]
}

func (f *fakeTasks) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// --- fakeLegacy: port.LegacyCloudSource ---

type fakeLegacy struct {
	vercelCred   domain.LegacyVercelCredential
	hasVercel    bool
	gcloudCred   domain.LegacyGCloudCredential
	hasGCloud    bool
	projectLinks []domain.LegacyVercelProjectLink
	hostingLinks []domain.LegacyHostingLink
	gcloudBinds  []domain.LegacyGCloudResourceBinding
}

var _ port.LegacyCloudSource = (*fakeLegacy)(nil)

func (f *fakeLegacy) LegacyVercelCredential(ctx context.Context) (domain.LegacyVercelCredential, bool, error) {
	return f.vercelCred, f.hasVercel, nil
}

func (f *fakeLegacy) LegacyGCloudCredential(ctx context.Context) (domain.LegacyGCloudCredential, bool, error) {
	return f.gcloudCred, f.hasGCloud, nil
}

func (f *fakeLegacy) ListLegacyVercelProjectLinks(ctx context.Context) ([]domain.LegacyVercelProjectLink, error) {
	return f.projectLinks, nil
}

func (f *fakeLegacy) ListLegacyVercelHostingLinks(ctx context.Context) ([]domain.LegacyHostingLink, error) {
	return f.hostingLinks, nil
}

func (f *fakeLegacy) ListLegacyGCloudResourceBindings(ctx context.Context) ([]domain.LegacyGCloudResourceBinding, error) {
	return f.gcloudBinds, nil
}

// --- fakeDeliveryRefresher: cloud.DeliveryRefresher ---

type alignCall struct {
	componentID uuid.UUID
	provider    domain.CloudProviderKind
}

type fakeDeliveryRefresher struct {
	mu           sync.Mutex
	refreshCalls []uuid.UUID
	alignCalls   []alignCall
	alignErr     error
}

var _ cloud.DeliveryRefresher = (*fakeDeliveryRefresher)(nil)

func (f *fakeDeliveryRefresher) RefreshDelivery(ctx context.Context, repositoryID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls = append(f.refreshCalls, repositoryID)
	return nil
}

func (f *fakeDeliveryRefresher) AlignDeliveryToProduction(ctx context.Context, componentID uuid.UUID, provider domain.CloudProviderKind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alignCalls = append(f.alignCalls, alignCall{componentID: componentID, provider: provider})
	return f.alignErr
}

func (f *fakeDeliveryRefresher) alignCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.alignCalls)
}

func (f *fakeDeliveryRefresher) lastAlignCall() alignCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alignCalls[len(f.alignCalls)-1]
}

// --- fakeProvider: port.CloudProvider ---

type fakeProvider struct {
	mu sync.Mutex

	kind domain.CloudProviderKind

	verifyMeta  map[string]string
	verifyErr   error
	verifyCalls int

	resources    []domain.CloudResource
	resourcesErr error

	detail    domain.CloudResourceDetail
	detailErr error

	deployments     []domain.CloudDeployment
	deploymentsErr  error
	deploymentsEnvs []domain.DeployEnvironment

	logs     domain.RuntimeLogPage
	logsErr  error
	logsRefs []domain.CloudResourceRef

	errorGroups []domain.RuntimeErrorGroup
	errorsErr   error
}

var _ port.CloudProvider = (*fakeProvider)(nil)

func (f *fakeProvider) Kind() domain.CloudProviderKind { return f.kind }

func (f *fakeProvider) Verify(ctx context.Context, cred domain.CloudCredential) (map[string]string, error) {
	f.mu.Lock()
	f.verifyCalls++
	f.mu.Unlock()
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return f.verifyMeta, nil
}

func (f *fakeProvider) ListResources(ctx context.Context, cred domain.CloudCredential) ([]domain.CloudResource, error) {
	return f.resources, f.resourcesErr
}

func (f *fakeProvider) Resource(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	return f.detail, f.detailErr
}

func (f *fakeProvider) Deployments(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, env domain.DeployEnvironment, limit int) ([]domain.CloudDeployment, error) {
	f.mu.Lock()
	f.deploymentsEnvs = append(f.deploymentsEnvs, env)
	f.mu.Unlock()
	return f.deployments, f.deploymentsErr
}

func (f *fakeProvider) Logs(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, q domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	f.mu.Lock()
	f.logsRefs = append(f.logsRefs, ref)
	f.mu.Unlock()
	return f.logs, f.logsErr
}

func (f *fakeProvider) Errors(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, since time.Time) ([]domain.RuntimeErrorGroup, error) {
	return f.errorGroups, f.errorsErr
}

func (f *fakeProvider) getVerifyCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verifyCalls
}
