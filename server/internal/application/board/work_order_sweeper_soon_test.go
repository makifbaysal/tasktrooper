package board_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/board"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type parkedWorkOrderTasks struct {
	mu      sync.Mutex
	parked  map[uuid.UUID]domain.BoardTask
	lists   int
	cleared chan uuid.UUID
}

func (p *parkedWorkOrderTasks) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lists++
	out := make([]domain.BoardTask, 0, len(p.parked))
	for _, t := range p.parked {
		out = append(out, t)
	}
	return out, nil
}

func (p *parkedWorkOrderTasks) ClearWorkOrderWaiting(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	p.mu.Lock()
	task, ok := p.parked[taskID]
	delete(p.parked, taskID)
	p.mu.Unlock()
	if ok {
		p.cleared <- taskID
	}
	return task, ok, nil
}

func (p *parkedWorkOrderTasks) park(task domain.BoardTask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.parked[task.ID] = task
}

func (p *parkedWorkOrderTasks) listCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lists
}

type noBlockers struct{}

func (noBlockers) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

type WorkOrderSweepSoonSuite struct {
	suite.Suite
	tasks   *parkedWorkOrderTasks
	sweeper *board.WorkOrderSweeper
	cancel  context.CancelFunc
}

func TestWorkOrderSweepSoonSuite(t *testing.T) {
	suite.Run(t, new(WorkOrderSweepSoonSuite))
}

func (s *WorkOrderSweepSoonSuite) SetupTest() {
	s.tasks = &parkedWorkOrderTasks{parked: map[uuid.UUID]domain.BoardTask{}, cleared: make(chan uuid.UUID, 4)}
	disp := board.NewDispatcher(&fakeBoardConfigStore{}, &fakeEventStore{}, &fakeRunStore{}, &fakeRunner{}, true)
	disp.SetWorkflows(workflowtest.Default().Reader())
	s.sweeper = board.NewWorkOrderSweeper(s.tasks, noBlockers{}, disp)
	var ctx context.Context
	ctx, s.cancel = context.WithCancel(context.Background())
	s.sweeper.Start(ctx, time.Hour)
	s.Require().Eventually(func() bool { return s.tasks.listCount() >= 1 }, time.Second, 5*time.Millisecond)
}

func (s *WorkOrderSweepSoonSuite) TearDownTest() {
	s.cancel()
}

func (s *WorkOrderSweepSoonSuite) TestIntervalIsAFiveMinuteBackstop() {
	s.Equal(5*time.Minute, board.WorkOrderSweeperInterval)
}

func (s *WorkOrderSweepSoonSuite) TestSweepSoonFreesAParkWithoutWaitingForTheInterval() {
	task := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Key: "T-9", Column: domain.TaskColumnTodo, TaskType: "task"}
	s.tasks.park(task)

	s.sweeper.SweepSoon()

	select {
	case id := <-s.tasks.cleared:
		s.Equal(task.ID, id)
	case <-time.After(time.Second):
		s.Fail("SweepSoon did not run a pass")
	}
}

func (s *WorkOrderSweepSoonSuite) TestSweepSoonNeverBlocks() {
	done := make(chan struct{})
	go func() {
		for range 10 {
			s.sweeper.SweepSoon()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		s.Fail("SweepSoon blocked")
	}
}
