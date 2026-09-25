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

type deployFixture struct {
	svc     *Service
	store   *fakeReleaseStore
	actions *fakeActions
	repos   *fakeRepos
	waker   *fakeWaker
	tasks   *fakeTasks
	parked  *fakeParked
}

func newDeployFixture() *deployFixture {
	store := newFakeReleaseStore()
	actions := newFakeActions()
	repos := &fakeRepos{repo: domain.Repository{ID: uuid.New(), Name: "widget", RootPath: "/repos/widget"}}
	waker := &fakeWaker{}
	tasks := newFakeTasks()
	parked := newFakeParked()
	svc := New(Deps{
		Store:       store,
		Actions:     actions,
		Repos:       repos,
		Waker:       waker,
		Tasks:       tasks,
		ParkedTasks: parked,
		RepoCoordinates: func(context.Context, domain.Repository) (string, string, error) {
			return "acme", "widget", nil
		},
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &deployFixture{svc: svc, store: store, actions: actions, repos: repos, waker: waker, tasks: tasks, parked: parked}
}

func pendingDispatchRelease(repositoryID uuid.UUID) domain.Release {
	return domain.Release{
		RepositoryID: repositoryID,
		Mode:         domain.DeliveryDispatch,
		Executor:     domain.ExecutorGitHubActions,
		Status:       domain.ReleasePending,
		CommitSHA:    mergeSHA,
		Profile:      deliveryProfile(domain.DeliveryDispatch, domain.ExecutorGitHubActions),
	}
}

func TestDeployDispatchesTheWorkflowAtTheReleaseTag(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), nil)
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	assert.Equal(t, domain.ReleaseTagForCommit(mergeSHA), updated.Tag)
	require.NotNil(t, updated.DeployStartedAt)
	require.Len(t, f.actions.tagCalls, 1)
	require.Len(t, f.actions.dispatchCalls, 1)
	assert.Contains(t, f.actions.dispatchCalls[0], domain.ReleaseTagForCommit(mergeSHA))
}

func TestDeployRefusesAReleaseThatIsNotPending(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	r := pendingDispatchRelease(repositoryID)
	r.Status = domain.ReleaseDeploying
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
}

func TestDeployRefusesAnOnMergeRelease(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	r := pendingDispatchRelease(repositoryID)
	r.Mode = domain.DeliveryOnMerge
	created, err := f.store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseNoDeploy)
}

func TestDeployFailsTheReleaseWhenActionsIsUnavailable(t *testing.T) {
	f := newDeployFixture()
	f.actions.dispatchErr = errors.New("402 payment required")
	f.svc.ciUnavailable = alwaysCIUnavailable
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err, "a CI-unavailable dispatch refusal is reported on the release, not returned as an error")
	assert.Equal(t, domain.ReleaseFailed, updated.Status)
	assert.NotEmpty(t, updated.FailureReason)
	assert.Len(t, f.waker.calls, 1, "a failed deploy must hand the card back")
}

func TestDeployTreatsAnAlreadyExistingTagAsSuccess(t *testing.T) {
	f := newDeployFixture()
	f.actions.createTagErr = errors.New("422 Reference already exists")
	f.svc.refAlreadyExists = alwaysRefExists
	repositoryID := uuid.New()
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), nil)
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)
	assert.Equal(t, domain.ReleaseDeploying, updated.Status)
	require.Len(t, f.actions.dispatchCalls, 1, "dispatch must still run after an already-exists tag")
}

func TestDeploySecondConcurrentCallIsRefusedAfterTheFirstClaims(t *testing.T) {
	f := newDeployFixture()
	repositoryID := uuid.New()
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), nil)
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)
	assert.Len(t, f.actions.dispatchCalls, 1, "the second call must never dispatch the workflow again")
}

func TestDeployReturnsToPendingWhenNothingWasActuallyDispatched(t *testing.T) {
	f := newDeployFixture()
	f.actions.dispatchErr = errors.New("500 internal error")
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.Error(t, err, "a non-definitive dispatch failure (a 5xx, a network error) must be returned so deploy_release can retry")
	assert.Empty(t, f.waker.calls, "nothing was deployed — there is no card to hand back")

	after, gerr := f.store.Get(context.Background(), created.ID)
	require.NoError(t, gerr)
	assert.Equal(t, domain.ReleasePending, after.Status, "the claim must revert to pending, not get stuck failed, when nothing actually deployed")
	assert.Nil(t, after.DeployStartedAt, "DeployStartedAt must be cleared so the release reads as never having tried")
}

func TestDeployFailsOnA404WorkflowNotDispatchableEvenWithoutTheCIUnavailableClassifier(t *testing.T) {
	f := newDeployFixture()
	f.actions.dispatchErr = errors.New("github api: 404 Not Found")
	repositoryID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: repositoryID, Column: domain.TaskColumnDone}
	f.tasks.tasks[task.ID] = task
	created, err := f.store.Create(context.Background(), pendingDispatchRelease(repositoryID), []uuid.UUID{task.ID})
	require.NoError(t, err)

	updated, err := f.svc.Deploy(context.Background(), created.ID, domain.ReleaseActorAgent)
	require.NoError(t, err, "a definitive refusal is reported on the release, not returned as an API error")
	assert.Equal(t, domain.ReleaseFailed, updated.Status)
	assert.Contains(t, updated.FailureReason, "no rollback is needed")
	assert.Len(t, f.waker.calls, 1, "a failed deploy must hand the card back")
}
