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

// --- MergeGate ---

func TestMergeGateRefusesAnOnMergeComponentWithPendingSteps(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone, BeforeDeploy: strPtr("Flip feature flag X")}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	err := f.svc.MergeGate(context.Background(), repositoryID, task)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrBeforeDeployPending)
	assert.Contains(t, err.Error(), "Flip feature flag X")
}

func TestMergeGateAllowsAnOnMergeComponentOnceConfirmed(t *testing.T) {
	confirmedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := domain.BoardTask{
		ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone,
		BeforeDeploy: strPtr("Flip feature flag X"), BeforeDeployConfirmedAt: &confirmedAt,
	}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	assert.NoError(t, f.svc.MergeGate(context.Background(), repositoryID, task))
}

func TestMergeGateIgnoresADispatchComponent(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone, BeforeDeploy: strPtr("Run a migration")}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryDispatch, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	// dispatch deploys are gated at deploy_release, not at the merge — the
	// merge itself never ships anything for a dispatch component.
	assert.NoError(t, f.svc.MergeGate(context.Background(), repositoryID, task))
}

func TestMergeGateAllowsAnUnresolvedComponent(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone, BeforeDeploy: strPtr("Run a migration")}
	f := newOpenFixture(task)
	repositoryID := uuid.New()

	// No component, and OpenForMerge's own unresolved-component handling
	// already leaves the task waiting in done — MergeGate must not also
	// refuse it.
	assert.NoError(t, f.svc.MergeGate(context.Background(), repositoryID, task))
}

func TestMergeGateAllowsATaskWithNoBeforeDeploySteps(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := confirmedComponent(domain.DeliveryOnMerge, domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	assert.NoError(t, f.svc.MergeGate(context.Background(), repositoryID, task))
}

// --- Deploy gate (dispatch) ---

func TestDeployRefusesADispatchReleaseWithPendingBeforeDeploySteps(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: "T-1"}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)
	created.Tasks = []domain.ReleaseTaskRef{{ID: task.ID, Key: "T-1", BeforeDeploy: "Run the migration"}}
	f.store.releases[created.ID] = created

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrBeforeDeployPending)

	require.Len(t, f.tasks.comments, 1)
	assert.Contains(t, f.tasks.comments[0].Content, "Run the migration")
	assert.Contains(t, f.tasks.comments[0].Content, "Do not retry")
	assert.Empty(t, f.actions.dispatchCalls, "nothing must deploy while a step is pending")

	still, err := f.store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleasePending, still.Status, "the refusal must not change the release's status")
}

func TestDeployProceedsOnceBeforeDeployIsConfirmed(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone, Key: "T-1"}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)
	created.Tasks = []domain.ReleaseTaskRef{{ID: task.ID, Key: "T-1", BeforeDeploy: "Run the migration", BeforeDeployConfirmed: true}}
	f.store.releases[created.ID] = created

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	assert.Empty(t, f.tasks.comments)
	require.Len(t, f.actions.dispatchCalls, 1)
}

// --- Cut stamps before-deploy confirmations ---

func TestCutStampsBeforeDeployConfirmationsForTasksThatHaveSteps(t *testing.T) {
	store := newFakeReleaseStore()
	components := newFakeComponents()
	git := newFakeGit()
	tasks := newFakeTasks()
	confirmer := &fakeBeforeDeployConfirmer{}
	repo := domain.Repository{ID: uuid.New(), Name: "desktop", RootPath: "/repos/desktop"}
	repos := &fakeRepos{repo: repo}
	svc := New(Deps{
		Store: store, Components: components, Git: git, Tasks: tasks, Repos: repos,
		BeforeDeploy: confirmer,
		Clock:        func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	component := batchComponent(domain.ExecutorGitHubActions)
	components.add(repo.ID, component)

	withSteps := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1", Title: "Add flag", TaskType: domain.TaskTypeTask, MergeCommitSHA: mergeSHA, BeforeDeploy: "Flip feature flag"}
	noSteps := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-2", Title: "Fix typo", TaskType: domain.TaskTypeBug, MergeCommitSHA: mergeSHA}
	tasks.tasks[withSteps.ID] = domain.BoardTask{ID: withSteps.ID, Key: withSteps.Key, RepositoryID: repo.ID}
	tasks.tasks[noSteps.ID] = domain.BoardTask{ID: noSteps.ID, Key: noSteps.Key, RepositoryID: repo.ID}

	r := domain.Release{
		RepositoryID: repo.ID, ComponentID: &component.ID, Mode: domain.DeliveryBatch,
		Executor: component.Delivery.Override.Executor, Status: domain.ReleaseDraft,
		Profile: *component.Delivery.Override,
	}
	created, err := store.Create(context.Background(), r, []uuid.UUID{withSteps.ID, noSteps.ID})
	require.NoError(t, err)
	created.Tasks = []domain.ReleaseTaskRef{withSteps, noSteps}
	store.releases[created.ID] = created

	git.head = mergeSHA
	git.ancestors[mergeSHA] = true

	updated, err := svc.Cut(context.Background(), created.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "1.2.0"})
	require.NoError(t, err)
	assert.Equal(t, domain.ReleasePending, updated.Status)

	require.Len(t, confirmer.confirmed, 1, "cutting confirms only the task that actually carries before-deploy steps")
	assert.Equal(t, withSteps.ID, confirmer.confirmed[0])
}

// --- Finish posts after-deploy comments ---

func TestFinishPostsOneCommentPerTaskWithAfterDeploySteps(t *testing.T) {
	f := newFinishFixture()
	repositoryID := uuid.New()
	withSteps := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-1", AfterDeploy: "Clear the CDN cache"}
	noSteps := domain.ReleaseTaskRef{ID: uuid.New(), Key: "T-2"}
	f.tasks.tasks[withSteps.ID] = domain.BoardTask{ID: withSteps.ID, RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[noSteps.ID] = domain.BoardTask{ID: noSteps.ID, RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	created, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseAwaitingVerdict,
	}, []uuid.UUID{withSteps.ID, noSteps.ID})
	require.NoError(t, err)
	created.Tasks = []domain.ReleaseTaskRef{withSteps, noSteps}
	f.store.releases[created.ID] = created

	_, err = f.svc.Finish(context.Background(), created.ID, domain.ReleaseActorAgent, "checked logs")
	require.NoError(t, err)

	require.Len(t, f.tasks.comments, 1, "only the task with after-deploy text gets a comment")
	assert.Contains(t, f.tasks.comments[0].Content, "Released — do these after-deploy steps now: Clear the CDN cache")
}

// --- WakeTask ---

func TestWakeTaskReportsTheTasksReleaseStatusWhenOneExists(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	_, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseDeploying,
	}, []uuid.UUID{task.ID})
	require.NoError(t, err)

	require.NoError(t, f.svc.WakeTask(context.Background(), repositoryID, task))
	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, domain.ReleaseDeploying, f.waker.calls[0].status)
}

func TestWakeTaskReportsAnEmptyStatusWhenNoReleaseExistsYet(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}

	require.NoError(t, f.svc.WakeTask(context.Background(), repositoryID, task))
	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, domain.ReleaseStatus(""), f.waker.calls[0].status)
}

func TestWakeTaskIsNilSafeWithoutAWaker(t *testing.T) {
	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	task := domain.BoardTask{ID: uuid.New()}

	assert.NoError(t, svc.WakeTask(context.Background(), uuid.New(), task))
}
