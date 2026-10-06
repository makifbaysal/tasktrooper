package release

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (f *fakeTasks) HoldMerge(_ context.Context, _, taskID uuid.UUID, resource, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[taskID]
	t.BlockedResource, t.BlockedQuestion = resource, detail
	f.tasks[taskID] = t
	f.holdWrites++
	return nil
}

func (f *fakeTasks) ReleaseMergeHold(_ context.Context, taskID uuid.UUID, resources ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.tasks[taskID]
	if slices.Contains(resources, t.BlockedResource) {
		t.BlockedResource, t.BlockedQuestion = "", ""
		f.tasks[taskID] = t
	}
	return nil
}

const orderNoteOnly = domain.OrderNoteOpen + "\n**Release order (generated from this task's relations — do not edit by hand):**\n" +
	"- Ships after: T-1 (API foundation). Each one must be live in production before this task is released.\n" +
	domain.OrderNoteClose

func (f *orderFixture) withHolds() *orderFixture {
	f.svc.holds = f.tasks
	return f
}

// The T-101 regression: a before_deploy field holding nothing but the
// generated order note used to read as a step a human had to confirm, so an
// on_merge task whose dependency was already released never merged.
func TestMergeGateMergesATaskWhoseBeforeDeployIsOnlyTheOrderNote(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone, BeforeDeploy: strPtr(orderNoteOnly)}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	dependent = f.tasks.tasks[dependent.ID]

	require.NoError(t, f.svc.MergeGate(context.Background(), f.repo, dependent))
	assert.Empty(t, f.tasks.comments)
	assert.Empty(t, f.tasks.tasks[dependent.ID].BlockedResource)
}

func TestMergeGateWaitsForHumanStepsWrittenBesideTheOrderNote(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	steps := domain.ApplyOrderNote("Run the backfill script", orderNoteOnly)
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone, BeforeDeploy: &steps}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	dependent = f.tasks.tasks[dependent.ID]

	err := f.svc.MergeGate(context.Background(), f.repo, dependent)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrBeforeDeployPending)
	assert.Contains(t, err.Error(), "Run the backfill script")
	assert.NotContains(t, err.Error(), domain.OrderNoteOpen)
	held := f.tasks.tasks[dependent.ID]
	assert.Equal(t, domain.ResourceBeforeDeploy, held.BlockedResource)
	assert.Equal(t, "Run the backfill script", held.BlockedQuestion)
}

func TestMergeGateHoldsOnAnUnreleasedDeployDependency(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone, BeforeDeploy: strPtr(orderNoteOnly)}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	dependent = f.tasks.tasks[dependent.ID]

	err := f.svc.MergeGate(context.Background(), f.repo, dependent)
	require.ErrorIs(t, err, domain.ErrDeployDependencyPending)
	held := f.tasks.tasks[dependent.ID]
	assert.Equal(t, domain.ResourceDeployOrder, held.BlockedResource)
	assert.Contains(t, held.BlockedQuestion, "T-1")
	assert.Contains(t, held.BlockedQuestion, "[done]")
}

func TestMergeGateHoldsOnAnUnconfirmedDeliveryProfile(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	f := newOpenFixture(task)
	f.svc.holds = f.tasks
	repositoryID := uuid.New()
	component := domain.Component{
		ID: uuid.New(), Path: "api", Status: domain.ComponentStatusActive,
		Name: domain.Fact[string]{Override: strPtr("API")},
	}
	f.components.add(repositoryID, component)
	task.ComponentID = &component.ID
	f.tasks.tasks[task.ID] = task

	require.ErrorIs(t, f.svc.MergeGate(context.Background(), repositoryID, task), domain.ErrDeliveryUnconfirmed)
	held := f.tasks.tasks[task.ID]
	assert.Equal(t, domain.ResourceDeliveryProfile, held.BlockedResource)
	assert.Equal(t, "API", held.BlockedQuestion)
}

func TestMergeGateClearsItsHoldOnceTheMergeMayGo(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	dependent := domain.BoardTask{
		ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone,
		BlockedResource: domain.ResourceDeployOrder, BlockedQuestion: "T-1 [done]",
	}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()
	dependent = f.tasks.tasks[dependent.ID]

	require.NoError(t, f.svc.MergeGate(context.Background(), f.repo, dependent))
	assert.Empty(t, f.tasks.tasks[dependent.ID].BlockedResource)
}

func TestMergeGateDoesNotRewriteAnUnchangedHold(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnDone}
	dependent := domain.BoardTask{ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()

	for range 3 {
		_ = f.svc.MergeGate(context.Background(), f.repo, f.tasks.tasks[dependent.ID])
	}
	assert.Equal(t, 1, f.tasks.holdWrites)
}

func TestMergeGateLeavesAnotherParkAlone(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	dependent := domain.BoardTask{
		ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone,
		BlockedResource: domain.ResourceReleaseWatch,
	}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()

	require.NoError(t, f.svc.MergeGate(context.Background(), f.repo, f.tasks.tasks[dependent.ID]))
	assert.Equal(t, domain.ResourceReleaseWatch, f.tasks.tasks[dependent.ID].BlockedResource)
}

func TestWakingADeployDependentReleasesItsDeployOrderHold(t *testing.T) {
	dependency := domain.BoardTask{ID: uuid.New(), Key: "T-1", Column: domain.TaskColumnReleased}
	dependent := domain.BoardTask{
		ID: uuid.New(), Key: "T-2", Column: domain.TaskColumnDone,
		BlockedResource: domain.ResourceDeployOrder, BlockedQuestion: "T-1 [done]",
	}
	f := newOrderFixture(domain.DeliveryOnMerge, dependent, dependency).withHolds()

	f.svc.WakeDeployDependentsOf(context.Background(), dependency)

	require.Len(t, f.waker.calls, 1)
	assert.Empty(t, f.tasks.tasks[dependent.ID].BlockedResource)
}
