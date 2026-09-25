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

type fakeDeployOrder struct {
	bySource map[uuid.UUID][]domain.TaskRelation
}

func (f *fakeDeployOrder) ListBySource(_ context.Context, source uuid.UUID) ([]domain.TaskRelation, error) {
	return f.bySource[source], nil
}

func (f *fakeDeployOrder) ListDeployDependents(_ context.Context, targets []uuid.UUID) ([]domain.TaskRelation, error) {
	want := map[uuid.UUID]bool{}
	for _, t := range targets {
		want[t] = true
	}
	var out []domain.TaskRelation
	for _, rels := range f.bySource {
		for _, r := range rels {
			if want[r.TargetTaskID] {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

type fakeLocator struct{ repo uuid.UUID }

func (f fakeLocator) FindTaskRepositoryID(context.Context, uuid.UUID) (uuid.UUID, error) {
	return f.repo, nil
}

type orderFixture struct {
	svc        *Service
	store      *fakeReleaseStore
	tasks      *fakeTasks
	waker      *fakeWaker
	components *fakeComponents
	repo       uuid.UUID
	component  domain.Component
}

func newOrderFixture(mode domain.DeliveryMode, dependent, dependency domain.BoardTask) *orderFixture {
	repo := uuid.New()
	comps := newFakeComponents()
	component := comps.add(repo, confirmedComponent(mode, domain.ExecutorGitHubActions))
	dependent.RepositoryID, dependency.RepositoryID = repo, repo
	dependent.ComponentID = &component.ID
	store := newFakeReleaseStore()
	tk := newFakeTasks(dependent, dependency)
	waker := &fakeWaker{}
	order := &fakeDeployOrder{bySource: map[uuid.UUID][]domain.TaskRelation{
		dependent.ID: {{
			SourceTaskID: dependent.ID, TargetTaskID: dependency.ID,
			RelationType: domain.TaskRelationDeployDependsOn, TargetKey: dependency.Key,
		}},
	}}
	svc := New(Deps{
		Store:       store,
		Tasks:       tk,
		ParkedTasks: newFakeParked(),
		Components:  comps,
		Waker:       waker,
		DeployOrder: order,
		Locator:     fakeLocator{repo: repo},
		Clock:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &orderFixture{svc: svc, store: store, tasks: tk, waker: waker, components: comps, repo: repo, component: component}
}

func TestMergeGateHoldsAnOnMergeTaskUntilItsDeployDependencyIsReleased(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency)
	dependent = f.tasks.tasks[dependent.ID]

	err := f.svc.MergeGate(context.Background(), f.repo, dependent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDeployDependencyPending)
	assert.Contains(t, err.Error(), "T-1")
	require.Len(t, f.tasks.comments, 1)

	released := f.tasks.tasks[dependency.ID]
	released.Column = domain.TaskColumnReleased
	f.tasks.tasks[dependency.ID] = released
	assert.NoError(t, f.svc.MergeGate(context.Background(), f.repo, dependent))
}

func TestDeployRefusesADispatchReleaseWhoseDependencyIsNotLive(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnInProgress}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone, MergeCommitSHA: "abcdef1234567890"}
	f := newOrderFixture(domain.DeliveryDispatch, dependent, dependency)
	r, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: f.repo, ComponentID: &f.component.ID, Mode: domain.DeliveryDispatch,
		Status: domain.ReleasePending, CommitSHA: "abcdef1234567890", Profile: deliveryProfile(domain.DeliveryDispatch, domain.ExecutorGitHubActions),
	}, []uuid.UUID{dependent.ID})
	require.NoError(t, err)

	_, err = f.svc.Deploy(context.Background(), r.ID, domain.ReleaseActorAgent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrDeployDependencyPending)
	got, _ := f.store.Get(context.Background(), r.ID)
	assert.Equal(t, domain.ReleasePending, got.Status)
}

func TestADependencyInsideTheSameReleaseDoesNotBlockIt(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryDispatch, dependent, dependency)

	assert.Empty(t, f.svc.pendingDeployDependencies(context.Background(), []uuid.UUID{dependent.ID, dependency.ID}))
	assert.NotEmpty(t, f.svc.pendingDeployDependencies(context.Background(), []uuid.UUID{dependent.ID}))
}

func TestFinishWakesATaskThatWasWaitingOnTheRelease(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency)
	r, err := f.store.Create(context.Background(), domain.Release{
		RepositoryID: f.repo, ComponentID: &f.component.ID, Mode: domain.DeliveryOnMerge,
		Status: domain.ReleaseAwaitingVerdict, CommitSHA: "abcdef1234567890",
	}, []uuid.UUID{dependency.ID})
	require.NoError(t, err)

	_, err = f.svc.Finish(context.Background(), r.ID, domain.ReleaseActorAgent, "logs and errors clean since deploy")
	require.NoError(t, err)

	require.Len(t, f.waker.calls, 1)
	assert.Equal(t, dependent.ID, f.waker.calls[0].task.ID)
}
