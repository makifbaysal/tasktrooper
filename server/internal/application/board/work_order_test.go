package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func workOrderGateWorkflow() domain.Workflow {
	return workflowtest.Default().Workflows["task"]
}

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
		TaskType:     "task",
	}
}

func openBlocker() domain.BoardTask {
	return domain.BoardTask{
		ID:       uuid.New(),
		Key:      "T-1",
		Title:    "API migration",
		Column:   domain.TaskColumnInProgress,
		TaskType: "task",
	}
}

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

func TestWorkOrderParkWorksWithoutACommenter(t *testing.T) {
	task := workOrderTask(domain.TaskColumnTodo)
	parker := &stubResourceParker{}
	w := NewWorkOrder(&stubBlockerReader{}, parker)

	require.NoError(t, w.Park(context.Background(), task.RepositoryID, task, []domain.BoardTask{openBlocker()}))
	assert.Len(t, parker.parks, 1)
}

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
	wf := workOrderGateWorkflow()
	for _, tc := range cases {
		t.Run(string(tc.column), func(t *testing.T) {
			got := workOrderGateApplies(wf, true, DispatchInput{Task: workOrderTask(tc.column)})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWorkOrderGateSkipsTheSweepersResume(t *testing.T) {
	wf := workOrderGateWorkflow()
	input := DispatchInput{
		Task: workOrderTask(domain.TaskColumnTodo),
		Payload: map[string]interface{}{
			domain.EventPayloadResumedResource: domain.ResourceWorkOrder,
		},
	}
	assert.False(t, workOrderGateApplies(wf, true, input))

	input.Payload[domain.EventPayloadResumedResource] = domain.ResourceMobileDevice
	assert.True(t, workOrderGateApplies(wf, true, input))
}

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

func TestWorkOrderSweepReleasesTheTaskWhenItsBlockerLands(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, task.ID, store.takenIDs[0])
}

func TestWorkOrderSweepReleasesTheTaskWhenItsBlockerIsDeleted(t *testing.T) {
	waiting := workOrderTask(domain.TaskColumnBlocked)
	stillBlocked := workOrderTask(domain.TaskColumnBlocked)
	blockers := &stubBlockerReader{byTask: map[uuid.UUID][]domain.BoardTask{
		stillBlocked.ID: {openBlocker()},
	}}
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{waiting, stillBlocked}}
	s := NewWorkOrderSweeper(store, blockers, &Dispatcher{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	assert.Equal(t, waiting.ID, store.takenIDs[0])
}

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

func TestWorkOrderSweepLeavesTaskParkedWhenTheGraphCannotBeRead(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{err: errors.New("db down")}, &Dispatcher{})

	s.sweep(context.Background())

	assert.Empty(t, store.takenIDs)
}

func TestWorkOrderSweepToleratesLosingTheClaim(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{
		parked:     []domain.BoardTask{task},
		takeMisses: map[uuid.UUID]bool{task.ID: true},
	}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})

	s.sweep(context.Background())
}

func TestWorkOrderSweepPostsACommentOnASuccessfulResume(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	comments := &stubWorkOrderCommenter{}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})
	s.SetCommenter(comments)

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1)
	require.Len(t, comments.contents, 1)
	assert.Contains(t, comments.contents[0], "resumed automatically")
}

type failingWorkOrderCommenter struct{}

func (failingWorkOrderCommenter) AddComment(context.Context, uuid.UUID, uuid.UUID, domain.CreateTaskCommentRequest) (domain.TaskComment, error) {
	return domain.TaskComment{}, errors.New("comment store unavailable")
}

func TestWorkOrderSweepResumeSurvivesACommentFailure(t *testing.T) {
	task := workOrderTask(domain.TaskColumnBlocked)
	store := &stubWorkOrderParkStore{parked: []domain.BoardTask{task}}
	s := NewWorkOrderSweeper(store, &stubBlockerReader{}, &Dispatcher{})
	s.SetCommenter(failingWorkOrderCommenter{})

	s.sweep(context.Background())

	require.Len(t, store.takenIDs, 1, "the resume itself must still happen")
}

func TestWorkOrderParkAsksTheSweeperToLookOnceTheParkIsWritten(t *testing.T) {
	cases := []struct {
		name      string
		parked    bool
		parkErr   error
		wantHooks int
	}{
		{"park written", false, nil, 1},
		{"already parked", true, nil, 0},
		{"park write failed", false, errors.New("db down"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := workOrderTask(domain.TaskColumnTodo)
			if tc.parked {
				task.BlockedResource = domain.ResourceWorkOrder
			}
			parker := &stubResourceParker{err: tc.parkErr}
			w := NewWorkOrder(&stubBlockerReader{}, parker)
			hooks := 0
			w.SetParkedHook(func() {
				hooks++
				assert.Len(t, parker.parks, 1, "the hook runs after the park exists")
			})

			_ = w.Park(context.Background(), task.RepositoryID, task, []domain.BoardTask{openBlocker()})

			assert.Equal(t, tc.wantHooks, hooks)
		})
	}
}
