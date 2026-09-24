package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type wakeTaskStore struct {
	*fakePackageTaskStore
	blockedResource map[uuid.UUID]string
}

func newWakeTaskStore() *wakeTaskStore {
	return &wakeTaskStore{
		fakePackageTaskStore: &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}},
		blockedResource:      map[uuid.UUID]string{},
	}
}

func (w *wakeTaskStore) MarkWorkOrderWaiting(_ context.Context, _, taskID uuid.UUID, _ string) error {
	w.blockedResource[taskID] = domain.ResourceWorkOrder
	return nil
}

func (w *wakeTaskStore) ClearWorkOrderWaiting(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	if w.blockedResource[taskID] != domain.ResourceWorkOrder {
		return domain.BoardTask{}, false, nil
	}
	delete(w.blockedResource, taskID)
	return w.tasks[taskID], true, nil
}

type wakeRelationStore struct {
	*graphRelationStore
	tasks map[uuid.UUID]domain.BoardTask
}

func (w *wakeRelationStore) ListBlockingSources(_ context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error) {
	var out []domain.BoardTask
	for _, e := range w.edges {
		if e.TargetTaskID != targetTaskID || e.RelationType != domain.TaskRelationBlocks {
			continue
		}
		source := w.tasks[e.SourceTaskID]
		if source.Column == domain.TaskColumnDone || source.Column == domain.TaskColumnReleased {
			continue
		}
		out = append(out, source)
	}
	return out, nil
}

type wakeFixture struct {
	svc      *Service
	tasks    *wakeTaskStore
	relation *wakeRelationStore
	repoID   uuid.UUID
}

func newWakeFixture(t *testing.T) *wakeFixture {
	t.Helper()
	repoID := uuid.New()
	tasks := newWakeTaskStore()
	relation := &wakeRelationStore{graphRelationStore: newGraphRelations(), tasks: tasks.tasks}
	sweeper := board.NewWorkOrderSweeper(tasks, relation, &board.Dispatcher{})
	sweeper.SetDependents(relation)
	svc := &Service{
		repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
		tasks:     tasks,
		relations: relation,
		workflows: workflowtest.Default().Reader(),
		// The review chain is now enforced unconditionally, but this fixture
		// is about the work-order wake, not review-chain enforcement, so every
		// task starts having already passed it.
		spans: visited(domain.TaskColumnCodeReview, domain.TaskColumnInQA, domain.TaskColumnPMUAT),
	}
	svc.SetWorkOrderSweeper(sweeper)
	return &wakeFixture{svc: svc, tasks: tasks, relation: relation, repoID: repoID}
}

func (f *wakeFixture) addTask(column domain.TaskColumn) domain.BoardTask {
	id := uuid.New()
	task := domain.BoardTask{ID: id, RepositoryID: f.repoID, Key: "T-X", Column: column, TaskType: "task"}
	f.tasks.tasks[id] = task
	return task
}

func TestUpdateTaskWakesADependentInTheSameCallWhenItsOnlyBlockerReachesDone(t *testing.T) {
	f := newWakeFixture(t)
	blocker := f.addTask(domain.TaskColumnInProgress)
	dependent := f.addTask(domain.TaskColumnInProgress)
	f.relation.add(blocker.ID, dependent.ID, domain.TaskRelationBlocks)
	require.NoError(t, f.tasks.MarkWorkOrderWaiting(context.Background(), f.repoID, dependent.ID, "waiting for blocker"))

	done := domain.TaskColumnDone
	_, err := f.svc.UpdateTask(context.Background(), f.repoID, blocker.ID, domain.UpdateBoardTaskRequest{
		Column: &done,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
	_, stillWaiting := f.tasks.blockedResource[dependent.ID]
	assert.False(t, stillWaiting, "the dependent must be unparked in the same request, not on the next sweep")
}

func TestUpdateTaskLeavesADependentParkedWhileAnotherBlockerIsStillOpen(t *testing.T) {
	f := newWakeFixture(t)
	blockerDone := f.addTask(domain.TaskColumnInProgress)
	blockerStillOpen := f.addTask(domain.TaskColumnInProgress)
	dependent := f.addTask(domain.TaskColumnInProgress)
	f.relation.add(blockerDone.ID, dependent.ID, domain.TaskRelationBlocks)
	f.relation.add(blockerStillOpen.ID, dependent.ID, domain.TaskRelationBlocks)
	require.NoError(t, f.tasks.MarkWorkOrderWaiting(context.Background(), f.repoID, dependent.ID, "waiting for two blockers"))

	done := domain.TaskColumnDone
	_, err := f.svc.UpdateTask(context.Background(), f.repoID, blockerDone.ID, domain.UpdateBoardTaskRequest{
		Column: &done,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
	resource, stillWaiting := f.tasks.blockedResource[dependent.ID]
	assert.True(t, stillWaiting, "one blocker landing must not clear a dependent that still has another open")
	assert.Equal(t, domain.ResourceWorkOrder, resource)
}

func TestUpdateTaskDoesNotWakeDependentsOnANonTerminalMove(t *testing.T) {
	f := newWakeFixture(t)
	blocker := f.addTask(domain.TaskColumnInProgress)
	dependent := f.addTask(domain.TaskColumnInProgress)
	f.relation.add(blocker.ID, dependent.ID, domain.TaskRelationBlocks)
	require.NoError(t, f.tasks.MarkWorkOrderWaiting(context.Background(), f.repoID, dependent.ID, "waiting for blocker"))

	review := domain.TaskColumnCodeReview
	_, err := f.svc.UpdateTask(context.Background(), f.repoID, blocker.ID, domain.UpdateBoardTaskRequest{
		Column: &review,
		Actor:  domain.TaskActorHuman,
	})

	require.NoError(t, err)
	_, stillWaiting := f.tasks.blockedResource[dependent.ID]
	assert.True(t, stillWaiting, "the blocker has not reached done or released yet")
}
