package release

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type openFixture struct {
	svc          *Service
	store        *fakeReleaseStore
	tasks        *fakeTasks
	parked       *fakeParked
	components   *fakeComponents
	repositoryID uuid.UUID
}

func newOpenFixture(tasks ...domain.BoardTask) *openFixture {
	store := newFakeReleaseStore()
	tk := newFakeTasks(tasks...)
	parked := newFakeParked()
	comps := newFakeComponents()
	svc := New(Deps{
		Store:       store,
		Tasks:       tk,
		ParkedTasks: parked,
		Components:  comps,
		Clock:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &openFixture{svc: svc, store: store, tasks: tk, parked: parked, components: comps}
}

func deliveryProfile(mode domain.DeliveryMode, executor domain.DeliveryExecutor) domain.ComponentDelivery {
	return domain.ComponentDelivery{
		Mode:     mode,
		Executor: executor,
		Workflow: "deploy.yml",
		Verify:   domain.DeliveryVerify{SoakMinutes: 10},
	}.Normalized()
}

func confirmedComponent(mode domain.DeliveryMode, executor domain.DeliveryExecutor) domain.Component {
	profile := deliveryProfile(mode, executor)
	return domain.Component{
		ID:     uuid.New(),
		Path:   "api",
		Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{
			Override: &profile,
		},
	}
}

const mergeSHA = "abcdef0123456789abcdef0123456789abcdef01"

func TestOpenForMergeNoComponentIsUnconfirmed(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	assert.True(t, opening.Unconfirmed)
	assert.Nil(t, opening.ReleaseID)
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "no component")
}

func TestOpenForMergeUnconfirmedDeliveryPostsOneComment(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := domain.Component{
		ID:     uuid.New(),
		Path:   "api",
		Status: domain.ComponentStatusActive,
		Name:   domain.Fact[string]{Override: strPtr("API")},
	}
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	assert.True(t, opening.Unconfirmed)
	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "not confirmed")
	assert.Contains(t, f.tasks.comments[0].Content, "API")
	assert.Empty(t, f.store.releases)
}

func TestOpenForMergeNoneMovesTaskToReleased(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryNone, "")
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	assert.True(t, opening.Released)
	assert.Equal(t, domain.DeliveryNone, opening.Mode)
	require.Len(t, f.tasks.updates, 1)
	assert.Equal(t, domain.TaskColumnReleased, *f.tasks.updates[0].Column)
	assert.Equal(t, domain.MoveReasonMergeReleasedNoDelivery, f.tasks.updates[0].SystemReason)
	assert.Empty(t, f.store.releases)
}

func TestOpenForMergeBatchQueuesTheTaskInADraftRelease(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryBatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	assert.Equal(t, domain.DeliveryBatch, opening.Mode)
	assert.False(t, opening.Released)
	require.NotNil(t, opening.ReleaseID)
	assert.Equal(t, domain.ReleaseDraft, opening.Status)
	assert.Contains(t, opening.Next, "Queued")
	assert.Contains(t, opening.Next, "human cuts it")
	assert.Empty(t, f.tasks.updates, "the task stays in done — no column move")

	draft, err := f.store.Get(context.Background(), *opening.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDraft, draft.Status)
	assert.Equal(t, domain.DeliveryBatch, draft.Mode)
	assert.Empty(t, draft.Version)
	require.Len(t, draft.Tasks, 1)
	assert.Equal(t, task.ID, draft.Tasks[0].ID)
}

func TestOpenForMergeBatchJoinsAnExistingDraft(t *testing.T) {
	first := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	second := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOpenFixture(first, second)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryBatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	first.ComponentID = &component.ID
	second.ComponentID = &component.ID
	f.tasks.tasks[first.ID] = first
	f.tasks.tasks[second.ID] = second

	firstOpening := f.svc.OpenForMerge(context.Background(), repositoryID, first, "1111111111111111111111111111111111111111")
	secondOpening := f.svc.OpenForMerge(context.Background(), repositoryID, second, "2222222222222222222222222222222222222222")

	require.NotNil(t, firstOpening.ReleaseID)
	require.NotNil(t, secondOpening.ReleaseID)
	assert.Equal(t, *firstOpening.ReleaseID, *secondOpening.ReleaseID, "both merges must join the same draft")
	assert.Len(t, f.store.releases, 1)

	draft, err := f.store.Get(context.Background(), *firstOpening.ReleaseID)
	require.NoError(t, err)
	assert.Len(t, draft.Tasks, 2)
}

func TestOpenForMergeOnMergeOpensADeployingRelease(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	require.NotNil(t, opening.ReleaseID)
	assert.Equal(t, domain.DeliveryOnMerge, opening.Mode)
	assert.Equal(t, domain.ReleaseDeploying, opening.Status)
	assert.Equal(t, "call watch_release", opening.Next)

	r, err := f.store.Get(context.Background(), *opening.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, r.Status)
	assert.NotNil(t, r.DeployStartedAt)
	assert.Equal(t, domain.ShortSHA(mergeSHA), r.Version)
	assert.Equal(t, mergeSHA, r.CommitSHA)
	require.Len(t, r.Tasks, 1)
	assert.Equal(t, task.ID, r.Tasks[0].ID)
}

func TestOpenForMergeDispatchOpensAPendingRelease(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	require.NotNil(t, opening.ReleaseID)
	assert.Equal(t, domain.ReleasePending, opening.Status)
	assert.Contains(t, opening.Next, "deploy_release")

	r, err := f.store.Get(context.Background(), *opening.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleasePending, r.Status)
	assert.Nil(t, r.DeployStartedAt)
}

func TestOpenForMergeSupersedesTheOpenReleaseAndCarriesItsTasks(t *testing.T) {
	oldTask := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	newTask := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOpenFixture(oldTask, newTask)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	oldTask.ComponentID = &component.ID
	newTask.ComponentID = &component.ID
	f.tasks.tasks[oldTask.ID] = oldTask
	f.tasks.tasks[newTask.ID] = newTask

	first := f.svc.OpenForMerge(context.Background(), repositoryID, oldTask, "1111111111111111111111111111111111111111")
	require.NotNil(t, first.ReleaseID)

	// Park the old release's card, the way a dispatch-mode release engineer
	// waiting on watch_release would leave it.
	f.parked.parked[oldTask.ID] = oldTask

	second := f.svc.OpenForMerge(context.Background(), repositoryID, newTask, "2222222222222222222222222222222222222222")
	require.NotNil(t, second.ReleaseID)

	old, err := f.store.Get(context.Background(), *first.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseSuperseded, old.Status)
	assert.Contains(t, old.Verdict, domain.ShortSHA("2222222222222222222222222222222222222222"))

	newer, err := f.store.Get(context.Background(), *second.ReleaseID)
	require.NoError(t, err)
	taskIDs := map[uuid.UUID]bool{}
	for _, tr := range newer.Tasks {
		taskIDs[tr.ID] = true
	}
	assert.True(t, taskIDs[oldTask.ID], "the superseded release's task must carry over")
	assert.True(t, taskIDs[newTask.ID])

	assert.Contains(t, f.parked.taken, oldTask.ID, "the old release's parked card must be un-parked silently")
	// Un-parked, not woken: no waker was even configured, and the take must
	// not have gone through Dispatch (the newer release's card is the one
	// that will be woken).
}

func TestOpenPendingOpensReleasesForAlreadyMergedDoneTasks(t *testing.T) {
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	merged := domain.BoardTask{
		ID: uuid.New(), Key: "T-9", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID,
		MergeCommitSHA: mergeSHA,
	}
	notMerged := domain.BoardTask{
		ID: uuid.New(), Key: "T-10", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID,
	}
	f := newOpenFixture(merged, notMerged)
	f.components.add(repositoryID, component)

	n, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Len(t, f.store.releases, 1)
}

// N6: the on_merge catch-up must ship the newest waiting task's own merge
// commit in GIT order, never RemoteHead (which can be ahead of what these
// tasks actually cover) and never mere board/list order (ListTasks makes no
// ordering promise). Both waiting tasks are merged; only git.ancestors says
// which is newer.
func TestOpenPendingOnMergeUsesGitOrderNotRemoteHeadForTheCatchUpCommit(t *testing.T) {
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	older := domain.BoardTask{
		ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID, MergeCommitSHA: "1111111111111111111111111111111111111111",
	}
	newer := domain.BoardTask{
		ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID, MergeCommitSHA: "2222222222222222222222222222222222222222",
	}
	f := newOpenFixture(older, newer)
	f.components.add(repositoryID, component)
	git := newFakeGit()
	git.ancestors[older.MergeCommitSHA] = true
	git.head = "9999999999999999999999999999999999999999" // must never be used for on_merge
	f.svc = New(Deps{
		Store: f.store, Tasks: f.tasks, ParkedTasks: f.parked, Components: f.components,
		Git: git, Repos: &fakeRepos{repo: domain.Repository{RootPath: "/repo"}},
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	n, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	require.Len(t, f.store.releases, 1)
	var release domain.Release
	for _, r := range f.store.releases {
		release = r
	}
	assert.Equal(t, newer.MergeCommitSHA, release.CommitSHA,
		"the newest waiting task's own merge commit must be used, never RemoteHead or board order")
}

// Create must insert the carried (older) task before the new one, so
// their added_at values (and the tasks slice they populate) preserve that
// order rather than whichever order a map or a tied timestamp happens to
// produce.
func TestOpenForMergeSupersedeCarriesOlderTaskFirstAndNewTaskLast(t *testing.T) {
	oldTask := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	newTask := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOpenFixture(oldTask, newTask)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	oldTask.ComponentID = &component.ID
	newTask.ComponentID = &component.ID
	f.tasks.tasks[oldTask.ID] = oldTask
	f.tasks.tasks[newTask.ID] = newTask

	first := f.svc.OpenForMerge(context.Background(), repositoryID, oldTask, "1111111111111111111111111111111111111111")
	require.NotNil(t, first.ReleaseID)
	second := f.svc.OpenForMerge(context.Background(), repositoryID, newTask, "2222222222222222222222222222222222222222")
	require.NotNil(t, second.ReleaseID)

	newer, err := f.store.Get(context.Background(), *second.ReleaseID)
	require.NoError(t, err)
	require.Len(t, newer.Tasks, 2)
	assert.Equal(t, oldTask.ID, newer.Tasks[0].ID, "the carried task must come first")
	assert.Equal(t, newTask.ID, newer.Tasks[1].ID, "the just-merged task must come last")
}

// openRelease must not supersede an open release whose commit is not an
// ancestor of the new merge — chaining onto an unrelated commit would ship
// (and judge) a release for a deploy it never carried.
func TestOpenForMergeDoesNotSupersedeAnOpenReleaseWhoseCommitIsNotAnAncestor(t *testing.T) {
	oldTask := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	newTask := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOpenFixture(oldTask, newTask)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	oldTask.ComponentID = &component.ID
	newTask.ComponentID = &component.ID
	f.tasks.tasks[oldTask.ID] = oldTask
	f.tasks.tasks[newTask.ID] = newTask

	repo := domain.Repository{ID: repositoryID, Name: "app", RootPath: "/repos/app"}
	git := newFakeGit() // ancestors left empty: nothing is reported as an ancestor of anything
	f.svc = New(Deps{
		Store: f.store, Tasks: f.tasks, ParkedTasks: f.parked, Components: f.components,
		Git: git, Repos: &fakeRepos{repo: repo},
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	oldSHA := "1111111111111111111111111111111111111111"
	newSHA := "2222222222222222222222222222222222222222"
	first := f.svc.OpenForMerge(context.Background(), repositoryID, oldTask, oldSHA)
	require.NotNil(t, first.ReleaseID)

	second := f.svc.OpenForMerge(context.Background(), repositoryID, newTask, newSHA)
	require.NotNil(t, second.ReleaseID)

	old, err := f.store.Get(context.Background(), *first.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleasePending, old.Status, "an unrelated commit must not supersede the open release")

	newer, err := f.store.Get(context.Background(), *second.ReleaseID)
	require.NoError(t, err)
	require.Len(t, newer.Tasks, 1, "the new release must carry only its own task, not the unrelated one")
	assert.Equal(t, newTask.ID, newer.Tasks[0].ID)
}

// The mirror of the above: when Git reports the new commit as a genuine
// descendant, the take-over still happens exactly as before the check.
func TestOpenForMergeSupersedesWhenTheNewCommitIsADescendant(t *testing.T) {
	oldTask := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	newTask := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOpenFixture(oldTask, newTask)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	oldTask.ComponentID = &component.ID
	newTask.ComponentID = &component.ID
	f.tasks.tasks[oldTask.ID] = oldTask
	f.tasks.tasks[newTask.ID] = newTask

	repo := domain.Repository{ID: repositoryID, Name: "app", RootPath: "/repos/app"}
	oldSHA := "1111111111111111111111111111111111111111"
	newSHA := "2222222222222222222222222222222222222222"
	git := newFakeGit()
	git.ancestors[oldSHA] = true
	f.svc = New(Deps{
		Store: f.store, Tasks: f.tasks, ParkedTasks: f.parked, Components: f.components,
		Git: git, Repos: &fakeRepos{repo: repo},
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	first := f.svc.OpenForMerge(context.Background(), repositoryID, oldTask, oldSHA)
	require.NotNil(t, first.ReleaseID)
	second := f.svc.OpenForMerge(context.Background(), repositoryID, newTask, newSHA)
	require.NotNil(t, second.ReleaseID)

	old, err := f.store.Get(context.Background(), *first.ReleaseID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseSuperseded, old.Status)

	newer, err := f.store.Get(context.Background(), *second.ReleaseID)
	require.NoError(t, err)
	require.Len(t, newer.Tasks, 2)
}

// Create runs before the old release is marked superseded; if that
// supersede loses the race (something else already moved the old release on),
// the old release's current tasks must still be carried into the new one.
func TestSupersedeReleaseCarriesTasksForwardWhenTheRaceIsLost(t *testing.T) {
	f := newOpenFixture()
	repositoryID := uuid.New()
	oldTask := domain.BoardTask{ID: uuid.New()}
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, []uuid.UUID{oldTask.ID})
	require.NoError(t, err)

	newRelease, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, nil)
	require.NoError(t, err)

	// Something else moved the old release on before the supersede lands.
	raced := created
	raced.Status = domain.ReleaseFailed
	_, err = f.store.Update(context.Background(), raced, domain.ReleasePending)
	require.NoError(t, err)

	f.svc.supersedeRelease(context.Background(), created, newRelease.ID, "v2")

	got, err := f.store.Get(context.Background(), newRelease.ID)
	require.NoError(t, err)
	taskIDs := map[uuid.UUID]bool{}
	for _, tr := range got.Tasks {
		taskIDs[tr.ID] = true
	}
	assert.True(t, taskIDs[oldTask.ID], "the raced-superseded release's task must still be carried forward")

	stillFailed, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseFailed, stillFailed.Status, "the race winner's status must not be overwritten")
}

// N12: when the lost-supersede race lands the old release in a TERMINAL
// status (released/rolled_back/superseded), it already has its own
// resolution for its tasks — pulling them into the new release too would
// contradict that resolution, so they must NOT be carried forward.
func TestSupersedeReleaseDoesNotCarryTasksWhenTheRaceWinnerIsTerminal(t *testing.T) {
	f := newOpenFixture()
	repositoryID := uuid.New()
	oldTask := domain.BoardTask{ID: uuid.New()}
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, []uuid.UUID{oldTask.ID})
	require.NoError(t, err)

	newRelease, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, nil)
	require.NoError(t, err)

	// Something else already finished the old release before the supersede
	// lands — its task was already moved to released by that Finish.
	raced := created
	raced.Status = domain.ReleaseReleased
	now := time.Now()
	raced.FinishedAt = &now
	_, err = f.store.Update(context.Background(), raced, domain.ReleasePending)
	require.NoError(t, err)

	f.svc.supersedeRelease(context.Background(), created, newRelease.ID, "v2")

	got, err := f.store.Get(context.Background(), newRelease.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Tasks, "a terminal race winner's tasks must not be pulled into the new release")

	stillReleased, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseReleased, stillReleased.Status, "the race winner's own terminal status must not be overwritten")
}

// N12: when the race winner is still Open() (a sweep transition landed
// between openReleaseToSupersede's read and the CAS, not a resolution), the
// supersede transition is retried once, and it succeeds this time.
func TestSupersedeReleaseRetriesOnceWhenTheRaceWinnerIsStillOpen(t *testing.T) {
	f := newOpenFixture()
	repositoryID := uuid.New()
	oldTask := domain.BoardTask{ID: uuid.New()}
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, []uuid.UUID{oldTask.ID})
	require.NoError(t, err)

	newRelease, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleasePending,
	}, nil)
	require.NoError(t, err)

	// A sweep moved it from pending to deploying — still Open(), not a
	// resolution — between openReleaseToSupersede's read and this CAS.
	raced := created
	raced.Status = domain.ReleaseDeploying
	_, err = f.store.Update(context.Background(), raced, domain.ReleasePending)
	require.NoError(t, err)

	f.svc.supersedeRelease(context.Background(), created, newRelease.ID, "v2")

	stillOpen, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseSuperseded, stillOpen.Status, "the retry must have superseded it on the second attempt")
}

// every waiting task of the component must land in ONE release, not one
// release per task, and the release engineer is woken once.
func TestOpenPendingOpensOneReleaseForEveryWaitingTaskAndWakesOnce(t *testing.T) {
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	taskA := domain.BoardTask{
		ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID, MergeCommitSHA: "1111111111111111111111111111111111111111",
	}
	taskB := domain.BoardTask{
		ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID, MergeCommitSHA: "2222222222222222222222222222222222222222",
	}
	f := newOpenFixture(taskA, taskB)
	f.components.add(repositoryID, component)
	waker := &fakeWaker{}
	f.svc = New(Deps{
		Store: f.store, Tasks: f.tasks, ParkedTasks: f.parked, Components: f.components, Waker: waker,
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	n, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, f.store.releases, 1, "one release must carry every waiting task, not one release each")
	require.Len(t, waker.calls, 1, "a pending dispatch release must wake the release engineer exactly once")

	var release domain.Release
	for _, r := range f.store.releases {
		release = r
	}
	assert.Len(t, release.Tasks, 2)
}

// a done task that never merged (MergeGate refused it while the
// delivery profile was unconfirmed) must be woken once the profile is
// confirmed, or nothing would ever retry its merge.
func TestOpenPendingWakesUnmergedDoneTasksOfTheComponent(t *testing.T) {
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	unmerged := domain.BoardTask{
		ID: uuid.New(), Key: "T-9", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID,
	}
	f := newOpenFixture(unmerged)
	f.components.add(repositoryID, component)
	waker := &fakeWaker{}
	f.svc = New(Deps{
		Store: f.store, Tasks: f.tasks, ParkedTasks: f.parked, Components: f.components, Waker: waker,
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	n, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing merged yet, so nothing opened")
	require.Len(t, waker.calls, 1, "the unmerged task must be woken to retry its merge")
	assert.Equal(t, unmerged.ID, waker.calls[0].task.ID)
}

// a task whose only release is a draft left behind by its component
// leaving batch mode must be pulled out of that draft and opened normally;
// the emptied draft is marked superseded.
func TestOpenPendingPullsAStrandedDraftTaskOutWhenComponentLeavesBatchMode(t *testing.T) {
	repositoryID := uuid.New()
	componentID := uuid.New()
	task := domain.BoardTask{
		ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &componentID, MergeCommitSHA: mergeSHA,
	}
	f := newOpenFixture(task)

	draftRelease := domain.Release{
		RepositoryID: repositoryID, ComponentID: &componentID, Mode: domain.DeliveryBatch,
		Executor: domain.ExecutorGitHubActions, Status: domain.ReleaseDraft,
		Profile: deliveryProfile(domain.DeliveryBatch, domain.ExecutorGitHubActions),
	}
	draft, err := f.store.Create(context.Background(), draftRelease, []uuid.UUID{task.ID})
	require.NoError(t, err)

	onMergeComponent := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	onMergeComponent.ID = componentID
	f.components.add(repositoryID, onMergeComponent)

	n, err := f.svc.OpenPending(context.Background(), repositoryID, componentID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	emptiedDraft, err := f.store.Get(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseSuperseded, emptiedDraft.Status, "an emptied draft must be superseded")
	assert.Empty(t, emptiedDraft.Tasks)

	newRelease, err := f.store.ForTask(context.Background(), task.ID)
	require.NoError(t, err)
	assert.NotEqual(t, draft.ID, newRelease.ID)
	assert.Equal(t, domain.ReleaseDeploying, newRelease.Status)
}

func TestOpenPendingSkipsATaskThatAlreadyHasARelease(t *testing.T) {
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	task := domain.BoardTask{
		ID: uuid.New(), Key: "T-9", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID,
		MergeCommitSHA: mergeSHA,
	}
	f := newOpenFixture(task)
	f.components.add(repositoryID, component)

	first, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, first)

	second, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, second, "a task that already belongs to a release must not get a second one")
}

func strPtr(s string) *string { return &s }
