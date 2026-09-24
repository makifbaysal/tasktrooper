package board

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type gateTaskStore struct {
	task     domain.BoardTask
	getErr   error
	moves    []domain.TaskColumn
	reasons  []string
	comments []string
}

func (g *gateTaskStore) GetTask(_ context.Context, _, _ uuid.UUID) (domain.BoardTask, error) {
	if g.getErr != nil {
		return domain.BoardTask{}, g.getErr
	}
	return g.task, nil
}

func (g *gateTaskStore) UpdateTask(_ context.Context, _, _ uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	if req.Column != nil {
		g.moves = append(g.moves, *req.Column)
	}
	g.reasons = append(g.reasons, req.SystemReason)
	return g.task, nil
}

func (g *gateTaskStore) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	g.comments = append(g.comments, req.Content)
	return domain.TaskComment{}, nil
}

func (g *gateTaskStore) ListComments(_ context.Context, _, _ uuid.UUID) ([]domain.TaskComment, error) {
	return nil, nil
}

type gateRepos struct {
	repo domain.Repository
	err  error
}

func (g *gateRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error)    { return "", nil }
func (g *gateRepos) ResolveDescription(context.Context, uuid.UUID) (string, error) { return "", nil }
func (g *gateRepos) ResolveRepository(_ context.Context, _ uuid.UUID) (domain.Repository, error) {
	return g.repo, g.err
}

type gateJobStore struct {
	mappings []domain.RepositoryPipelineJob
}

func (g *gateJobStore) ListByRepository(context.Context, uuid.UUID) ([]domain.RepositoryPipelineJob, error) {
	return g.mappings, nil
}

func (g *gateJobStore) ListAll(context.Context) ([]domain.RepositoryPipelineJob, error) {
	return g.mappings, nil
}

func (g *gateJobStore) ReplaceForRepository(_ context.Context, _ uuid.UUID, jobs []domain.RepositoryPipelineJob) ([]domain.RepositoryPipelineJob, error) {
	return jobs, nil
}

type gateHarness struct {
	store    *fakePipelineStore
	qa       *fakeQADispatcher
	tasks    *gateTaskStore
	repos    *gateRepos
	jobs     *gateJobStore
	runner   *PipelineRunner
	repoID   uuid.UUID
	pipeline domain.TaskPipeline
}

func buildJobMapping(repoID uuid.UUID) []domain.RepositoryPipelineJob {
	return []domain.RepositoryPipelineJob{{
		RepositoryID: repoID,
		Category:     domain.PipelineCategoryBuild,
		TargetKind:   domain.PipelineTargetJob,
		TargetRef:    "build",
	}}
}

func newGateHarness(t *testing.T, age time.Duration, mappings []domain.RepositoryPipelineJob) *gateHarness {
	t.Helper()
	repoID := uuid.New()
	taskID := uuid.New()

	h := &gateHarness{
		store: newFakePipelineStore(),
		qa:    &fakeQADispatcher{},
		tasks: &gateTaskStore{task: domain.BoardTask{
			ID: taskID, RepositoryID: repoID, Key: "tt-1",
			Column: domain.TaskColumnCodeReview, TaskType: "task",
		}},
		repos:  &gateRepos{repo: domain.Repository{ID: repoID, RemoteURL: "https://github.com/acme/widgets.git"}},
		jobs:   &gateJobStore{mappings: mappings},
		repoID: repoID,
	}
	h.runner = NewPipelineRunner(PipelineRunnerDeps{
		Store:  h.store,
		Jobs:   h.jobs,
		Repos:  h.repos,
		Tasks:  h.tasks,
		QA:     h.qa,
		Tokens: func(context.Context) (string, error) { return "gh-token", nil },
	})
	h.runner.SetTaskReader(h.tasks)
	h.runner.SetWorkflows(workflowtest.Default().Reader())

	created, err := h.store.Create(context.Background(), domain.TaskPipeline{
		TaskID:       taskID,
		RepositoryID: repoID,
		Trigger:      domain.PipelineTriggerReadyForQA,
		Status:       domain.PipelineStatusRunning,
		HeadSHA:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	created.CreatedAt = time.Now().Add(-age)
	if _, err := h.store.Update(context.Background(), created); err != nil {
		t.Fatalf("age pipeline: %v", err)
	}
	h.pipeline = created
	return h
}

func stubGitHub(t *testing.T, runs []githubapi.WorkflowRun, runsErr error, jobs []githubapi.RunJob) {
	t.Helper()
	prevRuns, prevJobs := gateListRunsByHeadSHA, gateListRunJobs
	gateListRunsByHeadSHA = func(context.Context, string, string, string, string) ([]githubapi.WorkflowRun, error) {
		return runs, runsErr
	}
	gateListRunJobs = func(context.Context, string, string, string, int64) ([]githubapi.RunJob, error) {
		return jobs, nil
	}
	t.Cleanup(func() { gateListRunsByHeadSHA, gateListRunJobs = prevRuns, prevJobs })
}

func (h *gateHarness) settled(t *testing.T) domain.TaskPipeline {
	t.Helper()
	out, err := h.store.Get(context.Background(), h.pipeline.ID)
	if err != nil {
		t.Fatalf("read back pipeline: %v", err)
	}
	return out
}

func (h *gateHarness) assertGateOpened(t *testing.T, wantReason string) {
	t.Helper()
	settled := h.settled(t)
	if settled.Status != domain.PipelineStatusSkipped {
		t.Errorf("status = %q, want skipped: nothing built, so the row must not claim a result", settled.Status)
	}
	if settled.FinishedAt == nil {
		t.Error("the pipeline is still unfinished; the card would keep spinning and the sweeper would keep re-asking")
	}
	if settled.GateReason != wantReason {
		t.Errorf("gate_reason = %q, want %q", settled.GateReason, wantReason)
	}
	if len(h.qa.calls) != 1 {
		t.Fatalf("got %d reviewer dispatches, want 1 — an unopened gate is the deadlock this exists to fix", len(h.qa.calls))
	}
	if h.qa.gateReasons[0] != wantReason {
		t.Errorf("the dispatch carried gate reason %q, want %q: board history must not read like a passing build",
			h.qa.gateReasons[0], wantReason)
	}
	for _, col := range h.tasks.moves {
		if col == domain.TaskColumnNeedRevision {
			t.Error("an absent CI result was treated as a failing one; the work was never judged")
		}
	}
	if len(h.tasks.comments) == 0 {
		t.Error("the card says nothing about reviewing an unbuilt diff")
	}
}

func TestGateOpensOnTimeout(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "in_progress"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "in_progress"}})

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	h.assertGateOpened(t, domain.PipelineGateReasonTimeout)
	if !strings.Contains(h.settled(t).Note, PipelineGateWindow.String()) {
		t.Errorf("the note should name the window it waited out, got %q", h.settled(t).Note)
	}
}

func TestGateStaysClosedInsideTheWindow(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "in_progress"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "in_progress"}})

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settled := h.settled(t); settled.Status != domain.PipelineStatusRunning {
		t.Errorf("status = %q, want running: a live pipeline must be left alone", settled.Status)
	}
	if len(h.qa.calls) != 0 {
		t.Error("the reviewer was dispatched while the build was still running")
	}
}

func TestGateWindowOutlastsTheInProcessPoll(t *testing.T) {
	if PipelineGateWindow <= pipelineMaxWait {
		t.Fatalf("PipelineGateWindow (%s) must exceed pipelineMaxWait (%s), or a live pipeline is abandoned before it can answer",
			PipelineGateWindow, pipelineMaxWait)
	}
}

func TestGateOpensOnQuotaRefusalWithoutWaitingOutTheWindow(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t, nil, githubapi.NewAPIErrorForTest(402, "Actions minutes exhausted for this account"), nil)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	h.assertGateOpened(t, domain.PipelineGateReasonCIUnavailable)
}

func TestGateTellsBillingRefusalsFromRateLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		message  string
		wantOpen bool
	}{
		{"billing", "Actions is disabled for this repository: spending limit reached", true},
		{"rate limit", "You have exceeded a secondary rate limit", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
			stubGitHub(t, nil, githubapi.NewAPIErrorForTest(403, tc.message), nil)

			if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			opened := len(h.qa.calls) > 0
			if opened != tc.wantOpen {
				t.Fatalf("gate opened = %v, want %v (a rate limit must not be read as an exhausted account)", opened, tc.wantOpen)
			}
		})
	}
}

func TestGateOpensWhenNoRunEverAppearedForTheCommit(t *testing.T) {
	h := newGateHarness(t, pipelineNoRunGrace+time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t, nil, nil, nil)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	h.assertGateOpened(t, domain.PipelineGateReasonCIUnavailable)
	if !strings.Contains(h.settled(t).Note, domain.ShortSHA(h.pipeline.HeadSHA)) {
		t.Errorf("the note should name the commit nothing ran for, got %q", h.settled(t).Note)
	}
}

func TestGateWaitsOutTheNoRunGraceBeforeCallingCIUnavailable(t *testing.T) {
	h := newGateHarness(t, 10*time.Second, buildJobMapping(uuid.New()))
	stubGitHub(t, nil, nil, nil)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(h.qa.calls) != 0 {
		t.Error("the gate opened before GitHub had time to create the run")
	}
}

func TestGateOpensWhenNoWorkflowIsMapped(t *testing.T) {
	h := newGateHarness(t, time.Second, nil)
	prevRuns := gateListRunsByHeadSHA
	gateListRunsByHeadSHA = func(context.Context, string, string, string, string) ([]githubapi.WorkflowRun, error) {
		t.Error("asked GitHub about a repository with no mapped checks")
		return nil, nil
	}
	t.Cleanup(func() { gateListRunsByHeadSHA = prevRuns })

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	h.assertGateOpened(t, domain.PipelineGateReasonNoCI)
}

func TestFailedPipelineStillRoutesToNeedRevision(t *testing.T) {
	h := newGateHarness(t, time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "failure"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "failure"}})

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	settled := h.settled(t)
	if settled.Status != domain.PipelineStatusFailed {
		t.Errorf("status = %q, want failed", settled.Status)
	}
	if settled.GateReason != "" {
		t.Errorf("a real failure must carry no gate reason, got %q", settled.GateReason)
	}
	if len(h.qa.calls) != 0 {
		t.Fatal("the reviewer was dispatched onto a red build; the gate was silently opened on a failure")
	}
	if len(h.tasks.moves) != 1 || h.tasks.moves[0] != domain.TaskColumnNeedRevision {
		t.Fatalf("moves = %v, want one move to need_revision", h.tasks.moves)
	}
	if h.tasks.reasons[0] != domain.MoveReasonPipelineFailed {
		t.Errorf("system reason = %q, want %q", h.tasks.reasons[0], domain.MoveReasonPipelineFailed)
	}
}

func TestTimeoutWithAReportedFailurePrefersNeedRevision(t *testing.T) {
	repoID := uuid.New()
	mappings := append(buildJobMapping(repoID), domain.RepositoryPipelineJob{
		RepositoryID: repoID, Category: domain.PipelineCategoryTest,
		TargetKind: domain.PipelineTargetJob, TargetRef: "test",
	})
	h := newGateHarness(t, PipelineGateWindow+time.Minute, mappings)
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "failure"}}, nil,
		[]githubapi.RunJob{
			{Name: "build", Status: "completed", Conclusion: "failure"},
			{Name: "test", Status: "in_progress"},
		})

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settled := h.settled(t); settled.Status != domain.PipelineStatusFailed {
		t.Errorf("status = %q, want failed: one red check is enough", settled.Status)
	}
	if len(h.qa.calls) != 0 {
		t.Error("a red build with a straggler was reviewed instead of returned")
	}
	if len(h.tasks.moves) != 1 || h.tasks.moves[0] != domain.TaskColumnNeedRevision {
		t.Fatalf("moves = %v, want need_revision", h.tasks.moves)
	}
}

func TestPollResolvesAnUnfinishedPipelineFromGitHub(t *testing.T) {
	h := newGateHarness(t, 10*time.Minute, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "success"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "success"}})

	sweeper := NewPipelineGateSweeper(h.store, h.runner, PipelineGateWindow)
	sweeper.sweep(context.Background())

	settled := h.settled(t)
	if settled.Status != domain.PipelineStatusSuccess {
		t.Fatalf("status = %q, want success", settled.Status)
	}
	if settled.Provider != domain.PipelineProviderGitHubActions {
		t.Errorf("provider = %q, want github_actions", settled.Provider)
	}
	if len(h.qa.calls) != 1 {
		t.Fatalf("got %d reviewer dispatches, want 1", len(h.qa.calls))
	}
	if h.qa.gateReasons[0] != "" {
		t.Errorf("a real success carried gate reason %q, want empty", h.qa.gateReasons[0])
	}
	if len(h.tasks.comments) != 0 {
		t.Errorf("a passing build should not leave a gate-skipped note: %v", h.tasks.comments)
	}
}

func TestSweepSkipsPipelinesThisProcessIsAlreadyPolling(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	h.runner.inflight.Store(h.pipeline.ID, struct{}{})
	stubGitHub(t, nil, nil, nil)

	NewPipelineGateSweeper(h.store, h.runner, PipelineGateWindow).sweep(context.Background())

	if settled := h.settled(t); settled.Status != domain.PipelineStatusRunning {
		t.Errorf("status = %q, want running: the in-process poll still owns it", settled.Status)
	}
	if len(h.qa.calls) != 0 {
		t.Error("the sweeper resolved a pipeline the in-process poll was driving")
	}
}

func TestSweepIgnoresDeployPipelines(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	p := h.settled(t)
	p.Trigger = domain.PipelineTriggerProdDeploy
	if _, err := h.store.Update(context.Background(), p); err != nil {
		t.Fatalf("retrigger: %v", err)
	}

	NewPipelineGateSweeper(h.store, h.runner, PipelineGateWindow).sweep(context.Background())

	if settled := h.settled(t); settled.Status != domain.PipelineStatusRunning {
		t.Errorf("status = %q, want running: deploy pipelines belong to DeploySweeper", settled.Status)
	}
	if len(h.tasks.moves) != 0 {
		t.Errorf("a deploy pipeline was moved by the review gate: %v", h.tasks.moves)
	}
}

func TestResolveFiresNoSideEffectsOnceTheTaskLeftCodeReview(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	h.tasks.task.Column = domain.TaskColumnInQA
	stubGitHub(t, nil, nil, nil)

	if err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	settled := h.settled(t)
	if settled.Status != domain.PipelineStatusSkipped || settled.FinishedAt == nil {
		t.Errorf("the row must stop claiming to run, got status=%q finished=%v", settled.Status, settled.FinishedAt)
	}
	if len(h.qa.calls) != 0 || len(h.tasks.moves) != 0 {
		t.Errorf("side effects fired for a task that had already left code_review: dispatches=%d moves=%v",
			len(h.qa.calls), h.tasks.moves)
	}
}

func TestResolveLeavesThePipelineAloneWhenTheTaskCannotBeRead(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	h.tasks.getErr = errors.New("connection reset")

	err := h.runner.ResolveUnfinished(context.Background(), h.pipeline, PipelineGateWindow)
	if err == nil {
		t.Fatal("expected the read failure to be reported, not swallowed")
	}
	if settled := h.settled(t); settled.Status != domain.PipelineStatusRunning {
		t.Errorf("status = %q, want running", settled.Status)
	}
	if len(h.qa.calls) != 0 {
		t.Error("the gate opened on a control-plane error")
	}
}

func TestResolveByHeadSHAUpdatesTaskPipelinesFromAWorkflowRun(t *testing.T) {
	h := newGateHarness(t, 30*time.Second, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "success"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "success"}})

	n, err := h.runner.ResolveByHeadSHA(context.Background(), h.repoID, h.pipeline.HeadSHA)
	if err != nil {
		t.Fatalf("resolve by head sha: %v", err)
	}
	if n != 1 {
		t.Fatalf("resolved %d pipelines, want 1", n)
	}
	settled := h.settled(t)
	if settled.Status != domain.PipelineStatusSuccess {
		t.Errorf("status = %q, want success — the delivery must land on task_pipelines", settled.Status)
	}
	jobs, err := h.store.ListJobs(context.Background(), h.pipeline.ID)
	if err != nil || len(jobs) != 1 || jobs[0].Status != domain.PipelineJobStatusSuccess {
		t.Errorf("job rows = %v (err %v), want one successful build job", jobs, err)
	}
	if len(h.qa.calls) != 1 {
		t.Errorf("got %d reviewer dispatches, want 1: the delivery is what opens the gate", len(h.qa.calls))
	}
}

func TestResolveByHeadSHARoutesAFailingRunToNeedRevision(t *testing.T) {
	h := newGateHarness(t, 30*time.Second, buildJobMapping(uuid.New()))
	stubGitHub(t,
		[]githubapi.WorkflowRun{{ID: 7, Status: "completed", Conclusion: "failure"}}, nil,
		[]githubapi.RunJob{{Name: "build", Status: "completed", Conclusion: "failure"}})

	if _, err := h.runner.ResolveByHeadSHA(context.Background(), h.repoID, h.pipeline.HeadSHA); err != nil {
		t.Fatalf("resolve by head sha: %v", err)
	}
	if len(h.tasks.moves) != 1 || h.tasks.moves[0] != domain.TaskColumnNeedRevision {
		t.Fatalf("moves = %v, want need_revision", h.tasks.moves)
	}
	if len(h.qa.calls) != 0 {
		t.Error("a failing workflow_run opened the review gate")
	}
}

func TestResolveByHeadSHAIgnoresCommitsNobodyIsWaitingOn(t *testing.T) {
	h := newGateHarness(t, 30*time.Second, buildJobMapping(uuid.New()))
	stubGitHub(t, nil, nil, nil)

	n, err := h.runner.ResolveByHeadSHA(context.Background(), h.repoID, "0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("resolve by head sha: %v", err)
	}
	if n != 0 {
		t.Errorf("resolved %d pipelines for an unrelated commit, want 0", n)
	}
	if settled := h.settled(t); settled.Status != domain.PipelineStatusRunning {
		t.Errorf("an unrelated delivery settled a live pipeline: %q", settled.Status)
	}
}

func TestGateOpensOnTimeoutForAPipelineWithNoRecordedCommit(t *testing.T) {
	h := newGateHarness(t, PipelineGateWindow+time.Minute, buildJobMapping(uuid.New()))
	p := h.settled(t)
	p.HeadSHA = ""
	if _, err := h.store.Update(context.Background(), p); err != nil {
		t.Fatalf("clear head sha: %v", err)
	}
	prevRuns := gateListRunsByHeadSHA
	gateListRunsByHeadSHA = func(context.Context, string, string, string, string) ([]githubapi.WorkflowRun, error) {
		t.Error("asked GitHub about a pipeline with no recorded commit")
		return nil, nil
	}
	t.Cleanup(func() { gateListRunsByHeadSHA = prevRuns })

	if err := h.runner.ResolveUnfinished(context.Background(), h.store.mustGet(t, h.pipeline.ID), PipelineGateWindow); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	h.assertGateOpened(t, domain.PipelineGateReasonTimeout)
}

func (f *fakePipelineStore) mustGet(t *testing.T, id uuid.UUID) domain.TaskPipeline {
	t.Helper()
	p, err := f.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	return p
}

func TestEveryGateReasonIsLabelledAndRecognised(t *testing.T) {
	for _, reason := range []string{
		domain.PipelineGateReasonTimeout,
		domain.PipelineGateReasonCIUnavailable,
		domain.PipelineGateReasonNoCI,
	} {
		if !domain.PipelineGateReasonOpen(reason) {
			t.Errorf("%q is not recognised as a gate-open reason", reason)
		}
		if label := gateReasonLabel(reason); label == "" || label == "gate opened" {
			t.Errorf("%q has no specific label, got %q", reason, label)
		}
	}
	if domain.PipelineGateReasonOpen("") {
		t.Error("an ordinary pipeline (no reason) must not be reported as a skipped gate")
	}
}
