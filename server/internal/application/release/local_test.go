package release

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestSplitArgvRespectsQuotes(t *testing.T) {
	argv, err := splitArgv(`./release.sh --version 1.0.0 --note "shipped v1"`)
	require.NoError(t, err)
	assert.Equal(t, []string{"./release.sh", "--version", "1.0.0", "--note", "shipped v1"}, argv)
}

func TestSplitArgvRejectsAnUnterminatedQuote(t *testing.T) {
	_, err := splitArgv(`./release.sh "unterminated`)
	require.Error(t, err)
}

func TestRejectShellMetacharacters(t *testing.T) {
	for _, cmd := range []string{
		"./release.sh | tee log",
		"./release.sh && rm -rf /",
		"./release.sh; echo done",
		"./release.sh `whoami`",
		"./release.sh $(whoami)",
	} {
		assert.Error(t, rejectShellMetacharacters(cmd), cmd)
	}
	assert.NoError(t, rejectShellMetacharacters("./release.sh --version 1.0.0"))
}

type localFixture struct {
	svc    *Service
	store  *fakeReleaseStore
	runner *fakeLocalRunner
	repos  *fakeRepos
	waker  *fakeWaker
	tasks  *fakeTasks
	parked *fakeParked
}

func newLocalFixture() *localFixture {
	store := newFakeReleaseStore()
	runner := &fakeLocalRunner{}
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "desktop", RootPath: "/repos/desktop"}}
	waker := &fakeWaker{}
	tasks := newFakeTasks()
	parked := newFakeParked()
	svc := New(Deps{
		Store: store, LocalRunner: runner, Repos: repos, Waker: waker, Tasks: tasks, ParkedTasks: parked,
		DataDir: "/data",
		Clock:   func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &localFixture{svc: svc, store: store, runner: runner, repos: repos, waker: waker, tasks: tasks, parked: parked}
}

func TestDeployBatchLocalStartsTheRunAndReturnsImmediately(t *testing.T) {
	f := newLocalFixture()
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	require.NotNil(t, updated.LocalRun)
	assert.Equal(t, []string{"./release.sh", "1.2.0"}, updated.LocalRun.Argv)
	assert.Contains(t, updated.LocalRun.LogPath, created.ID.String())
	require.Len(t, f.runner.specs, 1)
	assert.Equal(t, f.repos.repo.RootPath, f.runner.specs[0].RootPath)
	assert.Equal(t, mergeSHA, f.runner.specs[0].CommitSHA)
	assert.Contains(t, f.runner.specs[0].Env, "RELEASE_VERSION=1.2.0")
	assert.Contains(t, f.runner.specs[0].Env, "RELEASE_TAG=v1.2.0")
	assert.Contains(t, f.runner.specs[0].Env, "RELEASE_COMMIT="+mergeSHA)
}

func TestDeployBatchLocalRejectsShellMetacharacters(t *testing.T) {
	f := newLocalFixture()
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	r.Profile.LocalCommand = "./release.sh {version} && rm -rf /"
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.Empty(t, f.runner.specs, "the runner must never see a rejected command")
}

func TestCompleteLocalRunSuccessLeavesStatusDeployingForTheSweeper(t *testing.T) {
	f := newLocalFixture()
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)
	deployed, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	f.svc.CompleteLocalRun(context.Background(), deployed.ID, 0, "build ok\npublished", nil)

	after, err := f.store.Get(context.Background(), deployed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, after.Status, "CompleteLocalRun records the outcome; the sweeper advances the status")
	require.NotNil(t, after.LocalRun.ExitCode)
	assert.Equal(t, 0, *after.LocalRun.ExitCode)
	require.NotNil(t, after.LocalRun.FinishedAt)
	require.NotNil(t, after.Deploy)
	assert.Equal(t, domain.DeployWatchSuccess, after.Deploy.State)
}

func TestCompleteLocalRunFailureRecordsTheTailAsDetail(t *testing.T) {
	f := newLocalFixture()
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)
	deployed, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	f.svc.CompleteLocalRun(context.Background(), deployed.ID, 1, "error: signing failed", nil)

	after, err := f.store.Get(context.Background(), deployed.ID)
	require.NoError(t, err)
	require.NotNil(t, after.Deploy)
	assert.Equal(t, domain.DeployWatchFailure, after.Deploy.State)
	assert.Contains(t, after.Deploy.Detail, "signing failed")
}

func TestSweepDeployingLocalSettlesToVerifyingOnSuccess(t *testing.T) {
	f := newLocalFixture()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: f.repos.repo.ID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	deployed, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	f.svc.CompleteLocalRun(context.Background(), deployed.ID, 0, "ok", nil)

	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), deployed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, after.Status)
}

func TestSweepDeployingLocalFailsOnANonZeroExit(t *testing.T) {
	f := newLocalFixture()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: f.repos.repo.ID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	deployed, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	f.svc.CompleteLocalRun(context.Background(), deployed.ID, 1, "boom", errors.New("exit status 1"))

	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), deployed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, after.Status)
	assert.NotEmpty(t, after.FailureReason)
	assert.Len(t, f.waker.calls, 1, "a failed local run must hand the card back")
}

func TestSweepDeployingLocalFailsWhenNoReportWithin70Minutes(t *testing.T) {
	f := newLocalFixture()
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f.svc.SetClock(clock.Now)
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: f.repos.repo.ID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	r := pendingBatchRelease(f.repos.repo.ID, domain.ExecutorLocal)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	deployed, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	clock.Advance(71 * time.Minute)
	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), deployed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, after.Status)
	assert.Contains(t, after.FailureReason, "restarted")
}
