package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

type ReleaseStoreSuite struct {
	suite.Suite
	ctx     context.Context
	cancel  context.CancelFunc
	pg      *database.Embedded
	pool    *pgxpool.Pool
	db      *postgres.DB
	store   *postgres.ReleaseStore
	tasks   *postgres.BoardTaskStore
	models  *postgres.ProjectModelStore
	repos   *postgres.RepositoryStore
	repoID  uuid.UUID
	compID  uuid.UUID
	compID2 uuid.UUID
}

func TestReleaseStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ReleaseStoreSuite))
}

func (s *ReleaseStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewReleaseStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.models = postgres.NewProjectModelStore(s.db)
	s.repos = postgres.NewRepositoryStore(s.db)
}

func (s *ReleaseStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repo, err := s.repos.Create(s.ctx, "release-store-test-"+suffix, "", "/tmp/release-store-test-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID

	comp, err := s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoID, Path: "."})
	s.Require().NoError(err)
	s.compID = comp.ID

	comp2, err := s.models.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoID, Path: "apps/other"})
	s.Require().NoError(err)
	s.compID2 = comp2.ID
}

func (s *ReleaseStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ReleaseStoreSuite) newTask(column domain.TaskColumn) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "release-store-test",
		TaskType:     "task",
		Column:       column,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

func testProfile() domain.ComponentDelivery {
	return domain.ComponentDelivery{
		Mode:     domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions,
		Workflow: "deploy.yml",
		Verify:   domain.DeliveryVerify{SoakMinutes: 10, MaxNewErrors: 1, Smoke: []domain.SmokeCheck{{Method: "GET", Path: "/health"}}},
	}
}

// Create writes the release and its starting tasks atomically; Get reads them
// back exactly, with every JSONB field (profile, deploy, checks, rollback)
// round-tripping and Tasks filled from the join rather than left empty.
func (s *ReleaseStoreSuite) TestCreateAndGetRoundTrip() {
	task := s.newTask(domain.TaskColumnDone)
	deployStarted := time.Now().UTC().Truncate(time.Millisecond)
	deploy := &domain.DeployWatchStatus{
		TaskID: task.ID, RepositoryID: s.repoID, State: domain.DeployWatchSuccess, Signal: domain.DeploySignalActionsRun,
	}
	checks := domain.ReleaseChecks{
		HealthURL: "https://api.example.com/health",
		Health:    []domain.HealthSample{{At: deployStarted, OK: true, Status: 200}},
		Notes:     []string{"no bound environment — runtime errors not read"},
	}
	rollback := &domain.ReleaseRollback{
		Reason: domain.RollbackVerifyFailed, Mechanism: domain.RollbackMechanismRevert, RevertSHA: "revertsha",
		StartedAt: deployStarted,
	}

	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID:    s.repoID,
		ComponentID:     &s.compID,
		Version:         "abc1234",
		Mode:            domain.DeliveryOnMerge,
		Executor:        domain.ExecutorGitHubActions,
		Status:          domain.ReleaseVerifying,
		CommitSHA:       "abc1234def5678",
		Tag:             "release/abc1234",
		Notes:           "opened at merge",
		Profile:         testProfile(),
		Deploy:          deploy,
		Checks:          checks,
		Rollback:        rollback,
		FailureReason:   "",
		DeployStartedAt: &deployStarted,
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.False(created.CreatedAt.IsZero())
	s.False(created.UpdatedAt.IsZero())
	s.Require().Len(created.Tasks, 1)
	s.Equal(task.ID, created.Tasks[0].ID)
	s.Equal(task.Key, created.Tasks[0].Key)
	s.Equal(domain.TaskColumnDone, created.Tasks[0].Column)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)
	s.Equal(testProfile(), got.Profile)
	s.Require().NotNil(got.Deploy)
	s.Equal(domain.DeployWatchSuccess, got.Deploy.State)
	s.Equal(checks.HealthURL, got.Checks.HealthURL)
	s.Require().Len(got.Checks.Health, 1)
	s.Require().NotNil(got.Rollback)
	s.Equal(domain.RollbackVerifyFailed, got.Rollback.Reason)
}

// A release opened with no component and no deploy/rollback evidence yet
// round-trips those as nil/zero, not as JSON null errors.
func (s *ReleaseStoreSuite) TestCreateWithoutComponentOrDeployRoundTrips() {
	task := s.newTask(domain.TaskColumnDone)
	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID,
		Version:      "def5678",
		Mode:         domain.DeliveryNone,
		Status:       domain.ReleaseReleased,
		CommitSHA:    "def5678abc123",
		Profile:      domain.ComponentDelivery{Mode: domain.DeliveryNone},
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)
	s.Nil(created.ComponentID)
	s.Nil(created.Deploy)
	s.Nil(created.Rollback)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Nil(got.ComponentID)
	s.Nil(got.Deploy)
	s.Nil(got.Rollback)
}

func (s *ReleaseStoreSuite) TestGetUnknownIDReturnsErrReleaseNotFound() {
	_, err := s.store.Get(s.ctx, uuid.New())
	s.Require().Error(err)
	s.True(errors.Is(err, domain.ErrReleaseNotFound))
}

// Update only writes when the stored status still equals expect: a wrong
// expect must change nothing and report ErrReleaseWrongStatus rather than
// silently applying, since that guard is the only thing stopping two sweeps
// (or a sweep racing an agent's finish/rollback) from both advancing the same
// release.
func (s *ReleaseStoreSuite) TestUpdateIsConditionalOnExpectedStatus() {
	task := s.newTask(domain.TaskColumnDone)
	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "aaa1111", Mode: domain.DeliveryDispatch,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleasePending, CommitSHA: "aaa1111bbb",
		Profile: testProfile(),
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)

	deploying := created
	deploying.Status = domain.ReleaseDeploying
	deploying.Tag = "release/aaa1111"
	now := time.Now().UTC().Truncate(time.Millisecond)
	deploying.DeployStartedAt = &now
	updated, err := s.store.Update(s.ctx, deploying, domain.ReleasePending)
	s.Require().NoError(err)
	s.Equal(domain.ReleaseDeploying, updated.Status)
	s.Equal("release/aaa1111", updated.Tag)

	// The stored status is now deploying; expecting pending again must be
	// refused, and refused without changing anything.
	stale := updated
	stale.Status = domain.ReleaseFailed
	_, err = s.store.Update(s.ctx, stale, domain.ReleasePending)
	s.Require().Error(err)
	s.True(errors.Is(err, domain.ErrReleaseWrongStatus))

	unchanged, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(domain.ReleaseDeploying, unchanged.Status, "the losing update must not have applied")
}

func (s *ReleaseStoreSuite) TestUpdateUnknownIDReturnsErrReleaseNotFound() {
	_, err := s.store.Update(s.ctx, domain.Release{ID: uuid.New(), Status: domain.ReleaseDeploying}, domain.ReleasePending)
	s.Require().Error(err)
	s.True(errors.Is(err, domain.ErrReleaseNotFound))
}

// ForTask is the newest release carrying the task: a superseding release
// carries the old one's tasks forward (AddTasks), so two rows in release_tasks
// can point at the same task and the newer release must win.
func (s *ReleaseStoreSuite) TestForTaskReturnsTheNewestRelease() {
	task := s.newTask(domain.TaskColumnDone)
	older, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "old0001", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseSuperseded, CommitSHA: "old0001aaa",
		Profile: testProfile(),
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)

	time.Sleep(10 * time.Millisecond)

	newer, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "new0002", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseDeploying, CommitSHA: "new0002bbb",
		Profile: testProfile(),
	}, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.store.AddTasks(s.ctx, newer.ID, []uuid.UUID{task.ID}))

	got, err := s.store.ForTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Equal(newer.ID, got.ID)
	s.NotEqual(older.ID, got.ID)
}

func (s *ReleaseStoreSuite) TestForTaskUnknownTaskReturnsErrReleaseNotFound() {
	_, err := s.store.ForTask(s.ctx, uuid.New())
	s.Require().Error(err)
	s.True(errors.Is(err, domain.ErrReleaseNotFound))
}

// List filters by repository, component, task (via the release_tasks EXISTS)
// and statuses, newest first.
func (s *ReleaseStoreSuite) TestListFilters() {
	taskA := s.newTask(domain.TaskColumnDone)
	taskB := s.newTask(domain.TaskColumnDone)

	rel1, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "list0001", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseReleased, CommitSHA: "list0001aaa",
		Profile: testProfile(),
	}, []uuid.UUID{taskA.ID})
	s.Require().NoError(err)
	time.Sleep(10 * time.Millisecond)

	rel2, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID2, Version: "list0002", Mode: domain.DeliveryDispatch,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseFailed, CommitSHA: "list0002bbb",
		Profile: testProfile(),
	}, []uuid.UUID{taskB.ID})
	s.Require().NoError(err)

	byComponent, err := s.store.List(s.ctx, domain.ReleaseListFilter{RepositoryID: &s.repoID, ComponentID: &s.compID})
	s.Require().NoError(err)
	s.Require().Len(byComponent, 1)
	s.Equal(rel1.ID, byComponent[0].ID)

	byTask, err := s.store.List(s.ctx, domain.ReleaseListFilter{RepositoryID: &s.repoID, TaskID: &taskB.ID})
	s.Require().NoError(err)
	s.Require().Len(byTask, 1)
	s.Equal(rel2.ID, byTask[0].ID)

	byStatus, err := s.store.List(s.ctx, domain.ReleaseListFilter{
		RepositoryID: &s.repoID, Statuses: []domain.ReleaseStatus{domain.ReleaseFailed},
	})
	s.Require().NoError(err)
	s.Require().Len(byStatus, 1)
	s.Equal(rel2.ID, byStatus[0].ID)

	all, err := s.store.List(s.ctx, domain.ReleaseListFilter{RepositoryID: &s.repoID})
	s.Require().NoError(err)
	s.Require().Len(all, 2)
	s.Equal(rel2.ID, all[0].ID, "newest first")
	s.Equal(rel1.ID, all[1].ID)

	limited, err := s.store.List(s.ctx, domain.ReleaseListFilter{RepositoryID: &s.repoID, Limit: 1})
	s.Require().NoError(err)
	s.Require().Len(limited, 1)
	s.Equal(rel2.ID, limited[0].ID)
}

// LastReleased is what a rollback redeploys: the newest `released` release of
// the SAME component finished before the release being rolled back, with NULL
// component matching only NULL component (IS NOT DISTINCT FROM).
func (s *ReleaseStoreSuite) TestLastReleasedMatchesComponentAndBeforeTime() {
	taskA := s.newTask(domain.TaskColumnReleased)
	taskB := s.newTask(domain.TaskColumnReleased)
	taskC := s.newTask(domain.TaskColumnReleased)

	base := time.Now().UTC().Truncate(time.Millisecond)
	early := base.Add(-2 * time.Hour)
	late := base.Add(-1 * time.Hour)

	earlyRel, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "early01", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseReleased, CommitSHA: "early01aaa",
		Profile: testProfile(), FinishedAt: &early,
	}, []uuid.UUID{taskA.ID})
	s.Require().NoError(err)

	lateRel, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "late01", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseReleased, CommitSHA: "late01bbb",
		Profile: testProfile(), FinishedAt: &late,
	}, []uuid.UUID{taskB.ID})
	s.Require().NoError(err)

	// A released release of a DIFFERENT component, finished even later, must
	// never be picked for compID's rollback.
	_, err = s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID2, Version: "other01", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseReleased, CommitSHA: "other01ccc",
		Profile: testProfile(), FinishedAt: &base,
	}, []uuid.UUID{taskC.ID})
	s.Require().NoError(err)

	got, err := s.store.LastReleased(s.ctx, s.repoID, &s.compID, base)
	s.Require().NoError(err)
	s.Equal(lateRel.ID, got.ID, "the newest released release before `base`")

	gotBeforeLate, err := s.store.LastReleased(s.ctx, s.repoID, &s.compID, late)
	s.Require().NoError(err)
	s.Equal(earlyRel.ID, gotBeforeLate.ID, "strictly before — the late release itself must not match")

	_, err = s.store.LastReleased(s.ctx, s.repoID, &s.compID, early)
	s.Require().Error(err)
	s.True(errors.Is(err, domain.ErrReleaseNotFound), "nothing released before the earliest one")
}

// AddTasks is idempotent (a supersede and the release being superseded can
// both try to carry the same task forward) and RemoveTask actually drops the
// membership row rather than just hiding it.
func (s *ReleaseStoreSuite) TestAddTasksIdempotentAndRemoveTask() {
	taskA := s.newTask(domain.TaskColumnDone)
	taskB := s.newTask(domain.TaskColumnDone)

	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "add0001", Mode: domain.DeliveryOnMerge,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseDeploying, CommitSHA: "add0001aaa",
		Profile: testProfile(),
	}, []uuid.UUID{taskA.ID})
	s.Require().NoError(err)

	s.Require().NoError(s.store.AddTasks(s.ctx, created.ID, []uuid.UUID{taskB.ID}))
	s.Require().NoError(s.store.AddTasks(s.ctx, created.ID, []uuid.UUID{taskB.ID}), "a repeat add must not error")

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Require().Len(got.Tasks, 2, "the repeat add must not duplicate the membership row")

	s.Require().NoError(s.store.RemoveTask(s.ctx, created.ID, taskA.ID))
	after, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Require().Len(after.Tasks, 1)
	s.Equal(taskB.ID, after.Tasks[0].ID)
}

func batchTestProfile(executor domain.DeliveryExecutor) domain.ComponentDelivery {
	return domain.ComponentDelivery{
		Mode:         domain.DeliveryBatch,
		Executor:     executor,
		TagPattern:   "v{version}",
		LocalCommand: "./release.sh {version}",
		Verify:       domain.DeliveryVerify{SoakMinutes: 10},
	}
}

// A draft carries no local_run/store_builds/cut_at yet: they must round-trip
// as nil/empty, not as JSON null errors — the same shape
// TestCreateWithoutComponentOrDeployRoundTrips checks for deploy/rollback.
func (s *ReleaseStoreSuite) TestDraftHasNoLocalRunStoreBuildsOrCutAt() {
	task := s.newTask(domain.TaskColumnDone)
	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Mode: domain.DeliveryBatch,
		Executor: domain.ExecutorLocal, Status: domain.ReleaseDraft,
		Profile: batchTestProfile(domain.ExecutorLocal),
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)
	s.Nil(created.LocalRun)
	s.Empty(created.StoreBuilds)
	s.Nil(created.CutAt)

	got, err := s.store.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Nil(got.LocalRun)
	s.Empty(got.StoreBuilds)
	s.Nil(got.CutAt)
}

// Cutting and deploying a batch release writes local_run, store_builds and
// cut_at, and a later Update (the local runner's completion callback, the
// store sweeper recording a new build) must round-trip every field of both.
func (s *ReleaseStoreSuite) TestLocalRunAndStoreBuildsAndCutAtRoundTrip() {
	task := s.newTask(domain.TaskColumnDone)
	cutAt := time.Now().UTC().Truncate(time.Millisecond)
	created, err := s.store.Create(s.ctx, domain.Release{
		RepositoryID: s.repoID, ComponentID: &s.compID, Version: "1.2.0", Mode: domain.DeliveryBatch,
		Executor: domain.ExecutorLocal, Status: domain.ReleasePending, CommitSHA: "cut0001aaa", Tag: "v1.2.0",
		Profile: batchTestProfile(domain.ExecutorLocal), CutAt: &cutAt,
	}, []uuid.UUID{task.ID})
	s.Require().NoError(err)
	s.Require().NotNil(created.CutAt)
	s.WithinDuration(cutAt, *created.CutAt, time.Second)

	startedAt := time.Now().UTC().Truncate(time.Millisecond)
	deploying := created
	deploying.Status = domain.ReleaseDeploying
	deploying.LocalRun = &domain.ReleaseLocalRun{
		Argv: []string{"./release.sh", "1.2.0"}, LogPath: "/data/releases/x.log", StartedAt: startedAt,
	}
	updated, err := s.store.Update(s.ctx, deploying, domain.ReleasePending)
	s.Require().NoError(err)
	s.Require().NotNil(updated.LocalRun)
	s.Equal([]string{"./release.sh", "1.2.0"}, updated.LocalRun.Argv)
	s.Nil(updated.LocalRun.ExitCode)

	finishedAt := startedAt.Add(time.Minute)
	exitCode := 0
	completed := updated
	completed.LocalRun.ExitCode = &exitCode
	completed.LocalRun.FinishedAt = &finishedAt
	completed.LocalRun.Tail = "build ok\npublished"
	completed.Deploy = &domain.DeployWatchStatus{State: domain.DeployWatchSuccess, Signal: "local_run"}
	final, err := s.store.Update(s.ctx, completed, domain.ReleaseDeploying)
	s.Require().NoError(err)

	got, err := s.store.Get(s.ctx, final.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got.LocalRun)
	s.Require().NotNil(got.LocalRun.ExitCode)
	s.Equal(0, *got.LocalRun.ExitCode)
	s.Require().NotNil(got.LocalRun.FinishedAt)
	s.WithinDuration(finishedAt, *got.LocalRun.FinishedAt, time.Second)
	s.Equal("build ok\npublished", got.LocalRun.Tail)
	s.Require().NotNil(got.Deploy)
	s.Equal(domain.DeployWatchSuccess, got.Deploy.State)

	storeBuilds := got
	storeBuilds.StoreBuilds = []domain.ReleaseStoreBuild{
		{Platform: "ios", Engine: "github_actions", BaselineBuild: "10", Build: "11"},
		{Platform: "android", Error: "no engine available"},
	}
	storeBuilds.Status = domain.ReleaseVerifying
	savedBuilds, err := s.store.Update(s.ctx, storeBuilds, domain.ReleaseDeploying)
	s.Require().NoError(err)
	s.Require().Len(savedBuilds.StoreBuilds, 2)

	gotBuilds, err := s.store.Get(s.ctx, final.ID)
	s.Require().NoError(err)
	s.Require().Len(gotBuilds.StoreBuilds, 2)
	s.Equal("11", gotBuilds.StoreBuilds[0].Build)
	s.Equal("no engine available", gotBuilds.StoreBuilds[1].Error)
}
