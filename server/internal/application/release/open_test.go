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

func TestOpenForMergeBatchLeavesTaskInDoneWithNoRelease(t *testing.T) {
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
	assert.Nil(t, opening.ReleaseID)
	assert.Contains(t, opening.Next, "cuts the next one")
	assert.Empty(t, f.tasks.updates)
	assert.Empty(t, f.store.releases)
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
