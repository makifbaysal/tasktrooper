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

func batchProfile(executor domain.DeliveryExecutor) domain.ComponentDelivery {
	p := domain.ComponentDelivery{
		Mode:         domain.DeliveryBatch,
		Executor:     executor,
		TagPattern:   "v{version}",
		LocalCommand: "./release.sh {version}",
		Verify:       domain.DeliveryVerify{SoakMinutes: 10},
	}
	return p.Normalized()
}

func batchComponent(executor domain.DeliveryExecutor) domain.Component {
	profile := batchProfile(executor)
	return domain.Component{
		ID:     uuid.New(),
		Path:   "desktop",
		Status: domain.ComponentStatusActive,
		Delivery: domain.Fact[domain.ComponentDelivery]{
			Override: &profile,
		},
	}
}

func TestOpenBatchCreatesTheDraftOnARace(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	repositoryID := uuid.New()
	component := batchComponent(domain.ExecutorGitHubActions)
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	// Simulate idx_releases_one_draft rejecting this merge's own Create
	// because a racing merge's draft already committed.
	f.store.failCreateTimes = 1

	opening := f.svc.OpenForMerge(context.Background(), repositoryID, task, mergeSHA)

	require.NotNil(t, opening.ReleaseID, "a Create failure must fall back to the racing winner's draft")
	assert.Equal(t, domain.ReleaseDraft, opening.Status)
	assert.Len(t, f.store.releases, 1, "only the winner's draft must exist, never a second row")

	draft, err := f.store.Get(context.Background(), *opening.ReleaseID)
	require.NoError(t, err)
	require.Len(t, draft.Tasks, 1)
	assert.Equal(t, task.ID, draft.Tasks[0].ID, "the loser's task must still land in the winner's draft")
}

func TestOpenPendingQueuesAMergedBatchTaskIntoADraft(t *testing.T) {
	repositoryID := uuid.New()
	component := batchComponent(domain.ExecutorLocal)
	merged := domain.BoardTask{
		ID: uuid.New(), Key: "T-9", Column: domain.TaskColumnDone,
		RepositoryID: repositoryID, ComponentID: &component.ID,
		MergeCommitSHA: mergeSHA,
	}
	f := newOpenFixture(merged)
	f.components.add(repositoryID, component)

	n, err := f.svc.OpenPending(context.Background(), repositoryID, component.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, f.store.releases, 1)

	r, err := f.store.ForTask(context.Background(), merged.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDraft, r.Status)
}

func newBatchDeployFixture(executor domain.DeliveryExecutor) (*Service, *fakeReleaseStore, *fakeActions, *fakeRepos) {
	store := newFakeReleaseStore()
	actions := newFakeActions()
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "widget", RootPath: "/repos/widget"}}
	svc := New(Deps{
		Store:   store,
		Actions: actions,
		Repos:   repos,
		RepoCoordinates: func(context.Context, domain.Repository) (string, string, error) {
			return "acme", "widget", nil
		},
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	_ = executor
	return svc, store, actions, repos
}

func pendingBatchRelease(repositoryID uuid.UUID, executor domain.DeliveryExecutor) domain.Release {
	return domain.Release{
		RepositoryID: repositoryID,
		Mode:         domain.DeliveryBatch,
		Executor:     executor,
		Status:       domain.ReleasePending,
		Version:      "1.2.0",
		Tag:          "v1.2.0",
		CommitSHA:    mergeSHA,
		Profile:      batchProfile(executor),
	}
}

func TestDeployBatchGitHubActionsTagsTheCutCommit(t *testing.T) {
	svc, store, actions, _ := newBatchDeployFixture(domain.ExecutorGitHubActions)
	repositoryID := uuid.New()
	created, err := store.Create(context.Background(), pendingBatchRelease(repositoryID, domain.ExecutorGitHubActions), nil)
	require.NoError(t, err)

	updated, err := svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	require.Len(t, actions.tagCalls, 1)
	assert.Contains(t, actions.tagCalls[0], "v1.2.0")
	assert.Empty(t, actions.dispatchCalls, "batch never dispatches — the tag-triggered workflow does the rest")
}

func TestDeployBatchGitHubActionsRefusesAnExistingTag(t *testing.T) {
	svc, store, actions, _ := newBatchDeployFixture(domain.ExecutorGitHubActions)
	actions.createTagErr = errors.New("422 Reference already exists")
	svc.refAlreadyExists = alwaysRefExists
	repositoryID := uuid.New()
	created, err := store.Create(context.Background(), pendingBatchRelease(repositoryID, domain.ExecutorGitHubActions), nil)
	require.NoError(t, err)

	_, err = svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseTagExists, "a batch release must never silently re-use an already-shipped tag")
	assert.Contains(t, err.Error(), "re-cut", "the message must tell the human how to recover")

	stillPending, gerr := store.Get(context.Background(), created.ID)
	require.NoError(t, gerr)
	assert.Equal(t, domain.ReleasePending, stillPending.Status)
	assert.Nil(t, stillPending.DeployStartedAt, "left un-claimed so Cut() accepts it back for a re-cut")
}

func TestRollbackBatchGoesStraightToRolledBackWithNoRedeploy(t *testing.T) {
	repositoryID := uuid.New()
	store := newFakeReleaseStore()
	reverter := &fakeReverter{sha: "revertsha0123456789012345678901234567890"}
	tasks := newFakeTasks()
	parked := newFakeParked()
	mergeState := &fakeMergeState{}
	repos := &fakeRepos{repo: domain.Repository{ID: repositoryID, Name: "widget", RootPath: "/repos/widget"}}
	svc := New(Deps{
		Store:       store,
		Reverter:    reverter,
		Tasks:       tasks,
		ParkedTasks: parked,
		MergeState:  mergeState,
		Repos:       repos,
		Clock:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})

	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", RepositoryID: repositoryID}
	tasks.tasks[task.ID] = task

	r := pendingBatchRelease(repositoryID, domain.ExecutorGitHubActions)
	r.Status = domain.ReleaseAwaitingVerdict
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.DeployedAt = &now
	created, err := store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := svc.Rollback(context.Background(), created.ID, domain.ReleaseActorHuman, domain.RollbackVerifyFailed, "smoke check failed")
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseRolledBack, updated.Status, "batch never goes through rolling_back — nothing is redeployed")
	require.NotNil(t, updated.Rollback)
	assert.Equal(t, domain.RollbackMechanismRevert, updated.Rollback.Mechanism)
	assert.Equal(t, reverter.sha, updated.Rollback.RestoredRef)
	assert.Contains(t, updated.Rollback.Detail, "nothing was redeployed")
	require.NotEmpty(t, updated.Rollback.ManualSteps)
	assert.Contains(t, updated.Rollback.ManualSteps[0], "v1.2.0", "the unpublish step must come before the tasks' own runbooks and name the tag")

	require.Len(t, tasks.updates, 1)
	assert.Equal(t, domain.TaskColumnNeedRevision, *tasks.updates[0].Column)
	require.Len(t, mergeState.reset, 1)
}

func TestRollbackBatchStoreNamesTheBuildsToHalt(t *testing.T) {
	repositoryID := uuid.New()
	store := newFakeReleaseStore()
	reverter := &fakeReverter{sha: "revertsha0123456789012345678901234567890"}
	tasks := newFakeTasks()
	repos := &fakeRepos{repo: domain.Repository{ID: repositoryID, Name: "widget", RootPath: "/repos/widget"}}
	svc := New(Deps{
		Store: store, Reverter: reverter, Tasks: tasks, ParkedTasks: newFakeParked(), Repos: repos,
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", RepositoryID: repositoryID}
	tasks.tasks[task.ID] = task

	r := pendingBatchRelease(repositoryID, domain.ExecutorStore)
	r.Status = domain.ReleaseFailed
	r.StoreBuilds = []domain.ReleaseStoreBuild{
		{Platform: "ios", Build: "42"},
		{Platform: "android", Error: "start failed"},
	}
	created, err := store.Create(context.Background(), r, []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := svc.Rollback(context.Background(), created.ID, domain.ReleaseActorHuman, domain.RollbackManual, "bad build")
	require.NoError(t, err)
	require.NotNil(t, updated.Rollback)
	require.Len(t, updated.Rollback.ManualSteps, 1, "only the platform with an actual build gets a halt step")
	assert.Contains(t, updated.Rollback.ManualSteps[0], "ios")
	assert.Contains(t, updated.Rollback.ManualSteps[0], "42")
}

func TestOpenBatchRetriesOnceWhenAddingToTheDraftLosesARace(t *testing.T) {
	store := newFakeReleaseStore()
	racey := &raceyDraftStore{fakeReleaseStore: store, failAddToDraftTimes: 1}
	svc := New(Deps{Store: racey, Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})

	repositoryID := uuid.New()
	component := batchComponent(domain.ExecutorGitHubActions)
	profile := *component.Delivery.Override

	first := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	opening := svc.openBatch(context.Background(), repositoryID, first, component, "widget", profile)
	require.NotNil(t, opening.ReleaseID)

	second := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	opening2 := svc.openBatch(context.Background(), repositoryID, second, component, "widget", profile)
	require.NotNil(t, opening2.ReleaseID, "AddTasksToDraft losing the race once must retry and still queue the task")

	draft, err := store.Get(context.Background(), *opening2.ReleaseID)
	require.NoError(t, err)
	found := false
	for _, tk := range draft.Tasks {
		if tk.ID == second.ID {
			found = true
		}
	}
	assert.True(t, found, "the task must land in the draft after the retry")
}

func TestOpenBatchGivesUpAfterOneRetry(t *testing.T) {
	store := newFakeReleaseStore()
	racey := &raceyDraftStore{fakeReleaseStore: store, failAddToDraftTimes: 2}
	svc := New(Deps{Store: racey, Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }})

	repositoryID := uuid.New()
	component := batchComponent(domain.ExecutorGitHubActions)
	profile := *component.Delivery.Override

	first := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	opening := svc.openBatch(context.Background(), repositoryID, first, component, "widget", profile)
	require.NotNil(t, opening.ReleaseID)

	second := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	opening2 := svc.openBatch(context.Background(), repositoryID, second, component, "widget", profile)
	assert.Nil(t, opening2.ReleaseID, "a second lost race must not be retried forever")
	assert.Contains(t, opening2.Next, "could not open a release")
}

func TestWakeNewestTaskSkipsATaskNotInDone(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorLocal)
	inDone := taskRef(domain.TaskTypeTask)
	movedElsewhere := taskRef(domain.TaskTypeTask)
	movedElsewhere.Column = domain.TaskColumnNeedRevision
	draft := f.addDraft(component, inDone, movedElsewhere)
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true

	_, err := f.svc.Cut(context.Background(), draft.ID, domain.ReleaseActorHuman, domain.ReleaseCutRequest{Version: "1.0.0"})
	require.NoError(t, err)

	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, inDone.ID, f.waker.calls[0].task.ID, "the newest task not in done must be skipped for one that still is")
}
