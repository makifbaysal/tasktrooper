package board

// The work-order park and its release.
//
// `blocks` has been in the schema since migration 022 and, until now, the only
// thing that read it refused a MOVE. These tests pin the two halves that make it
// real:
//
//  1. a task whose blocker is unfinished is PARKED — visibly, with a reason —
//     instead of being started or silently skipped;
//  2. the park ends on its own, because a sweeper asks the relation graph rather
//     than because somebody remembers to drag the card.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubBlockerReader struct {
	byTask map[uuid.UUID][]domain.BoardTask
	err    error
	calls  int
}

func (s *stubBlockerReader) ListBlockingSources(_ context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.byTask[targetTaskID], nil
}

type parkCall struct {
	taskID   uuid.UUID
	resource string
	detail   string
}

type stubResourceParker struct {
	parks []parkCall
	err   error
}

func (s *stubResourceParker) MarkWorkOrderWaiting(_ context.Context, _, taskID uuid.UUID, detail string) error {
	if s.err != nil {
		return s.err
	}
	s.parks = append(s.parks, parkCall{taskID: taskID, resource: domain.ResourceWorkOrder, detail: detail})
	return nil
}

type stubWorkOrderCommenter struct {
	contents []string
}

func (s *stubWorkOrderCommenter) AddComment(_ context.Context, _, _ uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	s.contents = append(s.contents, req.Content)
	return domain.TaskComment{Content: req.Content}, nil
}

func workOrderTask(col domain.TaskColumn) domain.BoardTask {
	return domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Key:          "T-2",
		Title:        "web export button",
		Column:       col,
		TaskType:     domain.TaskTypeTask,
	}
}

func openBlocker() domain.BoardTask {
	return domain.BoardTask{
		ID:       uuid.New(),
		Key:      "T-1",
		Title:    "API migration",
		Column:   domain.TaskColumnInProgress,
		TaskType: domain.TaskTypeTask,
	}
}

// -------------------------------------------------------------------- the park

// The park has to SAY what it is waiting for. A card sitting in blocked with no
// reason is indistinguishable from one a human parked, and the whole reason this
// is a park rather than a silent skip is that "nobody picked this up" was
// unreadable off the board.
func TestWorkOrderParkNamesEveryBlockerOnTheCard(t *testing.T) {
	task := workOrderTask(domain.TaskColumnTodo)
	blocker := openBlocker()
	parker := &stubResourceParker{}
	comments := &stubWorkOrderCommenter{}
	w := NewWorkOrder(&stubBlockerReader{}, parker)
	w.SetCommenter(comments)

	require.NoError(t, w.Park(context.Background(), task.RepositoryID, task, []domain.BoardTask{blocker}))

	require.Len(t, parker.parks, 1)
	assert.Equal(t, domain.ResourceWorkOrder, parker.parks[0].resource)
	assert.Contains(t, parker.parks[0].detail, "T-1 (API migration)")
	assert.Contains(t, parker.parks[0].detail, "in_progress")
	require.Len(t, comments.contents, 1)
	assert.Contains(t, comments.contents[0], "T-1 (API migration)")
}

// The park comment re-enters Dispatch for the same task.commented event
// (Service.AddComment calls s.emit synchronously), and since the column
// never moves out of {todo, in_progress} the gate is true again on that
// re-entrant call. Without this guard, Park would fire again from inside its
// own comment and recurse until the stack overflows. A task whose
// BlockedResource is already work_order is a re-entrant call, not a fresh
// park, so it must be a no-op.
func TestWorkOrderParkOnAnAlreadyParkedTaskIsANoOp(t *testing.T) {
	task := workOrderTask(domain.TaskColumnTodo)
	task.BlockedResource = domain.ResourceWorkOrder
	parker := &stubResourceParker{}
	comments := &stubWorkOrderCommenter{}
	w := NewWorkOrder(&stubBlockerReader{}, parker)
	w.SetCommenter(comments)

	require.NoError(t, w.Park(context.Background(), task.RepositoryID, task, []domain.BoardTask{openBlocker()}))

	assert.Empty(t, parker.parks, "already parked — no re-write of the same fields")
	assert.Empty(t, comments.contents, "already parked — no re-entrant comment, that is the recursion trigger")
}

// A missing comment store costs the card a line of history and nothing else.
func TestWorkOrderParkWorksWithoutACommenter(t *testing.T) {
	task := workOrderTask(domain.TaskColumnTodo)
	parker := &stubResourceParker{}
	w := NewWorkOrder(&stubBlockerReader{}, parker)

	require.NoError(t, w.Park(context.Background(), task.RepositoryID, task, []domain.BoardTask{openBlocker()}))
	assert.Len(t, parker.parks, 1)
}

// ------------------------------------------------------------------- the gate

// Only the columns where work STARTS. A blocks relation says who writes code
// first; parking a task that already reached code_review would strand a
// finished change behind a dependency the change no longer has.
func TestWorkOrderGateAppliesOnlyWhereWorkStarts(t *testing.T) {
	cases := []struct {
		column domain.TaskColumn
		want   bool
	}{
		{domain.TaskColumnTodo, true},
		{domain.TaskColumnInProgress, true},
		{domain.TaskColumnCodeReview, false},
		{domain.TaskColumnNeedRevision, false},
		{domain.TaskColumnReadyForQA, false},
		{domain.TaskColumnDone, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.column), func(t *testing.T) {
			got := workOrderGateApplies(DispatchInput{Task: workOrderTask(tc.column)})
			assert.Equal(t, tc.want, got)
		})
	}
}

// The sweeper's own hand-back must reach an agent. Re-asking the question the
// sweep just answered would, at best, confirm it and, at worst, re-park the task
// on a read that raced the blocker's own update.
func TestWorkOrderGateSkipsTheSweepersResume(t *testing.T) {
	input := DispatchInput{
		Task: workOrderTask(domain.TaskColumnTodo),
		Payload: map[string]interface{}{
			domain.EventPayloadResumedResource: domain.ResourceWorkOrder,
		},
	}
	assert.False(t, workOrderGateApplies(input))

	// Another park's resume is not this one's: a task handed back by the device
	// sweeper is still subject to its work order.
	input.Payload[domain.EventPayloadResumedResource] = domain.ResourceMobileDevice
	assert.True(t, workOrderGateApplies(input))
}

// ----------------------------------------------------------------- the sweeper

type stubWorkOrderParkStore struct {
	parked     []domain.BoardTask
	takenIDs   []uuid.UUID
	listErr    error
	takeMisses map[uuid.UUID]bool
}

func (s *stubWorkOrderParkStore) ListBlockedByResource(_ context.Context, resource string, _ int) ([]domain.BoardTask, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	if resource != domain.ResourceWorkOrder {
		return nil, nil
	}
	return s.parked, nil
}

func (s *stubWorkOrderParkStore) ClearWorkOrderWaiting(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	if s.takeMisses[taskID] {
		return domain.BoardTask{}, false, nil
	}
	s.takenIDs = append(s.takenIDs, taskID)
	for _, t := range s.parked {
		if t.ID == taskID {
			return t, true, nil
		}
	}
	return domain.BoardTask{}, false, nil
}

func TestWorkOrderSweepLeavesATaskParkedWhileItsBlockerIsOpen(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	blockers := &stubBlockerReader{byTask: map[uuid.UUID][]domain.BoardTask{task.ID: {openBlocker()}}}
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, blockers, &Dispatcher{})

	s.sweep(context.Background())

	assert.Equal(t, 1, blockers.calls, "one query per parked task, no agent run")
	assert.Empty(t, store.takenIDs, "the blocker has not landed, so the task stays parked")
}

// The release. ListBlockingSources filters out blockers that reached done or
// released, so an empty answer IS "everything it was waiting for is finished".
func TestWorkOrderSweepReleasesTheTaskWhenItsBlockerLands(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, task.ID, store.takenIDs[0])
}

// A cancelled blocker is a DELETED task, and deleting one cascades its
// task_relations rows away (migration 022). The dependent must not be left
// waiting for a task that no longer exists — which falls out of the same query,
// with nothing extra to remember.
func TestWorkOrderSweepReleasesTheTaskWhenItsBlockerIsDeleted(t *testing.T) {
	waiting := workOrderTask(domain.TaskColumnBlocked)
	stillBlocked := workOrderTask(domain.TaskColumnBlocked)
	blockers := &stubBlockerReader{byTask: map[uuid.UUID][]domain.BoardTask{
		// waiting's edge is gone with the deleted blocker; the other one's is not.
		stillBlocked.ID: {openBlocker()},
	}}
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{waiting, stillBlocked}}
	s := NewWorkOrderSweeper(store, blockers, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, waiting.ID, store.takenIDs[0])
}

// Per-task, not a queue: the second parked task's blocker may land first.
func TestWorkOrderSweepClaimsOnlyTheTasksThatAreFree(t *testing.T) {
	first := workOrderTask(domain.TaskColumnBlocked)
	second := workOrderTask(domain.TaskColumnBlocked)
	blockers := &stubBlockerReader{byTask: map[uuid.UUID][]domain.BoardTask{first.ID: {openBlocker()}}}
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{first, second}}
	s := NewWorkOrderSweeper(store, blockers, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, second.ID, store.takenIDs[0],
		"the later task whose blocker finished must be resumed, not the older one still waiting")
}

// An unreadable graph leaves the task parked: resuming on an error spends an
// agent run to be told what the sweep could not find out.
func TestWorkOrderSweepLeavesTaskParkedWhenTheGraphCannotBeRead(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{err: errors.New("db down")}, &Dispatcher{})

	s.sweep(context.Background())

	assert.Empty(t, store.takenIDs)
}

// Another pod, or a human dragging the card out of blocked, may claim it first.
func TestWorkOrderSweepToleratesLosingTheClaim(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{
		parked:     []domain.BoardTask{task},
		takeMisses: map[uuid.UUID]bool{task.ID: true},
	}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})

	s.sweep(context.Background()) // must not panic or dispatch
}
