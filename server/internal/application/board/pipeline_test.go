package board

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakePipelineStore struct {
	mu         sync.Mutex
	pipelines  map[uuid.UUID]domain.TaskPipeline
	jobs       map[uuid.UUID][]domain.TaskPipelineJob
	seq        []string
	superseded int
}

func newFakePipelineStore() *fakePipelineStore {
	return &fakePipelineStore{
		pipelines: map[uuid.UUID]domain.TaskPipeline{},
		jobs:      map[uuid.UUID][]domain.TaskPipelineJob{},
	}
}

func (f *fakePipelineStore) Create(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p.ID = uuid.New()
	if p.Status == "" {
		p.Status = domain.PipelineStatusPending
	}
	f.pipelines[p.ID] = p
	f.seq = append(f.seq, "create")
	return p, nil
}

func (f *fakePipelineStore) Update(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pipelines[p.ID] = p
	return p, nil
}

func (f *fakePipelineStore) ClaimTerminal(_ context.Context, p domain.TaskPipeline) (domain.TaskPipeline, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.pipelines[p.ID]
	if ok && current.Status != domain.PipelineStatusPending && current.Status != domain.PipelineStatusRunning {
		return domain.TaskPipeline{}, false, nil
	}
	f.pipelines[p.ID] = p
	return p, true, nil
}

func (f *fakePipelineStore) Get(_ context.Context, id uuid.UUID) (domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pipelines[id]
	if !ok {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	return p, nil
}

func (f *fakePipelineStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.TaskPipeline
	for _, p := range f.pipelines {
		if p.TaskID == taskID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakePipelineStore) LatestByTask(_ context.Context, taskID uuid.UUID) (domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest domain.TaskPipeline
	found := false
	for _, p := range f.pipelines {
		if p.TaskID != taskID {
			continue
		}
		if !found || p.CreatedAt.After(latest.CreatedAt) {
			latest = p
			found = true
		}
	}
	if !found {
		return domain.TaskPipeline{}, domain.ErrPipelineNotFound
	}
	return latest, nil
}

func (f *fakePipelineStore) LatestStatusByTasks(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]domain.TaskPipelineDigest, error) {
	return map[uuid.UUID]domain.TaskPipelineDigest{}, nil
}

func (f *fakePipelineStore) SupersedePending(_ context.Context, taskID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, p := range f.pipelines {
		if p.TaskID == taskID && p.Status == domain.PipelineStatusPending {
			p.Status = domain.PipelineStatusFailed
			p.Note = "superseded"
			f.pipelines[id] = p
			f.superseded++
		}
	}
	f.seq = append(f.seq, "supersede")
	return nil
}

func (f *fakePipelineStore) FailStaleRunning(_ context.Context, _ int) error { return nil }

func (f *fakePipelineStore) ListUnfinished(_ context.Context, limit int) ([]domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unfinishedLocked(limit, nil), nil
}

func (f *fakePipelineStore) ListUnfinishedByHeadSHA(_ context.Context, repositoryID uuid.UUID, headSHA string) ([]domain.TaskPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	match := func(p domain.TaskPipeline) bool {
		return p.RepositoryID == repositoryID && p.HeadSHA == headSHA
	}
	return f.unfinishedLocked(0, match), nil
}

func (f *fakePipelineStore) unfinishedLocked(limit int, keep func(domain.TaskPipeline) bool) []domain.TaskPipeline {
	var out []domain.TaskPipeline
	for _, p := range f.pipelines {
		if p.Status != domain.PipelineStatusPending && p.Status != domain.PipelineStatusRunning {
			continue
		}
		if keep != nil && !keep(p) {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakePipelineStore) CreateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j.ID = uuid.New()
	f.jobs[j.PipelineID] = append(f.jobs[j.PipelineID], j)
	return j, nil
}

func (f *fakePipelineStore) UpdateJob(_ context.Context, j domain.TaskPipelineJob) (domain.TaskPipelineJob, error) {
	return j, nil
}

func (f *fakePipelineStore) ListJobs(_ context.Context, pipelineID uuid.UUID) ([]domain.TaskPipelineJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[pipelineID], nil
}

func newRunnerForTrigger(store *fakePipelineStore) *PipelineRunner {
	return NewPipelineRunner(PipelineRunnerDeps{Store: store})
}

func TestTriggerSupersedesPending(t *testing.T) {
	store := newFakePipelineStore()
	runner := newRunnerForTrigger(store)
	task := domain.BoardTask{ID: uuid.New()}

	if _, err := runner.Trigger(context.Background(), uuid.New(), task, domain.PipelineTriggerReadyForQA); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if _, err := runner.Trigger(context.Background(), uuid.New(), task, domain.PipelineTriggerReadyForQA); err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if store.superseded == 0 {
		t.Errorf("expected the pending pipeline to be superseded")
	}
}

func TestTriggerManualRejectedWhileRunning(t *testing.T) {
	store := newFakePipelineStore()
	runner := newRunnerForTrigger(store)
	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New()}

	created, err := runner.Trigger(context.Background(), repoID, task, domain.PipelineTriggerReadyForQA)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	created.Status = domain.PipelineStatusRunning
	_, _ = store.Update(context.Background(), created)

	if _, err := runner.Trigger(context.Background(), repoID, task, domain.PipelineTriggerManual); err != domain.ErrPipelineActive {
		t.Errorf("expected ErrPipelineActive for manual trigger while running, got %v", err)
	}
}

func TestFilterMappings(t *testing.T) {
	repoID := uuid.New()
	mappings := []domain.RepositoryPipelineJob{
		{RepositoryID: repoID, Category: domain.PipelineCategoryBuild, TargetKind: domain.PipelineTargetJob, TargetRef: "build"},
		{RepositoryID: repoID, SubRepoKind: "backend", Category: domain.PipelineCategoryTest, TargetKind: domain.PipelineTargetJob, TargetRef: "backend-test"},
		{RepositoryID: repoID, Category: domain.PipelineCategoryStageDeploy, TargetKind: domain.PipelineTargetWorkflow, TargetRef: "deploy.yml"},
		{RepositoryID: repoID, Category: domain.PipelineCategoryTest, TargetKind: domain.PipelineTargetJob, TargetRef: ""},
	}
	jobTargets := filterMappings(mappings, domain.PipelineTargetJob,
		domain.PipelineCategoryValidate, domain.PipelineCategoryBuild, domain.PipelineCategoryTest)
	if len(jobTargets) != 2 {
		t.Fatalf("expected 2 job targets, got %d (%v)", len(jobTargets), jobTargets)
	}
	if jobTargets[1].label != "backend:test" {
		t.Errorf("expected sub-repo label backend:test, got %q", jobTargets[1].label)
	}
	deployTargets := filterMappings(mappings, domain.PipelineTargetWorkflow, domain.PipelineCategoryStageDeploy)
	if len(deployTargets) != 1 || deployTargets[0].ref != "deploy.yml" {
		t.Errorf("expected one stage deploy workflow deploy.yml, got %v", deployTargets)
	}
}

func TestFoldRunJobs(t *testing.T) {
	inProgress := foldRunJobs("deploy.yml", githubapi.WorkflowRun{}, []githubapi.RunJob{{Name: "a", Status: "in_progress"}})
	if inProgress.Status != "in_progress" {
		t.Errorf("any in-progress job → in_progress, got %q", inProgress.Status)
	}
	failed := foldRunJobs("deploy.yml", githubapi.WorkflowRun{}, []githubapi.RunJob{
		{ID: 1, Name: "a", Status: "completed", Conclusion: "success"},
		{ID: 2, Name: "b", Status: "completed", Conclusion: "failure"},
	})
	if failed.Conclusion != "failure" || failed.ID != 2 {
		t.Errorf("any failed job → failure surfacing that job, got %+v", failed)
	}
	ok := foldRunJobs("deploy.yml", githubapi.WorkflowRun{}, []githubapi.RunJob{
		{Name: "a", Status: "completed", Conclusion: "success"},
	})
	if ok.Conclusion != "success" {
		t.Errorf("all success → success, got %q", ok.Conclusion)
	}
}

func TestMergeJobPrefersCompleted(t *testing.T) {
	byName := map[string]githubapi.RunJob{}
	mergeJob(byName, githubapi.RunJob{Name: "build", Status: "in_progress"})
	mergeJob(byName, githubapi.RunJob{Name: "build", Status: "completed", Conclusion: "success"})
	if byName["build"].Status != "completed" {
		t.Errorf("completed job should win over in-progress, got %q", byName["build"].Status)
	}
	mergeJob(byName, githubapi.RunJob{Name: "build", Status: "in_progress"})
	if byName["build"].Status != "completed" {
		t.Errorf("in-progress must not clobber completed, got %q", byName["build"].Status)
	}
}

func TestEvaluate(t *testing.T) {
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore()})
	pipelineID := uuid.New()
	targets := []mappingTarget{{label: "build", ref: "build"}, {label: "test", ref: "test"}}

	byName := map[string]githubapi.RunJob{"build": {Name: "build", Status: "completed", Conclusion: "success"}}
	if done, _, _ := runner.evaluate(context.Background(), pipelineID, domain.TaskGitInfo{}, "", targets, byName, false); done {
		t.Errorf("expected not-done while a target is still missing")
	}

	byName = map[string]githubapi.RunJob{
		"build": {Name: "build", Status: "completed", Conclusion: "success"},
		"test":  {Name: "test", Status: "completed", Conclusion: "success"},
	}
	done, jobs, status := runner.evaluate(context.Background(), pipelineID, domain.TaskGitInfo{}, "", targets, byName, false)
	if !done || status != domain.PipelineStatusSuccess || len(jobs) != 2 {
		t.Errorf("all-success expected done+success+2 jobs, got done=%v status=%v jobs=%d", done, status, len(jobs))
	}

	byName = map[string]githubapi.RunJob{"build": {Name: "build", Status: "completed", Conclusion: "success"}}
	done, _, status = runner.evaluate(context.Background(), pipelineID, domain.TaskGitInfo{}, "", targets, byName, true)
	if !done || status != domain.PipelineStatusFailed {
		t.Errorf("timeout with missing target expected done+failed, got done=%v status=%v", done, status)
	}
}

type fakeDeployTargets struct {
	target domain.DeployTarget
	err    error
	envs   []string
}

func (f *fakeDeployTargets) Get(_ context.Context, _ uuid.UUID, _, env string) (domain.DeployTarget, error) {
	f.envs = append(f.envs, env)
	if f.err != nil {
		return domain.DeployTarget{}, f.err
	}
	return f.target, nil
}

func (f *fakeDeployTargets) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fakeDeployTargets) ListAll(context.Context) ([]domain.DeployTarget, error) { return nil, nil }
func (f *fakeDeployTargets) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fakeDeployTargets) Delete(context.Context, uuid.UUID, string, string) error { return nil }

type storeSubmitCall struct {
	repositoryID uuid.UUID
	platform     string
	version      string
}

type fakeStoreSubmitter struct {
	calls []storeSubmitCall
}

func (f *fakeStoreSubmitter) MarkSubmitted(_ context.Context, repositoryID uuid.UUID, platform, version string) error {
	f.calls = append(f.calls, storeSubmitCall{repositoryID, platform, version})
	return nil
}

func finalizeProdDeploy(t *testing.T, provider string, jobs []domain.TaskPipelineJob) *fakeStoreSubmitter {
	t.Helper()
	store := newFakePipelineStore()
	targets := &fakeDeployTargets{target: domain.DeployTarget{Env: domain.DeployEnvProd, Provider: provider}}
	submitter := &fakeStoreSubmitter{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: store, DeployTargets: targets})
	runner.SetStoreSubmitter(submitter)

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New()}
	pipeline, err := store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerProdDeploy,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	for i := range jobs {
		jobs[i].PipelineID = pipeline.ID
	}

	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}
	if err := runner.finalize(context.Background(), job, pipeline, domain.PipelineStatusSuccess, jobs); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	for i := range submitter.calls {
		if submitter.calls[i].repositoryID != repoID {
			t.Errorf("call %d carried repository %s, want %s", i, submitter.calls[i].repositoryID, repoID)
		}
	}
	return submitter
}

func TestProdDeploySuccessMarksAStoreTargetSubmitted(t *testing.T) {
	ran := []domain.TaskPipelineJob{{Name: "prod_deploy", Status: domain.PipelineJobStatusSuccess}}

	submitter := finalizeProdDeploy(t, domain.DeployProviderAppStore, ran)
	if len(submitter.calls) != 1 {
		t.Fatalf("got %d MarkSubmitted calls, want 1", len(submitter.calls))
	}
	if submitter.calls[0].platform != domain.MobileStorePlatformIOS {
		t.Errorf("platform = %q, want ios", submitter.calls[0].platform)
	}

	submitter = finalizeProdDeploy(t, domain.DeployProviderGooglePlay, ran)
	if len(submitter.calls) != 1 {
		t.Fatalf("got %d MarkSubmitted calls for google play, want 1", len(submitter.calls))
	}
	if submitter.calls[0].platform != domain.MobileStorePlatformAndroid {
		t.Errorf("platform = %q, want android", submitter.calls[0].platform)
	}
}

func TestProdDeploySuccessIgnoresNonStoreTargets(t *testing.T) {
	submitter := finalizeProdDeploy(t, domain.DeployProviderGCPCloudRun,
		[]domain.TaskPipelineJob{{Name: "prod_deploy", Status: domain.PipelineJobStatusSuccess}})
	if len(submitter.calls) != 0 {
		t.Fatalf("got %d MarkSubmitted calls for a cloud run target, want 0", len(submitter.calls))
	}
}

func TestProdDeploySkipDoesNotMarkSubmitted(t *testing.T) {
	submitter := finalizeProdDeploy(t, domain.DeployProviderAppStore,
		[]domain.TaskPipelineJob{{Name: "no prod_deploy workflow configured", Status: domain.PipelineJobStatusSkipped}})
	if len(submitter.calls) != 0 {
		t.Fatalf("got %d MarkSubmitted calls for a skipped deploy, want 0", len(submitter.calls))
	}
}

func TestProdDeploySkipReleasesWithAWarningComment(t *testing.T) {
	tasks := &fakeTaskUpdater{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore(), Tasks: tasks})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnDone}
	pipeline, err := runner.store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerProdDeploy,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobs := []domain.TaskPipelineJob{{
		PipelineID: pipeline.ID,
		Name:       "no prod_deploy workflow configured",
		Status:     domain.PipelineJobStatusSkipped,
	}}

	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}
	if err := runner.finalize(context.Background(), job, pipeline, domain.PipelineStatusSkipped, jobs); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	if len(tasks.calls) != 1 || tasks.calls[0].Column == nil || *tasks.calls[0].Column != domain.TaskColumnReleased {
		t.Fatalf("a skipped prod deploy must still release the task, got %+v", tasks.calls)
	}
	if len(tasks.comments) != 1 {
		t.Fatalf("comments = %d, want 1 — the skip has to reach the card", len(tasks.comments))
	}
	content := tasks.comments[0].Content
	if !strings.Contains(content, "no prod_deploy workflow configured") {
		t.Errorf("comment does not name the missing mapping: %q", content)
	}
	if !strings.Contains(content, "without a verified deploy") {
		t.Errorf("comment does not say the deploy was not verified: %q", content)
	}
}

func TestProdDeploySuccessReleasesWithoutAWarningComment(t *testing.T) {
	tasks := &fakeTaskUpdater{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore(), Tasks: tasks})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnDone}
	pipeline, err := runner.store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerProdDeploy,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobs := []domain.TaskPipelineJob{{
		PipelineID: pipeline.ID,
		Name:       "prod_deploy",
		Status:     domain.PipelineJobStatusSuccess,
	}}

	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}
	if err := runner.finalize(context.Background(), job, pipeline, domain.PipelineStatusSuccess, jobs); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	if len(tasks.calls) != 1 || tasks.calls[0].Column == nil || *tasks.calls[0].Column != domain.TaskColumnReleased {
		t.Fatalf("a successful prod deploy must release the task, got %+v", tasks.calls)
	}
	if len(tasks.comments) != 0 {
		t.Fatalf("comments = %d, want 0 — a real deploy adds no skip warning: %+v", len(tasks.comments), tasks.comments)
	}
}

func TestPreProdDeploySkipFallsThroughToReleaseWithAWarningComment(t *testing.T) {
	tasks := &fakeTaskUpdater{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore(), Tasks: tasks})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnDone}
	pipeline, err := runner.store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerPreProdDeploy,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobs := []domain.TaskPipelineJob{{
		PipelineID: pipeline.ID,
		Name:       "no preprod_deploy workflow configured",
		Status:     domain.PipelineJobStatusSkipped,
	}}

	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}
	if err := runner.finalize(context.Background(), job, pipeline, domain.PipelineStatusSkipped, jobs); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	if len(tasks.calls) != 1 || tasks.calls[0].Column == nil || *tasks.calls[0].Column != domain.TaskColumnReleased {
		t.Fatalf("a skipped preprod deploy with no prod mapping must still release the task, got %+v", tasks.calls)
	}
	if len(tasks.comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(tasks.comments))
	}
	content := tasks.comments[0].Content
	if !strings.Contains(content, "no preprod_deploy workflow configured") {
		t.Errorf("comment does not name the missing mapping: %q", content)
	}
	if !strings.Contains(content, "without a verified deploy") {
		t.Errorf("comment does not say the deploy was not verified: %q", content)
	}
}

type fakeQADispatcher struct {
	calls       []uuid.UUID
	gateReasons []string
}

func (f *fakeQADispatcher) DispatchQA(_ context.Context, _ uuid.UUID, _ domain.BoardTask, pipelineID uuid.UUID, gateReason string) error {
	f.calls = append(f.calls, pipelineID)
	f.gateReasons = append(f.gateReasons, gateReason)
	return nil
}

func TestNoChecksConfiguredFinishesSkippedAndStillOpensTheGate(t *testing.T) {
	store := newFakePipelineStore()
	qa := &fakeQADispatcher{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: store, QA: qa})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New()}
	pipeline, err := store.Create(context.Background(), domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID, Trigger: domain.PipelineTriggerReadyForQA,
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	job := pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}
	if err := runner.finishNoChecks(context.Background(), job, pipeline, "no checks configured"); err != nil {
		t.Fatalf("finishNoChecks: %v", err)
	}

	stored, err := store.Get(context.Background(), pipeline.ID)
	if err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	if stored.Status != domain.PipelineStatusSkipped {
		t.Errorf("status = %q, want skipped", stored.Status)
	}
	if stored.Provider != domain.PipelineProviderNone {
		t.Errorf("provider = %q, want none", stored.Provider)
	}
	if len(qa.calls) != 1 {
		t.Fatalf("got %d QA hand-offs, want 1 — a skipped run must still open the gate", len(qa.calls))
	}
}

func TestPipelineStatusOpensGate(t *testing.T) {
	for status, want := range map[domain.PipelineStatus]bool{
		domain.PipelineStatusSuccess: true,
		domain.PipelineStatusSkipped: true,
		domain.PipelineStatusFailed:  false,
		domain.PipelineStatusRunning: false,
		domain.PipelineStatusPending: false,
	} {
		if got := domain.PipelineStatusOpensGate(status); got != want {
			t.Errorf("PipelineStatusOpensGate(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestBillingBlockedDeployReportsButDoesNotBounceTheTask(t *testing.T) {
	tasks := &fakeTaskUpdater{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore(), Tasks: tasks})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Key: "T-7", Column: domain.TaskColumnDone}
	pipeline := domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID,
		Trigger: domain.PipelineTriggerProdDeploy,
		Status:  domain.PipelineStatusFailed,
		Jobs: []domain.TaskPipelineJob{{
			Name:   "prod_deploy",
			Status: domain.PipelineJobStatusFailed,
			Output: "dispatch failed: github api: 402 The job was not started because recent account payments have failed",
		}},
	}

	runner.reportPipelineFailure(context.Background(), pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}, pipeline)

	if len(tasks.calls) != 0 {
		t.Errorf("the task was moved %d time(s); a billing-blocked deploy must move nothing: %+v", len(tasks.calls), tasks.calls)
	}
	if len(tasks.comments) != 1 {
		t.Fatalf("comments = %d, want 1 — the reason has to reach the card", len(tasks.comments))
	}
	if !strings.Contains(tasks.comments[0].Content, "GitHub Actions is unavailable") {
		t.Errorf("the comment does not name the real cause: %q", tasks.comments[0].Content)
	}
}

func TestFailedDeployStillBouncesToNeedRevision(t *testing.T) {
	tasks := &fakeTaskUpdater{}
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: newFakePipelineStore(), Tasks: tasks})

	repoID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnDone}
	pipeline := domain.TaskPipeline{
		TaskID: task.ID, RepositoryID: repoID,
		Trigger: domain.PipelineTriggerProdDeploy,
		Status:  domain.PipelineStatusFailed,
		Jobs: []domain.TaskPipelineJob{{
			Name:   "prod_deploy",
			Status: domain.PipelineJobStatusFailed,
			Output: "Error: the migration failed to apply",
		}},
	}

	runner.reportPipelineFailure(context.Background(), pipelineJob{Pipeline: pipeline, RepositoryID: repoID, Task: task}, pipeline)

	if len(tasks.calls) != 1 || tasks.calls[0].Column == nil || *tasks.calls[0].Column != domain.TaskColumnNeedRevision {
		t.Fatalf("a genuinely failed deploy must return the task to need_revision, got %+v", tasks.calls)
	}
}

type fakePipelineGit struct {
	ensureCalls int
	pushCalls   int
	gitInfo     domain.TaskGitInfo
}

func (f *fakePipelineGit) TaskGitInfo(context.Context, string) (domain.TaskGitInfo, error) {
	return f.gitInfo, nil
}
func (f *fakePipelineGit) EnsurePullRequest(context.Context, string) (string, error) {
	f.ensureCalls++
	return "https://github.com/acme/repo/pull/1", nil
}
func (f *fakePipelineGit) PushBranch(context.Context, string) error {
	f.pushCalls++
	return nil
}

func newResolveGitInfoJob(t *testing.T, trigger domain.PipelineTrigger) (pipelineJob, string) {
	t.Helper()
	root := t.TempDir()
	taskID := uuid.New()
	dir, err := workspace.TaskDir(root, taskID)
	if err != nil {
		t.Fatalf("workspace.TaskDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir task workspace: %v", err)
	}
	return pipelineJob{
		Pipeline:     domain.TaskPipeline{Trigger: trigger},
		RepositoryID: uuid.New(),
		Task:         domain.BoardTask{ID: taskID, Key: "T-1"},
	}, root
}

// A prod/preprod deploy runs after the merge, whose branch is already
// deleted: ensuring/pushing a PR here would recreate that branch and open a
// stray PR against a merge that already happened.
func TestResolveGitInfoSkipsPullRequestOpsForProdAndPreProdDeploy(t *testing.T) {
	for _, trigger := range []domain.PipelineTrigger{domain.PipelineTriggerProdDeploy, domain.PipelineTriggerPreProdDeploy} {
		t.Run(string(trigger), func(t *testing.T) {
			job, root := newResolveGitInfoJob(t, trigger)
			git := &fakePipelineGit{}
			runner := &PipelineRunner{git: git, workspaceRoot: root}

			if _, err := runner.resolveGitInfo(context.Background(), job); err != nil {
				t.Fatalf("resolveGitInfo: %v", err)
			}
			if git.ensureCalls != 0 || git.pushCalls != 0 {
				t.Fatalf("a %s pipeline must not touch the PR: ensure=%d push=%d", trigger, git.ensureCalls, git.pushCalls)
			}
		})
	}
}

// Every other trigger (QA gate, stage deploy) keeps ensuring the PR exists —
// this is the only behaviour that changed.
func TestResolveGitInfoStillEnsuresPullRequestForNonReleaseDeployTriggers(t *testing.T) {
	for _, trigger := range []domain.PipelineTrigger{domain.PipelineTriggerReadyForQA, domain.PipelineTriggerStageDeploy} {
		t.Run(string(trigger), func(t *testing.T) {
			job, root := newResolveGitInfoJob(t, trigger)
			git := &fakePipelineGit{}
			runner := &PipelineRunner{git: git, workspaceRoot: root}

			if _, err := runner.resolveGitInfo(context.Background(), job); err != nil {
				t.Fatalf("resolveGitInfo: %v", err)
			}
			if git.ensureCalls != 1 {
				t.Fatalf("a %s pipeline must still ensure the PR, got %d calls", trigger, git.ensureCalls)
			}
		})
	}
}

type fakeComponentPaths struct {
	byID map[uuid.UUID]string
	err  error
}

func (f *fakeComponentPaths) ComponentPath(_ context.Context, _, componentID uuid.UUID) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.byID[componentID], nil
}

func TestResolveComponentPathNilForARootTask(t *testing.T) {
	runner := &PipelineRunner{components: &fakeComponentPaths{}}
	if got := runner.resolveComponentPath(context.Background(), uuid.New(), nil); got != nil {
		t.Fatalf("path = %v, want nil — a task with no component scopes nothing", got)
	}
}

func TestResolveComponentPathNilWhenResolverNotWired(t *testing.T) {
	runner := &PipelineRunner{}
	componentID := uuid.New()
	if got := runner.resolveComponentPath(context.Background(), uuid.New(), &componentID); got != nil {
		t.Fatalf("path = %v, want nil — without a resolver wired, every mapping still runs", got)
	}
}

func TestResolveComponentPathNilWhenResolutionFails(t *testing.T) {
	runner := &PipelineRunner{components: &fakeComponentPaths{err: errors.New("boom")}}
	componentID := uuid.New()
	if got := runner.resolveComponentPath(context.Background(), uuid.New(), &componentID); got != nil {
		t.Fatalf("path = %v, want nil on a resolution failure — never silently drop every mapping", got)
	}
}

func TestResolveComponentPathReturnsTheResolvedPath(t *testing.T) {
	componentID := uuid.New()
	runner := &PipelineRunner{components: &fakeComponentPaths{byID: map[uuid.UUID]string{componentID: "apps/api"}}}
	got := runner.resolveComponentPath(context.Background(), uuid.New(), &componentID)
	if got == nil || *got != "apps/api" {
		t.Fatalf("path = %v, want apps/api", got)
	}
}

func TestScopeMappingsToComponent(t *testing.T) {
	mappings := []domain.RepositoryPipelineJob{
		{SubProjectPath: "apps/api", Category: domain.PipelineCategoryBuild, TargetKind: domain.PipelineTargetJob, TargetRef: "api-build"},
		{SubProjectPath: "apps/web", Category: domain.PipelineCategoryBuild, TargetKind: domain.PipelineTargetJob, TargetRef: "web-build"},
		{SubProjectPath: "", Category: domain.PipelineCategoryBuild, TargetKind: domain.PipelineTargetJob, TargetRef: "root-build"},
	}

	if got := scopeMappingsToComponent(mappings, nil); len(got) != 3 {
		t.Fatalf("nil componentPath must keep every mapping, got %d", len(got))
	}

	api := "apps/api"
	scoped := scopeMappingsToComponent(mappings, &api)
	if len(scoped) != 1 || scoped[0].TargetRef != "api-build" {
		t.Fatalf("scoped to apps/api = %+v, want only api-build", scoped)
	}

	root := "."
	scopedRoot := scopeMappingsToComponent(mappings, &root)
	if len(scopedRoot) != 1 || scopedRoot[0].TargetRef != "root-build" {
		t.Fatalf("scoped to \".\" (root) = %+v, want only root-build — \".\" and \"\" mean the same thing", scopedRoot)
	}
}
