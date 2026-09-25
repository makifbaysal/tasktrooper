package board

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploywatch"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeDeployWatch scripts the legacy per-task signal get_deploy_logs falls
// back to when a task has no release.
type fakeDeployWatch struct {
	status       domain.DeployWatchStatus
	statusErr    error
	jobLogs      deploywatch.LogResult
	jobLogsErr   error
	gotJobID     int64
	endpointLogs deploywatch.LogResult
	endpointErr  error
	gotEnv       string
}

func (f *fakeDeployWatch) Status(context.Context, uuid.UUID, uuid.UUID) (domain.DeployWatchStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeDeployWatch) JobLogs(_ context.Context, _ uuid.UUID, jobID int64, _ int) (deploywatch.LogResult, error) {
	f.gotJobID = jobID
	return f.jobLogs, f.jobLogsErr
}

func (f *fakeDeployWatch) EndpointLogs(_ context.Context, _ uuid.UUID, env string, _ int) (deploywatch.LogResult, error) {
	f.gotEnv = env
	return f.endpointLogs, f.endpointErr
}

func deployLogsCtx(repoID, taskID uuid.UUID) context.Context {
	return registry.ContextWithRepositoryID(registry.ContextWithTaskID(context.Background(), taskID), repoID)
}

func TestDeployLogsToolNotConfigured(t *testing.T) {
	tool := newDeployLogsTool(&ToolKit{Tasks: &fakeTaskManager{}})
	res := tool.Execute(context.Background(), `{}`)
	if !res.IsError || !strings.Contains(res.Content, "not configured") {
		t.Fatalf("want a not-configured error, got: %+v", res)
	}
}

// The release's own failed_job is preferred over the legacy per-task status
// now: it is what deploy_release/watch_release actually recorded, and asking
// the legacy watch at all would be asking the wrong question once a release
// exists.
func TestDeployLogsToolDefaultsToTheReleasesFailedJob(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	releases := &fakeReleaseService{
		release: domain.Release{
			ID: uuid.New(),
			Deploy: &domain.DeployWatchStatus{
				FailedJob: &domain.DeployWatchJob{ID: 777, Name: "deploy"},
			},
		},
	}
	deployWatch := &fakeDeployWatch{
		// Scripted to prove it is NOT consulted: a call would return this id,
		// and the assertion below checks the release's id was used instead.
		status: domain.DeployWatchStatus{FailedJob: &domain.DeployWatchJob{ID: 999}},
	}
	kit := &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, DeployWatch: deployWatch, Releases: releases}
	tool := newDeployLogsTool(kit)

	res := tool.Execute(deployLogsCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if deployWatch.gotJobID != 777 {
		t.Fatalf("job id = %d, want the release's failed_job 777", deployWatch.gotJobID)
	}
}

// A task with no release (no release service wired, or ForTask finds none)
// falls back to the legacy deploy watch exactly as before.
func TestDeployLogsToolFallsBackToLegacyStatusWithNoRelease(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	deployWatch := &fakeDeployWatch{
		status: domain.DeployWatchStatus{FailedJob: &domain.DeployWatchJob{ID: 42}},
	}
	kit := &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, DeployWatch: deployWatch}
	tool := newDeployLogsTool(kit)

	res := tool.Execute(deployLogsCtx(repoID, taskID), `{}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if deployWatch.gotJobID != 42 {
		t.Fatalf("job id = %d, want the legacy status's failed_job 42", deployWatch.gotJobID)
	}
}

func TestDeployLogsToolNoFailedJobAnywhereIsAnError(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	deployWatch := &fakeDeployWatch{status: domain.DeployWatchStatus{State: domain.DeployWatchSuccess}}
	kit := &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, DeployWatch: deployWatch}
	tool := newDeployLogsTool(kit)

	res := tool.Execute(deployLogsCtx(repoID, taskID), `{}`)
	if !res.IsError {
		t.Fatalf("want an error when nothing names a failed job, got: %+v", res)
	}
}

func TestDeployLogsToolExplicitJobIDSkipsResolution(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	deployWatch := &fakeDeployWatch{jobLogs: deploywatch.LogResult{Content: "boom"}}
	kit := &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, DeployWatch: deployWatch}
	tool := newDeployLogsTool(kit)

	res := tool.Execute(deployLogsCtx(repoID, taskID), `{"job_id":555}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if deployWatch.gotJobID != 555 {
		t.Fatalf("job id = %d, want the explicit 555", deployWatch.gotJobID)
	}
	var out deploywatch.LogResult
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Content != "boom" {
		t.Errorf("content = %q, want %q", out.Content, "boom")
	}
}

func TestDeployLogsToolEndpointSource(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	deployWatch := &fakeDeployWatch{endpointLogs: deploywatch.LogResult{Content: "endpoint log"}}
	kit := &ToolKit{Tasks: &fakeTaskManager{taskRepoID: repoID}, DeployWatch: deployWatch}
	tool := newDeployLogsTool(kit)

	res := tool.Execute(deployLogsCtx(repoID, taskID), `{"source":"logs_url"}`)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if deployWatch.gotEnv != domain.DeployEnvProd {
		t.Errorf("env = %q, want the default prod", deployWatch.gotEnv)
	}
}

func TestDeployLogsToolRegistration(t *testing.T) {
	without := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}}))
	with := toolNames(NewExecutors(&ToolKit{Tasks: &fakeTaskManager{}, PullRequests: &fakeTaskPullRequests{}, Releases: &fakeReleaseService{}}))

	if hasName(without, deployLogsToolName) {
		t.Errorf("%s must not be registered without the PR dependency", deployLogsToolName)
	}
	if !hasName(with, deployLogsToolName) {
		t.Errorf("%s is not registered", deployLogsToolName)
	}
}
