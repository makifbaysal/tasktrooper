package release

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeReleaseStore is an in-memory port.ReleaseStore: enough to exercise the
// optimistic Update(r, expect) contract and the List/ForTask/LastReleased
// filters without a database.
type fakeReleaseStore struct {
	mu        sync.Mutex
	releases  map[uuid.UUID]domain.Release
	createErr error
	updateErr error
	// failCreateTimes makes the next N Create calls fail (then succeed),
	// simulating idx_releases_one_draft rejecting a losing racer's insert.
	failCreateTimes int
}

func newFakeReleaseStore() *fakeReleaseStore {
	return &fakeReleaseStore{releases: map[uuid.UUID]domain.Release{}}
}

func (f *fakeReleaseStore) Create(_ context.Context, r domain.Release, taskIDs []uuid.UUID) (domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return domain.Release{}, f.createErr
	}
	if f.failCreateTimes > 0 {
		f.failCreateTimes--
		// Model what a real unique-violation on idx_releases_one_draft means:
		// a concurrent racer's insert already committed and is now visible to
		// a re-read, which is exactly what openBatch does after a failed
		// Create.
		winner := r
		winner.ID = uuid.New()
		winner.Tasks = nil
		f.releases[winner.ID] = winner
		return domain.Release{}, fmt.Errorf("duplicate key value violates unique constraint %q", "idx_releases_one_draft")
	}
	r.ID = uuid.New()
	r.Tasks = nil
	for _, id := range taskIDs {
		r.Tasks = append(r.Tasks, domain.ReleaseTaskRef{ID: id})
	}
	f.releases[r.ID] = r
	return r, nil
}

func (f *fakeReleaseStore) Get(_ context.Context, id uuid.UUID) (domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.releases[id]
	if !ok {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return r, nil
}

func (f *fakeReleaseStore) ForTask(_ context.Context, taskID uuid.UUID) (domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.Release
	found := false
	for _, r := range f.releases {
		for _, t := range r.Tasks {
			if t.ID != taskID {
				continue
			}
			if !found || r.CreatedAt.After(best.CreatedAt) {
				best, found = r, true
			}
		}
	}
	if !found {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return best, nil
}

func (f *fakeReleaseStore) List(_ context.Context, filter domain.ReleaseListFilter) ([]domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Release
	for _, r := range f.releases {
		if filter.RepositoryID != nil && r.RepositoryID != *filter.RepositoryID {
			continue
		}
		if filter.ComponentID != nil && (r.ComponentID == nil || *r.ComponentID != *filter.ComponentID) {
			continue
		}
		if filter.TaskID != nil {
			has := false
			for _, t := range r.Tasks {
				if t.ID == *filter.TaskID {
					has = true
					break
				}
			}
			if !has {
				continue
			}
		}
		if len(filter.Statuses) > 0 {
			match := false
			for _, st := range filter.Statuses {
				if r.Status == st {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeReleaseStore) Update(_ context.Context, r domain.Release, expect domain.ReleaseStatus) (domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return domain.Release{}, f.updateErr
	}
	cur, ok := f.releases[r.ID]
	if !ok {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	if cur.Status != expect {
		return domain.Release{}, fmt.Errorf("%w: actual status is %s", domain.ErrReleaseWrongStatus, cur.Status)
	}
	if r.Tasks == nil {
		r.Tasks = cur.Tasks
	}
	r.UpdatedAt = time.Now()
	f.releases[r.ID] = r
	return r, nil
}

func (f *fakeReleaseStore) AddTasks(_ context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.releases[releaseID]
	if !ok {
		return domain.ErrReleaseNotFound
	}
	existing := map[uuid.UUID]bool{}
	for _, t := range r.Tasks {
		existing[t.ID] = true
	}
	for _, id := range taskIDs {
		if !existing[id] {
			r.Tasks = append(r.Tasks, domain.ReleaseTaskRef{ID: id})
		}
	}
	f.releases[releaseID] = r
	return nil
}

func (f *fakeReleaseStore) AddTasksToDraft(ctx context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) (bool, error) {
	f.mu.Lock()
	r, ok := f.releases[releaseID]
	f.mu.Unlock()
	if !ok {
		return false, domain.ErrReleaseNotFound
	}
	if r.Status != domain.ReleaseDraft {
		return false, nil
	}
	return true, f.AddTasks(ctx, releaseID, taskIDs)
}

func (f *fakeReleaseStore) RemoveTask(_ context.Context, releaseID, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.releases[releaseID]
	if !ok {
		return domain.ErrReleaseNotFound
	}
	out := r.Tasks[:0]
	for _, t := range r.Tasks {
		if t.ID != taskID {
			out = append(out, t)
		}
	}
	r.Tasks = out
	f.releases[releaseID] = r
	return nil
}

func (f *fakeReleaseStore) LastReleased(_ context.Context, repositoryID uuid.UUID, componentID *uuid.UUID, before time.Time) (domain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.Release
	found := false
	for _, r := range f.releases {
		if r.RepositoryID != repositoryID || !sameComponentPtr(r.ComponentID, componentID) {
			continue
		}
		if r.Status != domain.ReleaseReleased || r.FinishedAt == nil || !r.FinishedAt.Before(before) {
			continue
		}
		if !found || r.FinishedAt.After(*best.FinishedAt) {
			best, found = r, true
		}
	}
	if !found {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return best, nil
}

func sameComponentPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// fakeTasks is release.Tasks.
type fakeTasks struct {
	mu       sync.Mutex
	tasks    map[uuid.UUID]domain.BoardTask
	comments []domain.CreateTaskCommentRequest
	updates  []domain.UpdateBoardTaskRequest
	getErr   error
}

func newFakeTasks(tasks ...domain.BoardTask) *fakeTasks {
	m := map[uuid.UUID]domain.BoardTask{}
	for _, t := range tasks {
		m[t.ID] = t
	}
	return &fakeTasks{tasks: m}
}

func (f *fakeTasks) GetTask(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return domain.BoardTask{}, f.getErr
	}
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("task not found")
	}
	return t, nil
}

func (f *fakeTasks) UpdateTask(_ context.Context, _, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, req)
	t, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("task not found")
	}
	if req.Column != nil {
		t.Column = *req.Column
	}
	f.tasks[taskID] = t
	return t, nil
}

func (f *fakeTasks) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, req)
	return domain.TaskComment{Content: req.Content}, nil
}

func (f *fakeTasks) ListTasks(_ context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.BoardTask
	for _, t := range f.tasks {
		if t.RepositoryID == repositoryID {
			out = append(out, t)
		}
	}
	return out, nil
}

// fakeParked is release.ParkedTasks.
type fakeParked struct {
	mu     sync.Mutex
	parked map[uuid.UUID]domain.BoardTask
	taken  []uuid.UUID
}

func newFakeParked(tasks ...domain.BoardTask) *fakeParked {
	m := map[uuid.UUID]domain.BoardTask{}
	for _, t := range tasks {
		m[t.ID] = t
	}
	return &fakeParked{parked: m}
}

func (f *fakeParked) TakeBlockedResourceTask(_ context.Context, _ string, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.parked[taskID]
	if !ok {
		return domain.BoardTask{}, false, nil
	}
	delete(f.parked, taskID)
	f.taken = append(f.taken, taskID)
	return t, true, nil
}

// fakeMergeState is release.MergeStateResetter.
type fakeMergeState struct {
	mu    sync.Mutex
	reset []uuid.UUID
}

func (f *fakeMergeState) ResetMergeState(_ context.Context, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reset = append(f.reset, taskID)
	return nil
}

// fakeBeforeDeployConfirmer is release.BeforeDeployConfirmer.
type fakeBeforeDeployConfirmer struct {
	mu        sync.Mutex
	confirmed []uuid.UUID
	err       error
}

func (f *fakeBeforeDeployConfirmer) ConfirmBeforeDeploy(_ context.Context, _, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.confirmed = append(f.confirmed, taskID)
	return nil
}

// fakeWaker is release.Waker.
type wakeCall struct {
	repositoryID uuid.UUID
	task         domain.BoardTask
	status       domain.ReleaseStatus
}

type fakeWaker struct {
	mu    sync.Mutex
	calls []wakeCall
}

func (f *fakeWaker) Wake(_ context.Context, repositoryID uuid.UUID, task domain.BoardTask, status domain.ReleaseStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, wakeCall{repositoryID, task, status})
	return nil
}

// fakeComponents is release.Components.
type fakeComponents struct {
	byID   map[uuid.UUID]domain.Component
	byPath map[string]domain.Component
	all    map[uuid.UUID][]domain.Component
}

func newFakeComponents() *fakeComponents {
	return &fakeComponents{byID: map[uuid.UUID]domain.Component{}, byPath: map[string]domain.Component{}, all: map[uuid.UUID][]domain.Component{}}
}

func (f *fakeComponents) add(repositoryID uuid.UUID, c domain.Component) domain.Component {
	c.RepositoryID = repositoryID
	f.byID[c.ID] = c
	f.byPath[repositoryID.String()+"|"+c.Path] = c
	f.all[repositoryID] = append(f.all[repositoryID], c)
	return c
}

func (f *fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := f.byID[id]
	if !ok {
		return domain.Component{}, fmt.Errorf("component not found")
	}
	return c, nil
}

func (f *fakeComponents) ComponentByPath(_ context.Context, repositoryID uuid.UUID, path string) (domain.Component, error) {
	c, ok := f.byPath[repositoryID.String()+"|"+path]
	if !ok {
		return domain.Component{}, fmt.Errorf("component not found at %s", path)
	}
	return c, nil
}

func (f *fakeComponents) ListComponents(_ context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	return f.all[repositoryID], nil
}

// fakeEnvironments is release.Environments.
type fakeEnvironments struct {
	mu          sync.Mutex
	envs        map[uuid.UUID][]domain.ComponentEnvironment
	deployments map[uuid.UUID][]domain.CloudDeployment
	errorGroups map[uuid.UUID][]domain.RuntimeErrorGroup
	errorsErr   error

	// rollbackable/current/rollbackErr/promoteErr are keyed by environment id
	// and drive the WP-M-shaped provider-rollback surface: CanRollback,
	// CurrentDeployment, RollbackEnvironment, PromoteDeployment.
	rollbackable map[uuid.UUID]bool
	current      map[uuid.UUID]domain.CloudDeployment
	currentErr   map[uuid.UUID]error
	rollbackErr  map[uuid.UUID]error
	promoteErr   map[uuid.UUID]error

	rollbackCalls []envDeploymentCall
	promoteCalls  []envDeploymentCall
}

type envDeploymentCall struct {
	envID        uuid.UUID
	deploymentID string
}

func newFakeEnvironments() *fakeEnvironments {
	return &fakeEnvironments{
		envs:         map[uuid.UUID][]domain.ComponentEnvironment{},
		deployments:  map[uuid.UUID][]domain.CloudDeployment{},
		errorGroups:  map[uuid.UUID][]domain.RuntimeErrorGroup{},
		rollbackable: map[uuid.UUID]bool{},
		current:      map[uuid.UUID]domain.CloudDeployment{},
		currentErr:   map[uuid.UUID]error{},
		rollbackErr:  map[uuid.UUID]error{},
		promoteErr:   map[uuid.UUID]error{},
	}
}

func (f *fakeEnvironments) ListEnvironments(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	return f.envs[repositoryID], nil
}

func (f *fakeEnvironments) Deployments(_ context.Context, envID uuid.UUID, _ int) ([]domain.CloudDeployment, error) {
	return f.deployments[envID], nil
}

func (f *fakeEnvironments) Errors(_ context.Context, envID uuid.UUID, _ time.Time) ([]domain.RuntimeErrorGroup, error) {
	if f.errorsErr != nil {
		return nil, f.errorsErr
	}
	return f.errorGroups[envID], nil
}

func (f *fakeEnvironments) CanRollback(_ context.Context, envID uuid.UUID) bool {
	return f.rollbackable[envID]
}

func (f *fakeEnvironments) CurrentDeployment(_ context.Context, envID uuid.UUID) (domain.CloudDeployment, error) {
	if err := f.currentErr[envID]; err != nil {
		return domain.CloudDeployment{}, err
	}
	return f.current[envID], nil
}

func (f *fakeEnvironments) RollbackEnvironment(_ context.Context, envID uuid.UUID, deploymentID string) error {
	f.mu.Lock()
	f.rollbackCalls = append(f.rollbackCalls, envDeploymentCall{envID: envID, deploymentID: deploymentID})
	f.mu.Unlock()
	return f.rollbackErr[envID]
}

func (f *fakeEnvironments) PromoteDeployment(_ context.Context, envID uuid.UUID, deploymentID string) error {
	f.mu.Lock()
	f.promoteCalls = append(f.promoteCalls, envDeploymentCall{envID: envID, deploymentID: deploymentID})
	f.mu.Unlock()
	if err := f.promoteErr[envID]; err != nil {
		return err
	}
	return nil
}

// fakeDeployStatus is release.DeployStatus.
type deployStatusCall struct{ sha, workflow string }

type fakeDeployStatus struct {
	mu    sync.Mutex
	byKey map[string]domain.DeployWatchStatus
	err   error
	calls []deployStatusCall
}

func newFakeDeployStatus() *fakeDeployStatus {
	return &fakeDeployStatus{byKey: map[string]domain.DeployWatchStatus{}}
}

func (f *fakeDeployStatus) set(sha, workflow string, status domain.DeployWatchStatus) {
	f.byKey[sha+"|"+workflow] = status
}

func (f *fakeDeployStatus) StatusForCommit(_ context.Context, _ uuid.UUID, sha, workflow string) (domain.DeployWatchStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deployStatusCall{sha, workflow})
	if f.err != nil {
		return domain.DeployWatchStatus{}, f.err
	}
	if st, ok := f.byKey[sha+"|"+workflow]; ok {
		return st, nil
	}
	return domain.DeployWatchStatus{State: domain.DeployWatchUnknown}, nil
}

// fakeActions is port.ActionsClient.
type fakeActions struct {
	mu             sync.Mutex
	createTagErr   error
	dispatchErr    error
	tagCalls       []string
	dispatchCalls  []string
	commitStatuses map[string]port.CommitDeploySignal
}

func newFakeActions() *fakeActions { return &fakeActions{} }

func (f *fakeActions) ListWorkflowRuns(context.Context, string, string, string, string) ([]port.ActionsRun, error) {
	return nil, nil
}

func (f *fakeActions) DispatchWorkflow(_ context.Context, _, _, workflowFile, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatchCalls = append(f.dispatchCalls, workflowFile+"@"+ref)
	return f.dispatchErr
}

func (f *fakeActions) CreateTag(_ context.Context, _, _, tag, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tagCalls = append(f.tagCalls, tag+"@"+sha)
	return f.createTagErr
}

func (f *fakeActions) ListRunsForCommit(context.Context, string, string, string) ([]port.ActionsRun, error) {
	return nil, nil
}

func (f *fakeActions) ListRunJobs(context.Context, string, string, int64) ([]port.ActionsJob, error) {
	return nil, nil
}

func (f *fakeActions) JobLogs(context.Context, string, string, int64) (string, error) { return "", nil }

func (f *fakeActions) CommitDeployStatus(_ context.Context, _, _, sha string) (port.CommitDeploySignal, error) {
	return f.commitStatuses[sha], nil
}

// fakeReverter is release.Reverter.
type revertCall struct {
	rootPath string
	shas     []string
	message  string
}

type fakeReverter struct {
	mu    sync.Mutex
	sha   string
	err   error
	calls []revertCall
}

func (f *fakeReverter) RevertOnDefaultBranch(_ context.Context, rootPath string, shas []string, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, revertCall{rootPath, append([]string{}, shas...), message})
	if f.err != nil {
		return "", f.err
	}
	return f.sha, nil
}

// fakeRepos is release.Repos.
type fakeRepos struct {
	repo domain.Repository
	err  error
}

func (f *fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	return f.repo, f.err
}

// fakeIncidents is release.IncidentIngester.
type fakeIncidents struct {
	mu        sync.Mutex
	ingested  []domain.IncidentInput
	ingestErr error
}

func (f *fakeIncidents) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingested = append(f.ingested, in)
	return domain.Incident{}, f.ingestErr
}

// steppableClock is a monotonic-ish fake clock tests advance explicitly.
type steppableClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSteppableClock(start time.Time) *steppableClock { return &steppableClock{now: start} }

func (c *steppableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *steppableClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func alwaysCIUnavailable(string) bool { return true }

func neverCIUnavailable(string) bool { return false }

func alwaysRefExists(error) bool { return true }

// fakeGit is release.Git.
type isAncestorCall struct{ ancestor, descendant string }

type fakeGit struct {
	mu   sync.Mutex
	head string
	// ancestors maps a sha to whether it is an ancestor of the head passed to
	// IsAncestor; a sha absent from the map is treated as NOT an ancestor
	// (never seen on the default branch), matching a merge commit that never
	// landed rather than an error.
	ancestors map[string]bool
	tag       string

	headErr       error
	isAncestorErr error
	tagErr        error

	isAncestorCalls []isAncestorCall
}

func newFakeGit() *fakeGit {
	return &fakeGit{ancestors: map[string]bool{}}
}

func (f *fakeGit) RemoteHead(context.Context, string) (string, error) {
	if f.headErr != nil {
		return "", f.headErr
	}
	return f.head, nil
}

func (f *fakeGit) IsAncestor(_ context.Context, _, ancestor, descendant string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.isAncestorCalls = append(f.isAncestorCalls, isAncestorCall{ancestor, descendant})
	if f.isAncestorErr != nil {
		return false, f.isAncestorErr
	}
	return f.ancestors[ancestor], nil
}

func (f *fakeGit) LatestTag(context.Context, string, string) (string, error) {
	if f.tagErr != nil {
		return "", f.tagErr
	}
	return f.tag, nil
}

// fakeLocalRunner is release.LocalRunner: it does not actually run anything —
// it records the spec and lets the test call the done callback itself,
// whenever it wants (synchronously, no goroutine race in the test).
type fakeLocalRunner struct {
	mu       sync.Mutex
	startErr error
	specs    []LocalRunSpec
	dones    []func(exitCode int, tail string, err error)
}

func (f *fakeLocalRunner) Start(_ context.Context, spec LocalRunSpec, done func(exitCode int, tail string, err error)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.specs = append(f.specs, spec)
	f.dones = append(f.dones, done)
	return nil
}

// fakeStoreOps is release.Store (the storeops slice).
type fakeStoreBuildCall struct {
	repositoryID            uuid.UUID
	platform, engine, actor string
}

type fakeStoreOps struct {
	mu     sync.Mutex
	apps   map[uuid.UUID][]domain.MobileStoreApp
	tracks map[string]domain.StoreTracks // key: repositoryID|platform
	// startErr, keyed by platform, fails StartBuild for just that platform.
	startErr map[string]error
	// tracksErr, keyed by platform, fails Tracks for just that platform.
	tracksErr map[string]error

	startCalls []fakeStoreBuildCall
}

func newFakeStoreOps() *fakeStoreOps {
	return &fakeStoreOps{
		apps:      map[uuid.UUID][]domain.MobileStoreApp{},
		tracks:    map[string]domain.StoreTracks{},
		startErr:  map[string]error{},
		tracksErr: map[string]error{},
	}
}

func (f *fakeStoreOps) trackKey(repositoryID uuid.UUID, platform string) string {
	return repositoryID.String() + "|" + platform
}

func (f *fakeStoreOps) setTracks(repositoryID uuid.UUID, platform string, t domain.StoreTracks) {
	f.tracks[f.trackKey(repositoryID, platform)] = t
}

func (f *fakeStoreOps) AppsByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.MobileStoreApp, error) {
	return f.apps[repositoryID], nil
}

func (f *fakeStoreOps) Tracks(_ context.Context, repositoryID uuid.UUID, platform string) (domain.StoreTracks, error) {
	if err := f.tracksErr[platform]; err != nil {
		return domain.StoreTracks{}, err
	}
	return f.tracks[f.trackKey(repositoryID, platform)], nil
}

func (f *fakeStoreOps) StartBuild(_ context.Context, repositoryID uuid.UUID, platform, engine, actor string) (storeops.BuildStart, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls = append(f.startCalls, fakeStoreBuildCall{repositoryID, platform, engine, actor})
	if err := f.startErr[platform]; err != nil {
		return storeops.BuildStart{}, err
	}
	return storeops.BuildStart{Platform: platform, Engine: "github_actions"}, nil
}

// fakeDeployTargets is the legacy port.DeployTargetStore fallback.
type fakeDeployTargets struct {
	target domain.DeployTarget
	err    error
}

func (f *fakeDeployTargets) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargets) ListAll(context.Context) ([]domain.DeployTarget, error) { return nil, nil }
func (f *fakeDeployTargets) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	if f.err != nil {
		return domain.DeployTarget{}, f.err
	}
	return f.target, nil
}
func (f *fakeDeployTargets) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	f.target = t
	return t, nil
}
func (f *fakeDeployTargets) Delete(context.Context, uuid.UUID, string, string) error { return nil }

// raceyDraftStore wraps fakeReleaseStore to make AddTasksToDraft report a
// lost race (false, nil) for its first N calls before delegating — modelling
// a draft that was cut or superseded between openBatch's find and its add,
// without needing real concurrency in the fake.
type raceyDraftStore struct {
	*fakeReleaseStore
	failAddToDraftTimes int
}

func (f *raceyDraftStore) AddTasksToDraft(ctx context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) (bool, error) {
	if f.failAddToDraftTimes > 0 {
		f.failAddToDraftTimes--
		return false, nil
	}
	return f.fakeReleaseStore.AddTasksToDraft(ctx, releaseID, taskIDs)
}
