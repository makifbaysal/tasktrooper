package release

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// queueParked reads holds straight off fakeTasks, the way the real store reads
// blocked_resource, so a hold the gate writes is what the queue sees.
type queueParked struct {
	*fakeParked
	tasks *fakeTasks
}

func (q queueParked) ListBlockedByResource(_ context.Context, resource string, _ int) ([]domain.BoardTask, error) {
	q.tasks.mu.Lock()
	defer q.tasks.mu.Unlock()
	var out []domain.BoardTask
	for _, t := range q.tasks.tasks {
		if t.BlockedResource == resource {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

type queueFixture struct {
	svc       *Service
	store     *fakeReleaseStore
	tasks     *fakeTasks
	waker     *fakeWaker
	repo      uuid.UUID
	component domain.Component
	now       time.Time
}

var queueStart = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func newQueueFixture(mode domain.DeliveryMode, tasks ...domain.BoardTask) *queueFixture {
	f := &queueFixture{repo: uuid.New(), now: queueStart, waker: &fakeWaker{}, store: newFakeReleaseStore()}
	comps := newFakeComponents()
	f.component = comps.add(f.repo, confirmedComponent(mode, domain.ExecutorGitHubActions))
	for i := range tasks {
		tasks[i].RepositoryID = f.repo
		tasks[i].ComponentID = &f.component.ID
		tasks[i].Column = domain.TaskColumnDone
		tasks[i].CreatedAt = queueStart.Add(time.Duration(i) * time.Minute)
	}
	f.tasks = newFakeTasks(tasks...)
	f.svc = New(Deps{
		Store:       f.store,
		Tasks:       f.tasks,
		ParkedTasks: queueParked{fakeParked: newFakeParked(), tasks: f.tasks},
		Components:  comps,
		Waker:       f.waker,
		DeployOrder: &fakeDeployOrder{},
		Locator:     fakeLocator{repo: f.repo},
		Clock:       func() time.Time { return f.now },
	})
	f.svc.holds = f.tasks
	return f
}

func (f *queueFixture) inFlight(status domain.ReleaseStatus, shipping domain.BoardTask) domain.Release {
	r := domain.Release{
		ID: uuid.New(), RepositoryID: f.repo, ComponentID: &f.component.ID,
		Status: status, Version: "v1.4.0",
		Tasks: []domain.ReleaseTaskRef{{ID: shipping.ID, Key: shipping.Key}},
	}
	f.store.mu.Lock()
	f.store.releases[r.ID] = r
	f.store.mu.Unlock()
	return r
}

func (f *queueFixture) settle(r domain.Release, status domain.ReleaseStatus) {
	f.store.mu.Lock()
	r.Status = status
	f.store.releases[r.ID] = r
	f.store.mu.Unlock()
}

func (f *queueFixture) gate(id uuid.UUID) error {
	return f.svc.MergeGate(context.Background(), f.repo, f.tasks.tasks[id])
}

func (f *queueFixture) woken() []uuid.UUID {
	f.waker.mu.Lock()
	defer f.waker.mu.Unlock()
	var out []uuid.UUID
	for _, c := range f.waker.calls {
		out = append(out, c.task.ID)
	}
	return out
}

func TestMergeGateQueuesBehindAReleaseInFlight(t *testing.T) {
	for _, status := range []domain.ReleaseStatus{
		domain.ReleasePending, domain.ReleaseDeploying, domain.ReleaseVerifying,
		domain.ReleaseAwaitingVerdict, domain.ReleaseRollingBack, domain.ReleaseFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
			next := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
			f := newQueueFixture(domain.DeliveryOnMerge, shipping, next)
			f.inFlight(status, shipping)

			err := f.gate(next.ID)
			require.ErrorIs(t, err, domain.ErrReleaseInFlight)
			assert.Contains(t, err.Error(), "Do not retry")
			held := f.tasks.tasks[next.ID]
			assert.Equal(t, domain.ResourceReleaseInFlight, held.BlockedResource)
			assert.Equal(t, "T-1 (v1.4.0)", held.BlockedQuestion)
			require.Len(t, f.tasks.comments, 1)
			assert.Contains(t, f.tasks.comments[0].Content, "Waiting to merge")
		})
	}
}

func TestMergeGateLetsAMergeThroughOnceTheReleaseSettled(t *testing.T) {
	for _, status := range []domain.ReleaseStatus{domain.ReleaseReleased, domain.ReleaseRolledBack} {
		t.Run(string(status), func(t *testing.T) {
			shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
			next := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
			f := newQueueFixture(domain.DeliveryOnMerge, shipping, next)
			f.inFlight(status, shipping)

			require.NoError(t, f.gate(next.ID))
			assert.Empty(t, f.tasks.tasks[next.ID].BlockedResource)
		})
	}
}

func TestMergeGateDoesNotQueueBatchComponents(t *testing.T) {
	shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	next := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
	f := newQueueFixture(domain.DeliveryBatch, shipping, next)
	f.inFlight(domain.ReleaseVerifying, shipping)

	assert.NotErrorIs(t, f.gate(next.ID), domain.ErrReleaseInFlight)
}

func TestMergeGateMergesQueuedTasksOldestFirst(t *testing.T) {
	shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	first := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
	second := domain.BoardTask{ID: uuid.New(), Key: "T-3"}
	f := newQueueFixture(domain.DeliveryOnMerge, shipping, first, second)
	r := f.inFlight(domain.ReleaseVerifying, shipping)
	require.ErrorIs(t, f.gate(first.ID), domain.ErrReleaseInFlight)
	require.ErrorIs(t, f.gate(second.ID), domain.ErrReleaseInFlight)

	f.settle(r, domain.ReleaseReleased)

	// The younger task, woken out of turn, still waits for the older one.
	err := f.gate(second.ID)
	require.ErrorIs(t, err, domain.ErrReleaseInFlight)
	assert.Equal(t, "T-2", f.tasks.tasks[second.ID].BlockedQuestion)
	require.NoError(t, f.gate(first.ID))
	assert.Empty(t, f.tasks.tasks[first.ID].BlockedResource)
}

func TestWakeReleaseQueueWakesOnlyTheHeadOnceTheReleaseSettled(t *testing.T) {
	shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	first := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
	second := domain.BoardTask{ID: uuid.New(), Key: "T-3"}
	f := newQueueFixture(domain.DeliveryOnMerge, shipping, first, second)
	r := f.inFlight(domain.ReleaseVerifying, shipping)
	require.ErrorIs(t, f.gate(first.ID), domain.ErrReleaseInFlight)
	require.ErrorIs(t, f.gate(second.ID), domain.ErrReleaseInFlight)

	f.svc.wakeReleaseQueue(context.Background())
	assert.Empty(t, f.woken(), "nothing wakes while the release is in flight")

	f.settle(r, domain.ReleaseReleased)
	f.svc.wakeReleaseQueue(context.Background())
	assert.Equal(t, []uuid.UUID{first.ID}, f.woken())
	assert.Equal(t, domain.ResourceReleaseInFlight, f.tasks.tasks[first.ID].BlockedResource,
		"the hold stays until the woken merge passes the gate")

	// The sweeper's next pass does not wake it again before the interval.
	f.now = f.now.Add(time.Minute)
	f.svc.wakeReleaseQueue(context.Background())
	assert.Len(t, f.woken(), 1)

	// Once it merged (its hold cleared, a new release in flight) the next
	// task waits for that release, then is woken.
	require.NoError(t, f.gate(first.ID))
	next := f.inFlight(domain.ReleaseDeploying, first)
	f.svc.wakeReleaseQueue(context.Background())
	assert.Len(t, f.woken(), 1)
	f.settle(next, domain.ReleaseRolledBack)
	f.svc.wakeReleaseQueue(context.Background())
	assert.Equal(t, []uuid.UUID{first.ID, second.ID}, f.woken())
}

func TestWakeReleaseQueueReWakesAStalledHeadAfterTheInterval(t *testing.T) {
	shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	queued := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
	f := newQueueFixture(domain.DeliveryOnMerge, shipping, queued)
	r := f.inFlight(domain.ReleaseVerifying, shipping)
	require.ErrorIs(t, f.gate(queued.ID), domain.ErrReleaseInFlight)
	f.settle(r, domain.ReleaseReleased)

	f.svc.wakeReleaseQueue(context.Background())
	f.now = f.now.Add(queueReWakeInterval)
	f.svc.wakeReleaseQueue(context.Background())
	assert.Equal(t, []uuid.UUID{queued.ID, queued.ID}, f.woken())
}

func TestWakeReleaseQueueDropsAHoldWhoseTaskLeftDone(t *testing.T) {
	shipping := domain.BoardTask{ID: uuid.New(), Key: "T-1"}
	queued := domain.BoardTask{ID: uuid.New(), Key: "T-2"}
	f := newQueueFixture(domain.DeliveryOnMerge, shipping, queued)
	f.inFlight(domain.ReleaseVerifying, shipping)
	require.ErrorIs(t, f.gate(queued.ID), domain.ErrReleaseInFlight)

	moved := f.tasks.tasks[queued.ID]
	moved.Column = domain.TaskColumnInProgress
	f.tasks.tasks[queued.ID] = moved

	f.svc.wakeReleaseQueue(context.Background())
	assert.Empty(t, f.tasks.tasks[queued.ID].BlockedResource)
	assert.Empty(t, f.woken())
}
