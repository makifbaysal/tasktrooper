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

type storeFixture struct {
	svc      *Service
	store    *fakeReleaseStore
	storeOps *fakeStoreOps
	repos    *fakeRepos
	waker    *fakeWaker
	tasks    *fakeTasks
	parked   *fakeParked
	clock    *steppableClock
}

func newStoreFixture() *storeFixture {
	store := newFakeReleaseStore()
	storeOps := newFakeStoreOps()
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "mobile", RootPath: "/repos/mobile"}}
	waker := &fakeWaker{}
	tasks := newFakeTasks()
	parked := newFakeParked()
	clock := newSteppableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Store: store, StoreOps: storeOps, Repos: repos, Waker: waker, Tasks: tasks, ParkedTasks: parked,
		Clock: clock.Now,
	})
	return &storeFixture{svc: svc, store: store, storeOps: storeOps, repos: repos, waker: waker, tasks: tasks, parked: parked, clock: clock}
}

func TestDeployBatchStoreRecordsBaselineAndStartsEveryLinkedPlatform(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	f.storeOps.apps[repositoryID] = []domain.MobileStoreApp{
		{Platform: "ios", Identifier: "com.acme.app"},
		{Platform: "android", Identifier: "com.acme.app"},
	}
	f.storeOps.setTracks(repositoryID, "ios", domain.StoreTracks{Internal: domain.TrackRelease{Build: "10"}})
	f.storeOps.setTracks(repositoryID, "android", domain.StoreTracks{Internal: domain.TrackRelease{Build: "20"}})

	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	require.Len(t, updated.StoreBuilds, 2)
	byPlatform := map[string]domain.ReleaseStoreBuild{}
	for _, b := range updated.StoreBuilds {
		byPlatform[b.Platform] = b
	}
	assert.Equal(t, "10", byPlatform["ios"].BaselineBuild)
	assert.Equal(t, "20", byPlatform["android"].BaselineBuild)
	assert.Len(t, f.storeOps.startCalls, 2)
}

func TestDeployBatchStorePartialStartFailureKeepsGoingDeploying(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	f.storeOps.apps[repositoryID] = []domain.MobileStoreApp{
		{Platform: "ios", Identifier: "com.acme.app"},
		{Platform: "android", Identifier: "com.acme.app"},
	}
	f.storeOps.startErr["android"] = errors.New("no android engine available")

	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, updated.Status, "one working platform is enough to keep going")

	var androidBuild, iosBuild domain.ReleaseStoreBuild
	for _, b := range updated.StoreBuilds {
		if b.Platform == "android" {
			androidBuild = b
		} else {
			iosBuild = b
		}
	}
	assert.NotEmpty(t, androidBuild.Error)
	assert.Empty(t, iosBuild.Error)
}

func TestDeployBatchStoreFailsWhenEveryPlatformFailsToStart(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	f.storeOps.apps[repositoryID] = []domain.MobileStoreApp{
		{Platform: "ios", Identifier: "com.acme.app"},
	}
	f.storeOps.startErr["ios"] = errors.New("no engine available")

	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err, "a store start failure is recorded on the release, not returned as an error")
	assert.Equal(t, domain.ReleaseFailed, updated.Status)
	assert.NotEmpty(t, updated.FailureReason)
	assert.Len(t, f.waker.calls, 1)
}

func TestSweepDeployingStoreSucceedsWhenEveryStartedPlatformHasANewBuild(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task

	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	created.StoreBuilds = []domain.ReleaseStoreBuild{
		{Platform: "ios", BaselineBuild: "10"},
		{Platform: "android", Error: "never started"},
	}
	created.Status = domain.ReleaseDeploying
	now := f.clock.Now()
	created.DeployStartedAt = &now
	f.store.releases[created.ID] = created

	f.storeOps.setTracks(repositoryID, "ios", domain.StoreTracks{Internal: domain.TrackRelease{Build: "11"}})

	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseVerifying, after.Status)
	for _, b := range after.StoreBuilds {
		if b.Platform == "ios" {
			assert.Equal(t, "11", b.Build)
		}
	}
}

func TestSweepDeployingStoreWaitsWhenABuildHasNotChanged(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)
	created.StoreBuilds = []domain.ReleaseStoreBuild{{Platform: "ios", BaselineBuild: "10"}}
	created.Status = domain.ReleaseDeploying
	now := f.clock.Now()
	created.DeployStartedAt = &now
	f.store.releases[created.ID] = created
	f.storeOps.setTracks(repositoryID, "ios", domain.StoreTracks{Internal: domain.TrackRelease{Build: "10"}})

	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, after.Status)
}

func TestSweepDeployingStoreFailsAfter120Minutes(t *testing.T) {
	f := newStoreFixture()
	repositoryID := f.repos.repo.ID
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	created, err := f.store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)
	created.StoreBuilds = []domain.ReleaseStoreBuild{{Platform: "ios", BaselineBuild: "10"}}
	created.Status = domain.ReleaseDeploying
	started := f.clock.Now()
	created.DeployStartedAt = &started
	f.store.releases[created.ID] = created
	f.storeOps.setTracks(repositoryID, "ios", domain.StoreTracks{Internal: domain.TrackRelease{Build: "10"}})

	f.clock.Advance(121 * time.Minute)
	f.svc.SweepOnce(context.Background())

	after, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, after.Status)
	assert.Contains(t, after.FailureReason, "ios")
	assert.Len(t, f.waker.calls, 1)
}
