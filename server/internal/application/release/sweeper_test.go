package release

import (
	"context"
	"errors"
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

// a status-lookup error must not reset the deploy timeout — the release
// must still fail once 60 minutes have elapsed since DeployStartedAt, purely
// from elapsed time, even though every sweep in between errored.
func TestSweepDeployingFailsAfterSixtyMinutesWhenStatusLookupKeepsErroring(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.err = errors.New("actions api unavailable")

	f.clock.Advance(61 * time.Minute)
	f.svc.SweepOnce(context.Background())

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, r.Status)
	assert.Contains(t, r.FailureReason, "60 minutes")
	assert.Contains(t, r.FailureReason, "actions api unavailable")
	require.Len(t, f.waker.calls, 1)
}

func TestSweepDeployingKeepsWaitingWhenStatusLookupErrorsUnderTheTimeout(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), deployingRelease(repositoryID, f.clock.Now()), []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.ds.err = errors.New("actions api unavailable")

	f.clock.Advance(30 * time.Minute)
	f.svc.SweepOnce(context.Background())

	r, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, r.Status)
	assert.Empty(t, f.waker.calls)
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

// M2(a): a card parked on release_watch whose release already settled (a
// race between the sweeper and Watch, a crash mid hand-back) must be freed
// and woken immediately, not left stranded until a human notices.
func TestSweepFreesAStrandedReleaseWatchPark(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	_, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)
	f.parked.parked[task.ID] = task

	f.svc.SweepOnce(context.Background())

	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, task.ID, f.waker.calls[0].task.ID)
	assert.Contains(t, f.parked.taken, task.ID)
}

// M2(b)/N5: an awaiting_verdict/failed release with no parked card (the agent
// run that would park it is gone, or its hand-back dispatch was dropped) is
// re-woken on a 10-minute cadence, not left to wait forever. The bookkeeping
// is read off the release row (HandBackCount/LastHandBackAt), not a
// process-memory map, so the fixture simulates "already handed back once,
// eleven minutes ago" directly on the stored release.
func TestSweepRewakesAnUnparkedAwaitingVerdictReleaseAfterTenMinutes(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)
	past := f.clock.Now().Add(-11 * time.Minute)
	created.HandBackCount = 1
	created.LastHandBackAt = &past
	created.UpdatedAt = f.clock.Now()
	f.store.releases[created.ID] = created

	f.svc.SweepOnce(context.Background())
	require.Len(t, f.waker.calls, 1, "more than ten minutes past the last hand-back, the watchdog re-wakes it")

	f.svc.SweepOnce(context.Background())
	assert.Len(t, f.waker.calls, 1, "a sweep inside the fresh 10-minute cool-down must not re-wake it again")

	f.clock.Advance(10 * time.Minute)
	f.svc.SweepOnce(context.Background())
	assert.Len(t, f.waker.calls, 2, "past the cool-down again, the watchdog re-wakes it")
}

// M2(b)/N5: the re-wake is capped at six times per release so a release that
// never gets a verdict does not wake the agent forever; the cap is
// HandBackCount on the row, so a release that already has five recorded
// hand-backs gets exactly one more before the watchdog stops.
func TestSweepStopsRewakingAnAwaitingVerdictReleaseAfterSixTimes(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)
	past := f.clock.Now().Add(-11 * time.Minute)
	created.HandBackCount = 5
	created.LastHandBackAt = &past
	created.UpdatedAt = f.clock.Now()
	f.store.releases[created.ID] = created

	f.svc.SweepOnce(context.Background())
	require.Len(t, f.waker.calls, 1, "the sixth hand-back still fires")

	f.clock.Advance(15 * time.Minute)
	f.svc.SweepOnce(context.Background())
	assert.Len(t, f.waker.calls, 1, "a release already re-woken six times must not be woken a seventh")
}

// A release that has never been handed back through the sweeper's own
// handBack (LastHandBackAt unset) is not something the watchdog invents a
// first hand-back for — a release only reaches awaiting_verdict/failed
// through a transition that already calls handBack itself.
func TestSweepDoesNotRewakeAReleaseThatWasNeverHandedBack(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	_, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)

	f.svc.SweepOnce(context.Background())
	assert.Empty(t, f.waker.calls, "nothing was ever handed back for this release; the watchdog must not invent one")
}

// Once an agent has looked at the release (AgentSeenAt, stamped by
// Service.ForAgent — every release tool call) since the last hand-back, the
// watchdog leaves it alone: whatever is slow is the agent's own turn, not a
// dropped dispatch.
func TestSweepDoesNotRewakeWhenTheAgentHasSeenItSinceTheHandBack(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)
	past := f.clock.Now().Add(-30 * time.Minute)
	seen := f.clock.Now().Add(-20 * time.Minute)
	created.HandBackCount = 1
	created.LastHandBackAt = &past
	created.AgentSeenAt = &seen
	created.UpdatedAt = f.clock.Now()
	f.store.releases[created.ID] = created

	f.svc.SweepOnce(context.Background())
	assert.Empty(t, f.waker.calls, "an agent already looked at it after the hand-back; the watchdog must not re-wake it")
}

// A release nothing has touched in more than a day is not something the
// watchdog keeps pestering forever either — by then it is someone else's job
// to notice, not the sweeper's.
func TestSweepDoesNotRewakeAReleaseNotUpdatedInTheLastDay(t *testing.T) {
	f := newSweepFixture()
	repositoryID := uuid.New()
	task := f.withTask(repositoryID)
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)
	past := f.clock.Now().Add(-30 * time.Minute)
	stale := f.clock.Now().Add(-25 * time.Hour)
	created.HandBackCount = 1
	created.LastHandBackAt = &past
	created.UpdatedAt = stale
	f.store.releases[created.ID] = created

	f.svc.SweepOnce(context.Background())
	assert.Empty(t, f.waker.calls, "a release untouched for more than 24 hours must not be woken by the watchdog")
}
