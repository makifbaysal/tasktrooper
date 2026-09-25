package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

type sweepFixture struct {
	svc    *Service
	store  *fakeReleaseStore
	ds     *fakeDeployStatus
	envs   *fakeEnvironments
	waker  *fakeWaker
	parked *fakeParked
	tasks  *fakeTasks
	legacy *fakeDeployTargets
	clock  *steppableClock
}

func newSweepFixture() *sweepFixture {
	store := newFakeReleaseStore()
	ds := newFakeDeployStatus()
	envs := newFakeEnvironments()
	waker := &fakeWaker{}
	parked := newFakeParked()
	tasks := newFakeTasks()
	legacy := &fakeDeployTargets{}
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Store:         store,
		DeployStatus:  ds,
		Environments:  envs,
		Waker:         waker,
		ParkedTasks:   parked,
		Tasks:         tasks,
		LegacyTargets: legacy,
		Clock:         clock.Now,
	})
	svc.SetURLPolicy(urlguard.Policy{Schemes: []string{"http", "https"}, AllowLoopback: true, MaxRedirects: 3})
	return &sweepFixture{svc: svc, store: store, ds: ds, envs: envs, waker: waker, parked: parked, tasks: tasks, legacy: legacy, clock: clock}
}

func (f *sweepFixture) withTask(repositoryID uuid.UUID) domain.BoardTask {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: "T-1"}
	f.tasks.tasks[task.ID] = task
	return task
}

func deployingRelease(repositoryID uuid.UUID, startedAt time.Time) domain.Release {
	return domain.Release{
		RepositoryID:    repositoryID,
		Mode:            domain.DeliveryOnMerge,
		Executor:        domain.ExecutorGitHubActions,
		Status:          domain.ReleaseDeploying,
		CommitSHA:       mergeSHA,
		Profile:         deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
		DeployStartedAt: &startedAt,
	}
}

func TestSweepDeployingKeepsAPendingDeployUnderTheTimeout(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchPending})

	f.clock.Advance(30 * time.Minute)
	f.svc.SweepOnce(context.Background())

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, r.Status)
	assert.Empty(t, f.waker.calls)
}

func TestSweepDeployingFailsAPendingDeployAfterSixtyMinutes(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchPending})

	f.clock.Advance(61 * time.Minute)
	f.svc.SweepOnce(context.Background())

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, r.Status)
	assert.Contains(t, r.FailureReason, "60 minutes")
	assert.Len(t, f.waker.calls, 1)
}

func TestSweepDeployingFailsNoSignalAfterFifteenMinutes(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchNoSignal})

	f.clock.Advance(10 * time.Minute)
	f.svc.SweepOnce(context.Background())
	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, r.Status, "still inside the 15-minute grace")

	f.clock.Advance(6 * time.Minute)
	f.svc.SweepOnce(context.Background())
	r, err = f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, r.Status)
	assert.Contains(t, r.FailureReason, "15 minutes")
}

func TestSweepDeployingFailureHandsTheCardBack(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	f.parked.parked[task.ID] = task
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchFailure, Detail: "job deploy concluded failure"})

	f.svc.SweepOnce(context.Background())

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, r.Status)
	assert.Equal(t, "job deploy concluded failure", r.FailureReason)
	require.NotNil(t, r.Deploy)
	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, task.ID, f.waker.calls[0].task.ID, "the parked card must be the one woken")
	assert.Contains(t, f.parked.taken, task.ID)
}

func TestSweepDeployingSuccessMovesToVerifyingWithSmokeAndHealth(t *testing.T) {
	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	f := newSweepFixture()
	f.legacy.target = domain.DeployTarget{BaseURL: server.URL, HealthURL: server.URL + "/health"}
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := deployingRelease(repositoryID, f.clock.Now())
	r.Profile.Verify.Smoke = []domain.SmokeCheck{{Method: "GET", Path: "/smoke"}}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	require.NotNil(t, got.DeployedAt)
	require.NotNil(t, got.VerifyUntil)
	require.Len(t, got.Checks.Smoke, 1)
	assert.True(t, got.Checks.Smoke[0].OK)
	require.Len(t, got.Checks.Health, 1)
	assert.True(t, got.Checks.Health[0].OK)
	assert.ElementsMatch(t, []string{"/smoke", "/health"}, gotPaths)
	assert.Empty(t, f.waker.calls, "a clean success stays watched, no hand-back yet")
}

func TestSweepDeployingSuccessWithAFailingSmokeCheckGoesStraightToAwaitingVerdict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	f := newSweepFixture()
	f.legacy.target = domain.DeployTarget{BaseURL: server.URL}
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := deployingRelease(repositoryID, f.clock.Now())
	r.Profile.Verify.Smoke = []domain.SmokeCheck{{Method: "GET", Path: "/smoke"}}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.set(mergeSHA, "deploy.yml", domain.DeployWatchStatus{State: domain.DeployWatchSuccess})

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
	assert.Contains(t, got.Checks.EarlyStop, "smoke check failed")
	assert.Len(t, f.waker.calls, 1)
}

func verifyingRelease(repositoryID uuid.UUID, deployedAt, until time.Time) domain.Release {
	return domain.Release{
		RepositoryID: repositoryID,
		Mode:         domain.DeliveryOnMerge,
		Executor:     domain.ExecutorGitHubActions,
		Status:       domain.ReleaseVerifying,
		CommitSHA:    mergeSHA,
		Profile:      deliveryProfile(domain.DeliveryOnMerge, domain.ExecutorGitHubActions),
		DeployedAt:   &deployedAt,
		VerifyUntil:  &until,
	}
}

func TestSweepVerifyingEarlyStopsAfterTwoConsecutiveFailedHealthChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now(), f.clock.Now().Add(10*time.Minute))
	r.Checks.HealthURL = server.URL
	r.Checks.Health = []domain.HealthSample{{At: f.clock.Now(), OK: false, Status: 503}}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
	assert.Equal(t, "health check failed twice", got.Checks.EarlyStop)
	require.Len(t, f.waker.calls, 1)
}

func TestSweepVerifyingEarlyStopsOnTooManyNewErrorGroups(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	envID := uuid.New()
	r := verifyingRelease(repositoryID, f.clock.Now(), f.clock.Now().Add(10*time.Minute))
	r.Checks.EnvironmentID = &envID
	r.Profile.Verify.MaxNewErrors = 1
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.envs.errorGroups[envID] = []domain.RuntimeErrorGroup{
		{Fingerprint: "a", New: true},
		{Fingerprint: "b", New: true},
	}

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
	assert.Contains(t, got.Checks.EarlyStop, "2 new runtime error groups")
	require.Len(t, f.waker.calls, 1)
}

func TestSweepVerifyingMovesToAwaitingVerdictWhenTheWindowEnds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now().Add(-11*time.Minute), f.clock.Now().Add(-1*time.Minute))
	r.Checks.BaseURL = server.URL
	r.Profile.Verify.Smoke = []domain.SmokeCheck{{Method: "GET", Path: "/smoke"}}
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseAwaitingVerdict, got.Status)
	require.Len(t, got.Checks.Smoke, 1, "the window-end sweep must run one more smoke round")
	require.Len(t, f.waker.calls, 1)
}

func TestSweepVerifyingStaysWatchedInsideTheWindowWithNoProblems(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	r := verifyingRelease(repositoryID, f.clock.Now(), f.clock.Now().Add(10*time.Minute))
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.SweepOnce(context.Background())

	got, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, got.Status)
	assert.Empty(t, f.waker.calls)
}
