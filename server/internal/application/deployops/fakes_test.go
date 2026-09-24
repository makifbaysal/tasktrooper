package deployops_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func runTime(r domain.DeploymentRun) time.Time {
	if r.StartedAt != nil {
		return *r.StartedAt
	}
	return time.Time{}
}

func runIsNewer(a, b domain.DeploymentRun) bool {
	at, bt := runTime(a), runTime(b)
	if !at.Equal(bt) {
		return at.After(bt)
	}
	return a.CreatedAt.After(b.CreatedAt)
}

type fakeDeploymentRunStore struct {
	mu   sync.Mutex
	rows []domain.DeploymentRun
}

var _ port.DeploymentRunStore = (*fakeDeploymentRunStore)(nil)

func newFakeDeploymentRunStore(rows []domain.DeploymentRun) *fakeDeploymentRunStore {
	return &fakeDeploymentRunStore{rows: append([]domain.DeploymentRun(nil), rows...)}
}

func (f *fakeDeploymentRunStore) Upsert(_ context.Context, run domain.DeploymentRun) (domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.RepositoryID == run.RepositoryID && r.RunID == run.RunID {
			f.rows[i] = run
			return run, nil
		}
	}
	f.rows = append(f.rows, run)
	return run, nil
}

func (f *fakeDeploymentRunStore) ByRunID(_ context.Context, repositoryID uuid.UUID, runID int64) (domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.RunID == runID {
			return r, nil
		}
	}
	return domain.DeploymentRun{}, fmt.Errorf("run %d: %w", runID, port.ErrNotFound)
}

func (f *fakeDeploymentRunStore) Latest(_ context.Context, repositoryID uuid.UUID, env string) (domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.DeploymentRun
	found := false
	for _, r := range f.rows {
		if r.RepositoryID != repositoryID || r.Env != env {
			continue
		}
		if !found || runIsNewer(r, best) {
			best, found = r, true
		}
	}
	if !found {
		return domain.DeploymentRun{}, fmt.Errorf("get latest run: %w", port.ErrNotFound)
	}
	return best, nil
}

func (f *fakeDeploymentRunStore) LatestAll(_ context.Context) ([]domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type key struct {
		repositoryID uuid.UUID
		env          string
	}
	best := map[key]domain.DeploymentRun{}
	for _, r := range f.rows {
		k := key{r.RepositoryID, r.Env}
		if cur, ok := best[k]; !ok || runIsNewer(r, cur) {
			best[k] = r
		}
	}
	out := make([]domain.DeploymentRun, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RepositoryID != out[j].RepositoryID {
			return out[i].RepositoryID.String() < out[j].RepositoryID.String()
		}
		return out[i].Env < out[j].Env
	})
	return out, nil
}

func (f *fakeDeploymentRunStore) ListByEnv(_ context.Context, repositoryID uuid.UUID, env string, limit int) ([]domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var matched []domain.DeploymentRun
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID && r.Env == env {
			matched = append(matched, r)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return runIsNewer(matched[i], matched[j]) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func (f *fakeDeploymentRunStore) LastSuccessfulBefore(_ context.Context, repositoryID uuid.UUID, env, excludeSHA string) (domain.DeploymentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best domain.DeploymentRun
	found := false
	for _, r := range f.rows {
		if r.RepositoryID != repositoryID || r.Env != env {
			continue
		}
		if r.Conclusion != domain.RunConclusionSuccess {
			continue
		}
		if r.HeadSHA == "" || r.HeadSHA == excludeSHA {
			continue
		}
		if !found || runIsNewer(r, best) {
			best, found = r, true
		}
	}
	if !found {
		return domain.DeploymentRun{}, fmt.Errorf("get last successful run: %w", port.ErrNotFound)
	}
	return best, nil
}

func (f *fakeDeploymentRunStore) Stamp(_ context.Context, repositoryID uuid.UUID, runID int64, triggerSource, triggeredBy, rollbackOfSHA string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.RepositoryID != repositoryID || r.RunID != runID {
			continue
		}
		if triggerSource != "" {
			f.rows[i].TriggerSource = triggerSource
		}
		if triggeredBy != "" {
			f.rows[i].TriggeredBy = triggeredBy
		}
		if rollbackOfSHA != "" {
			f.rows[i].RollbackOfSHA = rollbackOfSHA
		}
		return nil
	}
	return fmt.Errorf("stamp run: %w", port.ErrNotFound)
}

type fakeDeployDispatchStore struct {
	mu   sync.Mutex
	rows []domain.DeployDispatch
}

var _ port.DeployDispatchStore = (*fakeDeployDispatchStore)(nil)

func newFakeDeployDispatchStore(rows []domain.DeployDispatch) *fakeDeployDispatchStore {
	return &fakeDeployDispatchStore{rows: append([]domain.DeployDispatch(nil), rows...)}
}

func (f *fakeDeployDispatchStore) Create(_ context.Context, d domain.DeployDispatch) (domain.DeployDispatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	f.rows = append(f.rows, d)
	return d, nil
}

func (f *fakeDeployDispatchStore) ListPending(_ context.Context) ([]domain.DeployDispatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.DeployDispatch
	for _, d := range f.rows {
		if d.State == domain.DispatchStatePending {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDeployDispatchStore) Resolve(_ context.Context, id uuid.UUID, state string, runID *int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, d := range f.rows {
		if d.ID == id {
			f.rows[i].State = state
			f.rows[i].MatchedRunID = runID
			return nil
		}
	}
	return fmt.Errorf("resolve dispatch: %w", port.ErrNotFound)
}

type fakeOpsAuditStore struct {
	mu   sync.Mutex
	rows []domain.OpsAuditEntry
}

var _ port.OpsAuditStore = (*fakeOpsAuditStore)(nil)

func newFakeOpsAuditStore(rows []domain.OpsAuditEntry) *fakeOpsAuditStore {
	return &fakeOpsAuditStore{rows: append([]domain.OpsAuditEntry(nil), rows...)}
}

func (f *fakeOpsAuditStore) Log(_ context.Context, entry domain.OpsAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	f.rows = append([]domain.OpsAuditEntry{entry}, f.rows...)
	return nil
}

func (f *fakeOpsAuditStore) List(_ context.Context, repositoryID *uuid.UUID, limit int) ([]domain.OpsAuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.OpsAuditEntry
	for _, e := range f.rows {
		if repositoryID != nil {
			if e.RepositoryID == nil || *e.RepositoryID != *repositoryID {
				continue
			}
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fakeDeployTargetStore struct {
	mu   sync.Mutex
	rows []domain.DeployTarget
}

var _ port.DeployTargetStore = (*fakeDeployTargetStore)(nil)

func newFakeDeployTargetStore(rows []domain.DeployTarget) *fakeDeployTargetStore {
	return &fakeDeployTargetStore{rows: append([]domain.DeployTarget(nil), rows...)}
}

func (f *fakeDeployTargetStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.DeployTarget
	for _, t := range f.rows {
		if t.RepositoryID == repositoryID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDeployTargetStore) ListAll(_ context.Context) ([]domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.DeployTarget(nil), f.rows...), nil
}

func (f *fakeDeployTargetStore) Get(_ context.Context, repositoryID uuid.UUID, subProjectPath, env string) (domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.rows {
		if t.RepositoryID == repositoryID && t.Env == env {
			return t, nil
		}
	}
	return domain.DeployTarget{}, fmt.Errorf("get deploy target: %w", port.ErrNotFound)
}

func (f *fakeDeployTargetStore) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.rows {
		if existing.RepositoryID == t.RepositoryID && existing.Env == t.Env {
			f.rows[i] = t
			return t, nil
		}
	}
	f.rows = append(f.rows, t)
	return t, nil
}

func (f *fakeDeployTargetStore) Delete(_ context.Context, repositoryID uuid.UUID, subProjectPath, env string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.rows {
		if existing.RepositoryID == repositoryID && existing.Env == env {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

type fakeRepositoryStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.Repository
}

var _ port.RepositoryStore = (*fakeRepositoryStore)(nil)

func newFakeRepositoryStore(rows []domain.Repository) *fakeRepositoryStore {
	m := make(map[uuid.UUID]domain.Repository, len(rows))
	for _, r := range rows {
		m[r.ID] = r
	}
	return &fakeRepositoryStore{rows: m}
}

func (f *fakeRepositoryStore) Create(_ context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := domain.Repository{ID: uuid.New(), Name: name, Description: description, RootPath: rootPath, RemoteURL: remoteURL}
	f.rows[r.ID] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateRemoteURL(_ context.Context, id uuid.UUID, remoteURL string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository remote url: %w", port.ErrNotFound)
	}
	r.RemoteURL = remoteURL
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateRootPath(_ context.Context, id uuid.UUID, rootPath string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository root path: %w", port.ErrNotFound)
	}
	r.RootPath = rootPath
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("get repository: %w", port.ErrNotFound)
	}
	return r, nil
}

func (f *fakeRepositoryStore) GetByRootPath(_ context.Context, rootPath string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.RootPath == rootPath {
			return r, nil
		}
	}
	return domain.Repository{}, fmt.Errorf("get repository by root path: %w", port.ErrNotFound)
}

func (f *fakeRepositoryStore) List(_ context.Context) ([]domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Repository, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (f *fakeRepositoryStore) Update(_ context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository: %w", port.ErrNotFound)
	}
	r.Name = name
	r.Description = description
	if verifyCommand != nil {
		r.VerifyCommand = *verifyCommand
	}
	if buildCommand != nil {
		r.BuildCommand = *buildCommand
	}
	if testCommand != nil {
		r.TestCommand = *testCommand
	}
	if requireHumanReview != nil {
		r.RequireHumanReview = *requireHumanReview
	}
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateMeta(_ context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository meta: %w", port.ErrNotFound)
	}
	if kind != nil {
		r.Kind = *kind
	}
	if subRepoKinds != nil {
		r.SubRepoKinds = *subRepoKinds
	}
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateMobilePlatform(_ context.Context, id uuid.UUID, platform string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository mobile platform: %w", port.ErrNotFound)
	}
	r.MobilePlatform = platform
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateReleaseEngine(_ context.Context, id uuid.UUID, engine string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository release engine: %w", port.ErrNotFound)
	}
	r.ReleaseEngine = engine
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateDetectedBuildTargets(_ context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository build targets: %w", port.ErrNotFound)
	}
	r.DetectedBuildTargets = targets
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateDetectedAppIdentity(_ context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository app identity: %w", port.ErrNotFound)
	}
	r.DetectedAppIdentity = identity
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateQualityGates(_ context.Context, id uuid.UUID, coverage, mutation domain.QualityGate) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update quality gates: %w", port.ErrNotFound)
	}
	r.RequireOverallCoverage = coverage.Enabled
	r.CoverageThreshold = coverage.Threshold
	r.MutationEnabled = mutation.Enabled
	r.MutationThreshold = mutation.Threshold
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) SetDocsTaskID(_ context.Context, id uuid.UUID, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("set repository docs task: %w", port.ErrNotFound)
	}
	r.DocsTaskID = taskID
	f.rows[id] = r
	return nil
}

func (f *fakeRepositoryStore) UpdateSubProjects(_ context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository sub-projects: %w", port.ErrNotFound)
	}
	r.SubProjects = subProjects
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateIncidentPolicy(_ context.Context, id uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update incident policy: %w", port.ErrNotFound)
	}
	r.IncidentPolicy = policy
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateTestStrategy(_ context.Context, id uuid.UUID, strategy string) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update test strategy: %w", port.ErrNotFound)
	}
	r.TestStrategy = strategy
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) UpdateDocs(_ context.Context, id uuid.UUID, docs domain.RepositoryDocs) (domain.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return domain.Repository{}, fmt.Errorf("update repository docs: %w", port.ErrNotFound)
	}
	r.Docs = docs
	f.rows[id] = r
	return r, nil
}

func (f *fakeRepositoryStore) SetWebhook(_ context.Context, id uuid.UUID, _ string, hookID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("set repository webhook: %w", port.ErrNotFound)
	}
	r.WebhookInstalled = hookID != 0
	f.rows[id] = r
	return nil
}

func (f *fakeRepositoryStore) WebhookSecret(_ context.Context, id uuid.UUID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[id]; !ok {
		return "", fmt.Errorf("get webhook secret: %w", port.ErrNotFound)
	}
	return "", nil
}

func (f *fakeRepositoryStore) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeRepositoryStore) SetProjects(_ context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[repositoryID]
	if !ok {
		return fmt.Errorf("set projects: %w", port.ErrNotFound)
	}
	r.ProjectIDs = projectIDs
	f.rows[repositoryID] = r
	return nil
}

func (f *fakeRepositoryStore) ListProjectIDs(_ context.Context, repositoryID uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[repositoryID]
	if !ok {
		return nil, fmt.Errorf("list project ids: %w", port.ErrNotFound)
	}
	return r.ProjectIDs, nil
}

func (f *fakeRepositoryStore) ListProjectIDsByRepositories(_ context.Context, repositoryIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[uuid.UUID][]uuid.UUID, len(repositoryIDs))
	for _, id := range repositoryIDs {
		if r, ok := f.rows[id]; ok {
			out[id] = r.ProjectIDs
		}
	}
	return out, nil
}

type fakeRepositoryPipelineJobStore struct {
	mu   sync.Mutex
	rows []domain.RepositoryPipelineJob
}

var _ port.RepositoryPipelineJobStore = (*fakeRepositoryPipelineJobStore)(nil)

func newFakeRepositoryPipelineJobStore(rows []domain.RepositoryPipelineJob) *fakeRepositoryPipelineJobStore {
	return &fakeRepositoryPipelineJobStore{rows: append([]domain.RepositoryPipelineJob(nil), rows...)}
}

func (f *fakeRepositoryPipelineJobStore) ListByRepository(_ context.Context, repositoryID uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.RepositoryPipelineJob
	for _, j := range f.rows {
		if j.RepositoryID == repositoryID {
			out = append(out, j)
		}
	}
	return out, nil
}

func (f *fakeRepositoryPipelineJobStore) ListAll(_ context.Context) ([]domain.RepositoryPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.RepositoryPipelineJob(nil), f.rows...), nil
}

func (f *fakeRepositoryPipelineJobStore) ReplaceForRepository(_ context.Context, repositoryID uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []domain.RepositoryPipelineJob
	for _, j := range f.rows {
		if j.RepositoryID != repositoryID {
			kept = append(kept, j)
		}
	}
	f.rows = append(kept, jobs...)
	return jobs, nil
}

type fakeActionsClient struct {
	mu sync.Mutex

	ListRunsForCommitResult  []port.ActionsRun
	ListRunsForCommitErr     error
	ListRunsForCommitCalls   []listRunsForCommitCall
	ListRunJobsResult        map[int64][]port.ActionsJob
	ListRunJobsErr           error
	JobLogsResult            map[int64]string
	JobLogsErr               error
	CommitDeployStatusResult port.CommitDeploySignal
	CommitDeployStatusErr    error

	ListWorkflowRunsResult []port.ActionsRun
	ListWorkflowRunsErr    error
	DispatchWorkflowErr    error
	CreateTagErr           error

	ListWorkflowRunsCalls []listWorkflowRunsCall
	DispatchWorkflowCalls []dispatchWorkflowCall
	CreateTagCalls        []createTagCall

	dispatches int
	lastRef    string
	tags       int
	lastTag    string
	lastTagSHA string
}

type listWorkflowRunsCall struct {
	Owner, Repo, WorkflowFile, Branch string
}

type dispatchWorkflowCall struct {
	Owner, Repo, WorkflowFile, Ref string
}

type createTagCall struct {
	Owner, Repo, Tag, SHA string
}

var _ port.ActionsClient = (*fakeActionsClient)(nil)

func (f *fakeActionsClient) ListWorkflowRuns(_ context.Context, owner, repo, workflowFile, branch string) ([]port.ActionsRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListWorkflowRunsCalls = append(f.ListWorkflowRunsCalls, listWorkflowRunsCall{owner, repo, workflowFile, branch})
	return f.ListWorkflowRunsResult, f.ListWorkflowRunsErr
}

func (f *fakeActionsClient) DispatchWorkflow(_ context.Context, owner, repo, workflowFile, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DispatchWorkflowCalls = append(f.DispatchWorkflowCalls, dispatchWorkflowCall{owner, repo, workflowFile, ref})
	f.dispatches++
	f.lastRef = ref
	return f.DispatchWorkflowErr
}

func (f *fakeActionsClient) CreateTag(_ context.Context, owner, repo, tag, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateTagCalls = append(f.CreateTagCalls, createTagCall{owner, repo, tag, sha})
	f.tags++
	f.lastTag = tag
	f.lastTagSHA = sha
	return f.CreateTagErr
}

type fixture struct {
	repos      []domain.Repository
	targets    []domain.DeployTarget
	jobs       []domain.RepositoryPipelineJob
	runs       []domain.DeploymentRun
	dispatches []domain.DeployDispatch
	audit      []domain.OpsAuditEntry

	dispatchErr  error
	createTagErr error

	actionsRuns []port.ActionsRun
}

func newTestService(t *testing.T, f fixture) *deployops.Service {
	t.Helper()
	svc, _ := newTestServiceWithActions(t, f)
	return svc
}

func newTestServiceWithActions(t *testing.T, f fixture) (*deployops.Service, *fakeActionsClient) {
	t.Helper()
	actions := &fakeActionsClient{
		ListWorkflowRunsResult: f.actionsRuns,
		DispatchWorkflowErr:    f.dispatchErr,
		CreateTagErr:           f.createTagErr,
	}
	svc := deployops.New(
		newFakeDeploymentRunStore(f.runs),
		newFakeDeployDispatchStore(f.dispatches),
		newFakeOpsAuditStore(f.audit),
		newFakeDeployTargetStore(f.targets),
		newFakeRepositoryStore(f.repos),
		newFakeRepositoryPipelineJobStore(f.jobs),
		actions,
	)
	return svc, actions
}

type fakeIncidentIngester struct {
	mu    sync.Mutex
	count int
	last  domain.IncidentInput
}

func (f *fakeIncidentIngester) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	f.last = in
	return domain.Incident{}, nil
}

type monitorFakes struct {
	runs       *fakeDeploymentRunStore
	dispatches *fakeDeployDispatchStore
	actions    *fakeActionsClient
	ingester   *fakeIncidentIngester
}

func newTestMonitor(t *testing.T, f fixture) (*deployops.Monitor, monitorFakes) {
	t.Helper()
	runsStore := newFakeDeploymentRunStore(f.runs)
	dispatchesStore := newFakeDeployDispatchStore(f.dispatches)
	actions := &fakeActionsClient{
		ListWorkflowRunsResult: f.actionsRuns,
		DispatchWorkflowErr:    f.dispatchErr,
		CreateTagErr:           f.createTagErr,
	}
	svc := deployops.New(
		runsStore,
		dispatchesStore,
		newFakeOpsAuditStore(f.audit),
		newFakeDeployTargetStore(f.targets),
		newFakeRepositoryStore(f.repos),
		newFakeRepositoryPipelineJobStore(f.jobs),
		actions,
	)
	svc.SetRepoResolver(func(_ context.Context, repo domain.Repository) (string, string, error) {
		return "owner", repo.Name, nil
	})
	ingester := &fakeIncidentIngester{}
	monitor := deployops.NewMonitor(svc, ingester)
	return monitor, monitorFakes{runs: runsStore, dispatches: dispatchesStore, actions: actions, ingester: ingester}
}

func (f *fakeActionsClient) ListRunsForCommit(_ context.Context, owner, repo, sha string) ([]port.ActionsRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListRunsForCommitCalls = append(f.ListRunsForCommitCalls, listRunsForCommitCall{owner, repo, sha})
	return f.ListRunsForCommitResult, f.ListRunsForCommitErr
}

func (f *fakeActionsClient) ListRunJobs(_ context.Context, _, _ string, runID int64) ([]port.ActionsJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ListRunJobsResult[runID], f.ListRunJobsErr
}

func (f *fakeActionsClient) JobLogs(_ context.Context, _, _ string, jobID int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.JobLogsResult[jobID], f.JobLogsErr
}

func (f *fakeActionsClient) CommitDeployStatus(_ context.Context, _, _, _ string) (port.CommitDeploySignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.CommitDeployStatusResult, f.CommitDeployStatusErr
}

type listRunsForCommitCall struct{ owner, repo, sha string }
