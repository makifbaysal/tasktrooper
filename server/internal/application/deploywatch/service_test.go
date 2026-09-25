package deploywatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const mergeSHA = "abc123def456789012345678901234567890abcd"

var (
	repoID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	taskID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fixed  = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
)

func releasedTask() domain.BoardTask {
	plan := "Turn the `new_pricing` flag off, then revert. The migration adding pricing_tier must be dropped by hand."
	return domain.BoardTask{
		ID:             taskID,
		RepositoryID:   repoID,
		Key:            "T-7",
		Title:          "New pricing tiers",
		TaskType:       "task",
		Column:         domain.TaskColumnDone,
		MergeCommitSHA: mergeSHA,
		HasMigration:   true,
		RollbackPlan:   &plan,
	}
}

type harness struct {
	svc       *deploywatch.Service
	actions   *fakeActions
	comments  *fakeComments
	incidents *fakeIncidents
	git       *fakeGit
	rollback  *fakeRollbacker
	runs      *fakeRuns
	targets   *fakeTargets
	pipeline  *fakePipelineJobs
}

func newHarness(t *testing.T, task domain.BoardTask, target domain.DeployTarget, opts ...func(*deploywatch.Deps)) *harness {
	t.Helper()
	h := &harness{
		actions:   &fakeActions{jobsByRun: map[int64][]port.ActionsJob{}, logs: map[int64]string{}},
		comments:  &fakeComments{},
		incidents: &fakeIncidents{},
		git:       &fakeGit{},
		rollback:  &fakeRollbacker{},
		runs:      &fakeRuns{byEnv: map[string][]domain.DeploymentRun{}},
		targets:   &fakeTargets{byEnv: map[string]domain.DeployTarget{domain.DeployEnvProd: target}},
		pipeline:  &fakePipelineJobs{},
	}
	deps := deploywatch.Deps{
		Tasks:     newFakeTasks(task),
		Comments:  h.comments,
		Targets:   h.targets,
		Repos:     &fakeRepos{repo: domain.Repository{ID: repoID, Name: "acme", RootPath: "/tmp/acme"}},
		Runs:      h.runs,
		Pipeline:  h.pipeline,
		Actions:   h.actions,
		Rollbacks: h.rollback,
		Git:       h.git,
		Incidents: h.incidents,
		RepoCoordinates: func(context.Context, domain.Repository) (string, string, error) {
			return "acme-org", "acme", nil
		},
	}
	for _, opt := range opts {
		opt(&deps)
	}
	h.svc = deploywatch.New(deps)
	h.svc.SetClock(func() time.Time { return fixed })
	return h
}

func TestStatusResolvesFromActionsDeployJob(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd, HealthURL: "https://api.example.com/health"})
	h.actions.runsForCommit = actionRuns(actionRun(55, "https://gh/run/55"))
	h.actions.jobsByRun[55] = jobs(
		job(1, "build", "completed", "success"),
		job(2, "deploy", "completed", "success"),
	)

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success (detail: %s)", got.State, got.Detail)
	}
	if got.Signal != domain.DeploySignalActionsRun {
		t.Fatalf("signal = %q, want actions_run", got.Signal)
	}

	if h.actions.commitCalls != 0 {
		t.Fatalf("commit status consulted %d times despite an Actions deploy job", h.actions.commitCalls)
	}
	if got.HealthWindowUntil == nil || !got.HealthWindowUntil.Equal(fixed.Add(deploywatch.DefaultHealthWindow)) {
		t.Fatalf("health window = %v, want %v", got.HealthWindowUntil, fixed.Add(deploywatch.DefaultHealthWindow))
	}
}

func TestStatusIgnoresNonDeployJobsInTheSameRun(t *testing.T) {

	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(7, ""))
	h.actions.jobsByRun[7] = jobs(
		job(1, "test", "completed", "failure"),
		job(2, "deploy", "completed", "success"),
	)

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success — a failing test job is not a failing deploy", got.State)
	}
}

func TestStatusPendingWhileDeployJobRuns(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(9, ""))
	h.actions.jobsByRun[9] = jobs(job(3, "deploy", "in_progress", ""))

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchPending {
		t.Fatalf("state = %q, want pending", got.State)
	}
	if got.State.Settled() {
		t.Fatal("pending must not be Settled — the sweeper would resume the task immediately")
	}
	if got.HealthWindowUntil != nil {
		t.Fatal("a pending deploy must not open a health window")
	}
}

func TestStatusResolvesFromCommitStatusWhenNoActionsDeployJob(t *testing.T) {

	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(12, ""))
	h.actions.jobsByRun[12] = jobs(job(4, "lint", "completed", "success"))
	h.actions.commitSignal = commitSignal(domain.DeploySignalCommitStatus, "success", "Vercel")

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success", got.State)
	}
	if got.Signal != domain.DeploySignalCommitStatus {
		t.Fatalf("signal = %q, want commit_status", got.Signal)
	}
	if len(got.Contexts) == 0 || got.Contexts[0] != "Vercel" {
		t.Fatalf("contexts = %v, want the Vercel context named", got.Contexts)
	}
	if !strings.Contains(got.Detail, "deploys on push") {
		t.Fatalf("detail should say this repository deploys on push, got %q", got.Detail)
	}
}

func TestStatusResolvesFromDeploymentStatus(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.commitSignal = commitSignal(domain.DeploySignalDeploymentState, "failure", "")

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchFailure {
		t.Fatalf("state = %q, want failure", got.State)
	}
	if got.Signal != domain.DeploySignalDeploymentState {
		t.Fatalf("signal = %q, want deployment_status", got.Signal)
	}
}

func TestStatusNoSignalIsNotAFailure(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchNoSignal {
		t.Fatalf("state = %q, want no_signal", got.State)
	}
	if !got.State.Settled() {
		t.Fatal("no_signal must be Settled — the watch has an answer and must end")
	}
}

func TestStatusWithoutMergeCommitIsUnknown(t *testing.T) {
	task := releasedTask()
	task.MergeCommitSHA = ""
	h := newHarness(t, task, domain.DeployTarget{Env: domain.DeployEnvProd})

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchUnknown {
		t.Fatalf("state = %q, want unknown", got.State)
	}
}

func TestFailedDeployNamesTheJobAndItsLogIsSummarized(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(21, "https://gh/run/21"))
	h.actions.jobsByRun[21] = jobs(job(99, "deploy", "completed", "failure"))
	h.actions.logs[99] = strings.Repeat("noisy setup line\n", 2000) +
		"##[error]migration 0021 failed: relation pricing_tier already exists\n" +
		strings.Repeat("cleanup\n", 200)

	status, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != domain.DeployWatchFailure {
		t.Fatalf("state = %q, want failure", status.State)
	}
	if status.FailedJob == nil || status.FailedJob.ID != 99 {
		t.Fatalf("failed job = %+v, want job 99 so get_deploy_logs has something to fetch", status.FailedJob)
	}

	res, err := h.svc.JobLogs(context.Background(), repoID, status.FailedJob.ID, 2000)
	if err != nil {
		t.Fatalf("JobLogs: %v", err)
	}
	if !res.Truncated {
		t.Fatal("a 40k-line log must come back truncated, not whole")
	}
	if len(res.Content) > 2400 {
		t.Fatalf("summary is %d chars, well past the 2000 cap", len(res.Content))
	}

	if !strings.Contains(res.Content, "migration 0021 failed") {
		t.Fatalf("the error line was lost in truncation:\n%s", res.Content)
	}
}

func TestSummarizeLogKeepsShortLogsWhole(t *testing.T) {
	out, truncated := deploywatch.SummarizeLog("boom\n", 500)
	if truncated {
		t.Fatal("a short log must not be reported as truncated")
	}
	if out != "boom" {
		t.Fatalf("out = %q", out)
	}
}

func TestAttributeReleaseNamesTheTaskThatDeployedTheLiveCommit(t *testing.T) {
	task := releasedTask()
	h := newHarness(t, task, domain.DeployTarget{Env: domain.DeployEnvProd})
	completed := fixed.Add(-5 * time.Minute)
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{
		RepositoryID: repoID,
		Env:          domain.DeployEnvProd,
		HeadSHA:      mergeSHA,
		Status:       domain.RunStatusCompleted,
		Conclusion:   domain.RunConclusionSuccess,
		CompletedAt:  &completed,
	}}

	got, ok := h.svc.AttributeRelease(context.Background(), repoID, domain.DeployEnvProd, fixed)
	if !ok {
		t.Fatal("expected the incident to be attributed to T-7")
	}
	if got.TaskID != taskID || got.TaskKey != "T-7" {
		t.Fatalf("attribution = %+v, want T-7", got)
	}
	if got.MergeSHA != mergeSHA {
		t.Fatalf("merge sha = %q", got.MergeSHA)
	}
}

func TestAttributeReleaseDeclinesOutsideTheHealthWindow(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	completed := fixed.Add(-2 * time.Hour)
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{
		HeadSHA: mergeSHA, Status: domain.RunStatusCompleted,
		Conclusion: domain.RunConclusionSuccess, CompletedAt: &completed,
	}}

	if _, ok := h.svc.AttributeRelease(context.Background(), repoID, domain.DeployEnvProd, fixed); ok {
		t.Fatal("a release two hours old must not be blamed for a fresh incident")
	}
}

func TestAttributeReleaseIgnoresAFailedDeploy(t *testing.T) {

	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	completed := fixed.Add(-1 * time.Minute)
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{
		HeadSHA: mergeSHA, Status: domain.RunStatusCompleted,
		Conclusion: domain.RunConclusionFailure, CompletedAt: &completed,
	}}

	if _, ok := h.svc.AttributeRelease(context.Background(), repoID, domain.DeployEnvProd, fixed); ok {
		t.Fatal("a FAILED deploy must not be attributed as the live release")
	}
}

func TestAttributeReleaseDeclinesWhenNoTaskClaimsTheCommit(t *testing.T) {

	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	completed := fixed.Add(-1 * time.Minute)
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{
		HeadSHA: "0000000000000000000000000000000000000000",
		Status:  domain.RunStatusCompleted, Conclusion: domain.RunConclusionSuccess, CompletedAt: &completed,
	}}

	if _, ok := h.svc.AttributeRelease(context.Background(), repoID, domain.DeployEnvProd, fixed); ok {
		t.Fatal("a commit no task claims must not be attributed")
	}
}

func failedDeployHarness(t *testing.T, target domain.DeployTarget) *harness {
	t.Helper()
	h := newHarness(t, releasedTask(), target)
	h.actions.runsForCommit = actionRuns(actionRun(31, ""))
	h.actions.jobsByRun[31] = jobs(job(77, "deploy", "completed", "failure"))
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{HeadSHA: mergeSHA}}
	return h
}

func rollbackReq() deploywatch.RollbackRequest {
	return deploywatch.RollbackRequest{
		RepositoryID: repoID,
		TaskID:       taskID,
		Env:          domain.DeployEnvProd,
		Trigger:      deploywatch.RollbackTriggerDeployFailed,
		AgentName:    "qa-agent",
		Note:         "deploy job failed on the migration step",
	}
}

func TestRollbackAutoDisabledProposesAndExecutesNothing(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: false})

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !res.Proposed || res.RolledBack {
		t.Fatalf("result = %+v, want proposed and not rolled back", res)
	}
	if len(h.rollback.calls) != 0 {
		t.Fatal("auto_rollback is off — nothing may be dispatched")
	}
	if len(h.git.reverted) != 0 {
		t.Fatal("auto_rollback is off — nothing may be reverted")
	}
	if !strings.Contains(res.Message, "auto_rollback is off") {
		t.Fatalf("message must say why nothing happened: %q", res.Message)
	}
	if len(h.incidents.ingests) == 0 {
		t.Fatal("a proposed rollback must still open an incident")
	}
	if !strings.Contains(h.comments.all(), "PROPOSED") {
		t.Fatalf("the proposal must land on the card:\n%s", h.comments.all())
	}
}

func TestRollbackWorkflowMechanismDispatchesTheTag(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})
	h.rollback.dispatch = domain.DeployDispatch{
		Ref: "rollback/prod/1700000000", WorkflowFile: "deploy-prod.yml",
		RollbackOfSHA: "feedfacefeedfacefeedfacefeedfacefeedface",
	}

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !res.RolledBack || res.Mechanism != domain.RollbackMechanismWorkflow {
		t.Fatalf("result = %+v, want a workflow_dispatch rollback", res)
	}
	if res.Ref != "rollback/prod/1700000000" {
		t.Fatalf("ref = %q", res.Ref)
	}
	if len(h.git.reverted) != 0 {
		t.Fatal("a repository with a deploy workflow must not be reverted")
	}
}

func TestRollbackRevertMechanismWhenNoDeployWorkflow(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	h.rollback.err = errNoWorkflow
	h.git.hasGit = true
	h.git.revertSHA = "9999999999999999999999999999999999999999"

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !res.RolledBack || res.Mechanism != domain.RollbackMechanismRevert {
		t.Fatalf("result = %+v, want a revert_push rollback", res)
	}
	if len(h.git.reverted) != 1 || h.git.reverted[0] != mergeSHA {
		t.Fatalf("reverted %v, want exactly the merge commit", h.git.reverted)
	}
	if res.RevertSHA != "9999999999999999999999999999999999999999" {
		t.Fatalf("revert sha = %q", res.RevertSHA)
	}
}

func TestRollbackReportsManualStepsItCannotPerform(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	res, err := h.svc.Rollback(context.Background(), rollbackReq())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	joined := strings.Join(res.ManualSteps, "\n")
	if !strings.Contains(joined, "DATABASE SCHEMA") {
		t.Fatalf("a task with has_migration must be told the migration is NOT reversed:\n%s", joined)
	}
	if !strings.Contains(joined, "new_pricing") {
		t.Fatalf("the task's own rollback_plan must be handed back verbatim:\n%s", joined)
	}
	if !strings.Contains(h.comments.all(), "MECHANICAL half") {
		t.Fatalf("the card must say the rollback was only the mechanical half:\n%s", h.comments.all())
	}
}

func TestRollbackRefusesWhenAnotherReleaseIsLive(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{HeadSHA: "1234512345123451234512345123451234512345"}}

	_, err := h.svc.Rollback(context.Background(), rollbackReq())
	if !errors.Is(err, domain.ErrRollbackNotOwner) {
		t.Fatalf("err = %v, want ErrRollbackNotOwner", err)
	}
	if len(h.rollback.calls) != 0 {
		t.Fatal("nothing may be dispatched when the task does not own the live release")
	}
}

func TestRollbackCarriesTheAgentAuthorizationIntoDeployops(t *testing.T) {
	h := failedDeployHarness(t, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	if _, err := h.svc.Rollback(context.Background(), rollbackReq()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(h.rollback.calls) != 1 {
		t.Fatalf("expected exactly one authorized rollback, got %d", len(h.rollback.calls))
	}
	auth := h.rollback.calls[0]
	if auth.TaskID != taskID || auth.TaskKey != "T-7" {
		t.Fatalf("auth does not name the card: %+v", auth)
	}
	if auth.OwnedMergeSHA != mergeSHA {
		t.Fatalf("auth.OwnedMergeSHA = %q, want the commit whose ownership was proven", auth.OwnedMergeSHA)
	}
	if auth.Trigger != deploywatch.RollbackTriggerDeployFailed || auth.AgentName != "qa-agent" {
		t.Fatalf("auth loses the audit fields: %+v", auth)
	}

	if h.rollback.inputs[0].Confirm != "" {
		t.Fatalf("an agent rollback must not supply a confirmation phrase, got %q", h.rollback.inputs[0].Confirm)
	}
	if h.rollback.inputs[0].Actor != "agent:qa-agent" {
		t.Fatalf("actor = %q, want the agent named in the audit", h.rollback.inputs[0].Actor)
	}
}

func TestRollbackRefusesWithoutARealTrigger(t *testing.T) {

	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})
	h.actions.runsForCommit = actionRuns(actionRun(41, ""))
	h.actions.jobsByRun[41] = jobs(job(5, "deploy", "completed", "success"))
	h.runs.byEnv[domain.DeployEnvProd] = []domain.DeploymentRun{{HeadSHA: mergeSHA}}

	_, err := h.svc.Rollback(context.Background(), rollbackReq())
	if !errors.Is(err, domain.ErrRollbackNoTrigger) {
		t.Fatalf("err = %v, want ErrRollbackNoTrigger", err)
	}
}

func TestRollbackRefusesAnUnmergedTask(t *testing.T) {
	task := releasedTask()
	task.MergeCommitSHA = ""
	h := newHarness(t, task, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	_, err := h.svc.Rollback(context.Background(), rollbackReq())
	if !errors.Is(err, domain.ErrRollbackNotMerged) {
		t.Fatalf("err = %v, want ErrRollbackNotMerged", err)
	}
}

func TestRollbackRefusesFromANonReleasedColumn(t *testing.T) {
	task := releasedTask()
	task.Column = domain.TaskColumnInQA
	h := newHarness(t, task, domain.DeployTarget{Env: domain.DeployEnvProd, AutoRollback: true})

	_, err := h.svc.Rollback(context.Background(), rollbackReq())
	if !errors.Is(err, domain.ErrRollbackColumn) {
		t.Fatalf("err = %v, want ErrRollbackColumn", err)
	}
}

// StatusForCommit is StatusForTask keyed on a commit instead of a task — a
// release has no single owning card.

func TestStatusForCommitWithWorkflowUsesOnlyThatWorkflowsRuns(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(55, mergeSHA, "completed", "success")},
	}
	h.actions.jobsByRun[55] = jobs(job(1, "deploy", "completed", "success"))

	got, err := h.svc.StatusForCommit(context.Background(), repoID, mergeSHA, "deploy-prod.yml")
	if err != nil {
		t.Fatalf("StatusForCommit: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success (detail: %s)", got.State, got.Detail)
	}
	if len(h.actions.workflowRunsCalls) != 1 || h.actions.workflowRunsCalls[0] != "deploy-prod.yml" {
		t.Fatalf("workflow runs calls = %v, want exactly one for deploy-prod.yml", h.actions.workflowRunsCalls)
	}
	if h.actions.runsErr == nil && h.actions.commitCalls != 0 {
		t.Fatalf("commit status consulted despite a matching Actions run")
	}
}

func TestStatusForCommitWorkflowFilterIgnoresRunsForAnotherCommit(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(1, "0000000000000000000000000000000000000000", "completed", "success")},
	}

	got, err := h.svc.StatusForCommit(context.Background(), repoID, mergeSHA, "deploy-prod.yml")
	if err != nil {
		t.Fatalf("StatusForCommit: %v", err)
	}
	if got.State != domain.DeployWatchNoSignal {
		t.Fatalf("state = %q, want no_signal — the only run in that workflow was for a different commit", got.State)
	}
}

// The whole-run rule: inside a run matched by workflow (or by a mapped
// prod/preprod target), a run with no job matching by name still counts —
// its own conclusion is the deploy result.

func TestStatusForCommitWholeRunRuleUsesRunConclusionWhenNoJobMatches(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(60, mergeSHA, "completed", "success")},
	}
	h.actions.jobsByRun[60] = jobs(job(1, "build", "completed", "success"), job(2, "test", "completed", "success"))

	got, err := h.svc.StatusForCommit(context.Background(), repoID, mergeSHA, "deploy-prod.yml")
	if err != nil {
		t.Fatalf("StatusForCommit: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success from the whole run's own conclusion", got.State)
	}
}

func TestStatusForCommitWholeRunRuleFailsOnRunConclusion(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(61, mergeSHA, "completed", "failure")},
	}
	h.actions.jobsByRun[61] = jobs(job(1, "build", "completed", "success"))

	got, err := h.svc.StatusForCommit(context.Background(), repoID, mergeSHA, "deploy-prod.yml")
	if err != nil {
		t.Fatalf("StatusForCommit: %v", err)
	}
	if got.State != domain.DeployWatchFailure {
		t.Fatalf("state = %q, want failure from the whole run's own conclusion", got.State)
	}
}

// Without a workflow filter, the whole-run rule only fires for a run whose
// own workflow file is one of the repository's mapped prod/preprod deploy
// targets — otherwise an unrelated run with no matching job must stay silent
// rather than being read as a deploy.

func TestStatusMappedWorkflowWholeRunRuleAppliesWithoutAnExplicitWorkflow(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.pipeline.jobs = []domain.RepositoryPipelineJob{
		{Category: domain.PipelineCategoryProdDeploy, TargetKind: domain.PipelineTargetWorkflow, TargetRef: "deploy-prod.yml"},
	}
	h.actions.runsForCommit = actionRuns(deployRun(70, mergeSHA, "completed", "success"))
	h.actions.jobsByRun[70] = jobs(job(1, "build", "completed", "success"))
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRun(70, mergeSHA, "completed", "success")},
	}

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success — run 70 is the mapped prod_deploy workflow's run for this commit", got.State)
	}
}

func TestStatusUnmappedRunWithNoMatchingJobIsNotADeploy(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(deployRun(71, mergeSHA, "completed", "success"))
	h.actions.jobsByRun[71] = jobs(job(1, "build", "completed", "success"))

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchNoSignal {
		t.Fatalf("state = %q, want no_signal — nothing maps this run to a deploy", got.State)
	}
}

// The deploy job matcher: whole tokens only, and a docs/notes/preview token
// anywhere in the job name disqualifies it even alongside a deploy word.

func TestDeployJobNameMatcherExcludesDocVariants(t *testing.T) {
	cases := []string{"deployment-docs", "release-drafter", "publish-docs", "deploy-preview", "changelog-deploy"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
			h.actions.runsForCommit = actionRuns(actionRun(80, ""))
			h.actions.jobsByRun[80] = jobs(job(1, name, "completed", "success"))

			got, err := h.svc.Status(context.Background(), repoID, taskID)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got.State != domain.DeployWatchNoSignal {
				t.Fatalf("job %q: state = %q, want no_signal (excluded token present)", name, got.State)
			}
		})
	}
}

func TestDeployJobNameMatcherAcceptsWholeTokens(t *testing.T) {
	cases := []string{"deploy", "deploy-prod", "rollout-canary", "deployment", "ship-it"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
			h.actions.runsForCommit = actionRuns(actionRun(81, ""))
			h.actions.jobsByRun[81] = jobs(job(1, name, "completed", "success"))

			got, err := h.svc.Status(context.Background(), repoID, taskID)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got.State != domain.DeployWatchSuccess {
				t.Fatalf("job %q: state = %q, want success", name, got.State)
			}
		})
	}
}

// A run whose jobs could not be listed must never read as "nothing here" —
// that would silently fall back to the commit-status path while an Actions
// deploy may well be running.
func TestStatusJobsListErrorReturnsPendingNotEmpty(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	h.actions.runsForCommit = actionRuns(actionRun(90, "https://gh/run/90"))
	h.actions.jobsErr = errors.New("github: 502")

	got, err := h.svc.Status(context.Background(), repoID, taskID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != domain.DeployWatchPending {
		t.Fatalf("state = %q, want pending — a jobs-list failure must not read as no signal", got.State)
	}
	if !strings.Contains(got.Detail, "could not read the run's jobs") {
		t.Fatalf("detail = %q, want it to say the jobs could not be read", got.Detail)
	}
	if h.actions.commitCalls != 0 {
		t.Fatal("a jobs-list failure must not fall through to the commit-status path")
	}
}

// StatusForCommitSince narrows StatusForCommit to Actions runs started at or
// after since — a release rollback's redeploy watch, which reuses the
// SAME sha/workflow an earlier release (or a prior failed rollback attempt)
// already ran and must not read that other run's outcome as its own.

func TestStatusForCommitSinceIgnoresARunStartedBeforeSince(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	since := fixed
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {deployRunAt(1, mergeSHA, "completed", "success", since.Add(-time.Hour))},
	}

	got, err := h.svc.StatusForCommitSince(context.Background(), repoID, mergeSHA, "deploy-prod.yml", since)
	if err != nil {
		t.Fatalf("StatusForCommitSince: %v", err)
	}
	if got.State != domain.DeployWatchPending {
		t.Fatalf("state = %q, want pending — the only run for this sha/workflow started before the rollback began", got.State)
	}
	if h.actions.commitCalls != 0 {
		t.Fatal("a since-narrowed miss must not fall back to the commit-status signal — it carries no timestamp to pin to since either")
	}
}

func TestStatusForCommitSinceFindsARunStartedAfterSince(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	since := fixed
	h.actions.workflowRuns = map[string][]port.ActionsRun{
		"deploy-prod.yml": {
			deployRunAt(1, mergeSHA, "completed", "success", since.Add(-time.Hour)), // the OLD, stale run
			deployRunAt(2, mergeSHA, "completed", "success", since.Add(time.Minute)),
		},
	}
	h.actions.jobsByRun[2] = jobs(job(1, "deploy", "completed", "success"))

	got, err := h.svc.StatusForCommitSince(context.Background(), repoID, mergeSHA, "deploy-prod.yml", since)
	if err != nil {
		t.Fatalf("StatusForCommitSince: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success — run 2 started after since", got.State)
	}
	if got.RunID != 2 {
		t.Fatalf("run id = %d, want the new run (2), not the stale one (1)", got.RunID)
	}
}

func TestStatusForCommitSinceStillFindsARunWhenNoWorkflowFilterIsGiven(t *testing.T) {
	h := newHarness(t, releasedTask(), domain.DeployTarget{Env: domain.DeployEnvProd})
	since := fixed
	h.actions.runsForCommit = actionRuns(deployRunAt(3, mergeSHA, "completed", "success", since.Add(time.Minute)))
	h.actions.jobsByRun[3] = jobs(job(1, "deploy", "completed", "success"))

	got, err := h.svc.StatusForCommitSince(context.Background(), repoID, mergeSHA, "", since)
	if err != nil {
		t.Fatalf("StatusForCommitSince: %v", err)
	}
	if got.State != domain.DeployWatchSuccess {
		t.Fatalf("state = %q, want success from the matched deploy job", got.State)
	}
}
