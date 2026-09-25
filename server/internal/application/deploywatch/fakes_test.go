package deploywatch_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeTasks struct {
	byID       map[uuid.UUID]domain.BoardTask
	byMergeSHA map[string]domain.BoardTask
	getErr     error
}

func newFakeTasks(tasks ...domain.BoardTask) *fakeTasks {
	f := &fakeTasks{byID: map[uuid.UUID]domain.BoardTask{}, byMergeSHA: map[string]domain.BoardTask{}}
	for _, t := range tasks {
		f.byID[t.ID] = t
		if t.MergeCommitSHA != "" {
			f.byMergeSHA[t.MergeCommitSHA] = t
		}
	}
	return f
}

func (f *fakeTasks) Get(_ context.Context, _, taskID uuid.UUID) (domain.BoardTask, error) {
	if f.getErr != nil {
		return domain.BoardTask{}, f.getErr
	}
	task, ok := f.byID[taskID]
	if !ok {
		return domain.BoardTask{}, port.ErrNotFound
	}
	return task, nil
}

func (f *fakeTasks) FindTaskByMergeCommit(_ context.Context, _ uuid.UUID, sha string) (domain.BoardTask, error) {
	task, ok := f.byMergeSHA[sha]
	if !ok {
		return domain.BoardTask{}, port.ErrNotFound
	}
	return task, nil
}

type fakeComments struct {
	mu    sync.Mutex
	posts []string
}

func (f *fakeComments) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, req.Content)
	return domain.TaskComment{}, nil
}

func (f *fakeComments) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := ""
	for _, p := range f.posts {
		out += p + "\n"
	}
	return out
}

type fakeTargets struct {
	byEnv map[string]domain.DeployTarget
}

func (f *fakeTargets) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeTargets) ListAll(context.Context) ([]domain.DeployTarget, error) { return nil, nil }
func (f *fakeTargets) Get(_ context.Context, _ uuid.UUID, _, env string) (domain.DeployTarget, error) {
	t, ok := f.byEnv[env]
	if !ok {
		return domain.DeployTarget{}, fmt.Errorf("get deploy target: %w", port.ErrNotFound)
	}
	return t, nil
}
func (f *fakeTargets) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeTargets) Delete(context.Context, uuid.UUID, string, string) error { return nil }

type fakeRepos struct{ repo domain.Repository }

func (f *fakeRepos) Get(context.Context, uuid.UUID) (domain.Repository, error) { return f.repo, nil }

type fakeRuns struct {
	byEnv  map[string][]domain.DeploymentRun
	getErr error
}

func (f *fakeRuns) Upsert(_ context.Context, r domain.DeploymentRun) (domain.DeploymentRun, error) {
	return r, nil
}
func (f *fakeRuns) Latest(_ context.Context, _ uuid.UUID, env string) (domain.DeploymentRun, error) {
	if f.getErr != nil {
		return domain.DeploymentRun{}, f.getErr
	}
	runs := f.byEnv[env]
	if len(runs) == 0 {
		return domain.DeploymentRun{}, fmt.Errorf("latest: %w", port.ErrNotFound)
	}
	return runs[0], nil
}
func (f *fakeRuns) ByRunID(_ context.Context, _ uuid.UUID, runID int64) (domain.DeploymentRun, error) {
	for _, runs := range f.byEnv {
		for _, r := range runs {
			if r.RunID == runID {
				return r, nil
			}
		}
	}
	return domain.DeploymentRun{}, port.ErrNotFound
}

func (f *fakeRuns) LatestAll(context.Context) ([]domain.DeploymentRun, error) { return nil, nil }
func (f *fakeRuns) ListByEnv(_ context.Context, _ uuid.UUID, env string, _ int) ([]domain.DeploymentRun, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.byEnv[env], nil
}
func (f *fakeRuns) LastSuccessfulBefore(context.Context, uuid.UUID, string, string) (domain.DeploymentRun, error) {
	return domain.DeploymentRun{}, fmt.Errorf("last successful: %w", port.ErrNotFound)
}
func (f *fakeRuns) Stamp(context.Context, uuid.UUID, int64, string, string, string) error { return nil }

type fakePipelineJobs struct {
	jobs []domain.RepositoryPipelineJob
	err  error
}

func (f *fakePipelineJobs) ListByRepository(context.Context, uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	return f.jobs, f.err
}
func (f *fakePipelineJobs) ListAll(context.Context) ([]domain.RepositoryPipelineJob, error) {
	return f.jobs, nil
}
func (f *fakePipelineJobs) ReplaceForRepository(context.Context, uuid.UUID, []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	return nil, nil
}

type fakeActions struct {
	runsForCommit     []port.ActionsRun
	runsErr           error
	jobsByRun         map[int64][]port.ActionsJob
	jobsErr           error
	logs              map[int64]string
	commitSignal      port.CommitDeploySignal
	commitErr         error
	commitCalls       int
	dispatchedRefs    []string
	workflowRuns      map[string][]port.ActionsRun
	workflowRunsErr   error
	workflowRunsCalls []string
}

func (f *fakeActions) ListWorkflowRuns(_ context.Context, _, _, workflowFile, _ string) ([]port.ActionsRun, error) {
	f.workflowRunsCalls = append(f.workflowRunsCalls, workflowFile)
	if f.workflowRunsErr != nil {
		return nil, f.workflowRunsErr
	}
	return f.workflowRuns[workflowFile], nil
}
func (f *fakeActions) DispatchWorkflow(_ context.Context, _, _, _, ref string) error {
	f.dispatchedRefs = append(f.dispatchedRefs, ref)
	return nil
}
func (f *fakeActions) CreateTag(context.Context, string, string, string, string) error { return nil }
func (f *fakeActions) ListRunsForCommit(context.Context, string, string, string) ([]port.ActionsRun, error) {
	return f.runsForCommit, f.runsErr
}
func (f *fakeActions) ListRunJobs(_ context.Context, _, _ string, runID int64) ([]port.ActionsJob, error) {
	return f.jobsByRun[runID], f.jobsErr
}
func (f *fakeActions) JobLogs(_ context.Context, _, _ string, jobID int64) (string, error) {
	return f.logs[jobID], nil
}
func (f *fakeActions) CommitDeployStatus(context.Context, string, string, string) (port.CommitDeploySignal, error) {
	f.commitCalls++
	return f.commitSignal, f.commitErr
}

type fakeRollbacker struct {
	dispatch domain.DeployDispatch
	err      error
	calls    []deployops.AgentRollbackAuthorization
	inputs   []deployops.RollbackInput
}

func (f *fakeRollbacker) RollbackForTask(_ context.Context, in deployops.RollbackInput, auth deployops.AgentRollbackAuthorization) (domain.DeployDispatch, error) {
	f.inputs = append(f.inputs, in)
	f.calls = append(f.calls, auth)
	return f.dispatch, f.err
}

type fakeGit struct {
	hasGit    bool
	revertSHA string
	err       error
	reverted  []string
}

func (f *fakeGit) HasGit(string) bool { return f.hasGit }
func (f *fakeGit) RevertOnDefaultBranch(_ context.Context, _ string, shas []string, _ string) (string, error) {
	f.reverted = append(f.reverted, shas...)
	if f.err != nil {
		return "", f.err
	}
	return f.revertSHA, nil
}

type fakeIncidents struct {
	mu      sync.Mutex
	ingests []domain.IncidentInput
}

func (f *fakeIncidents) Ingest(_ context.Context, in domain.IncidentInput) (domain.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingests = append(f.ingests, in)
	return domain.Incident{ID: uuid.New()}, nil
}

var errNoWorkflow = fmt.Errorf("deployops: %w", deployops.ErrNoWorkflowMapping)

var _ = errors.Is

func actionRuns(rs ...port.ActionsRun) []port.ActionsRun { return rs }

func actionRun(id int64, htmlURL string) port.ActionsRun {
	return port.ActionsRun{ID: id, HTMLURL: htmlURL}
}

// deployRun builds a run carrying the fields the whole-run rule and the
// workflow-filtered lookup need: a head sha to match the release commit
// against, and a status/conclusion to fall back on when no job in the run
// matches by name.
func deployRun(id int64, headSHA, status, conclusion string) port.ActionsRun {
	return port.ActionsRun{ID: id, HeadSHA: headSHA, Status: status, Conclusion: conclusion}
}

func jobs(js ...port.ActionsJob) []port.ActionsJob { return js }

func job(id int64, name, status, conclusion string) port.ActionsJob {
	return port.ActionsJob{ID: id, Name: name, Status: status, Conclusion: conclusion}
}

func commitSignal(kind, state, ctxName string) port.CommitDeploySignal {
	sig := port.CommitDeploySignal{Kind: kind, State: state}
	if ctxName != "" {
		sig.Contexts = []string{ctxName}
	}
	return sig
}

// deployRunAt is deployRun plus RunStartedAt, for StatusForCommitSince tests
// : the since-narrowed watch only counts runs started at or after a
// given time, which deployRun's fixed zero-value RunStartedAt cannot exercise.
func deployRunAt(id int64, headSHA, status, conclusion string, startedAt time.Time) port.ActionsRun {
	r := deployRun(id, headSHA, status, conclusion)
	r.RunStartedAt = startedAt
	return r
}
