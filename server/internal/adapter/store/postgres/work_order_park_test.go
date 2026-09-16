package postgres_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// WorkOrderParkSuite covers the work_order resource's own park pair, the only
// one of the five blocked_resource kinds that must NOT move board_column: a
// `blocks` wait is a wait on another card on the same board, not on anything
// outside it, so hiding the task in the shared `blocked` column would take it
// out of a todo/in_progress view for no reason the other four resources share.
type WorkOrderParkSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestWorkOrderParkSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(WorkOrderParkSuite))
}

func (s *WorkOrderParkSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "work-order-park-test", "", "/tmp/work-order-park-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *WorkOrderParkSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *WorkOrderParkSuite) newTask(column domain.TaskColumn) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, domain.TaskTypeTask)
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "work-order-park-test",
		TaskType:     domain.TaskTypeTask,
		Column:       column,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// The column must not move for either of the two dispatchable columns.
func (s *WorkOrderParkSuite) TestMarkWorkOrderWaitingLeavesTodoInPlace() {
	task := s.newTask(domain.TaskColumnTodo)

	err := s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-12 (API migration) [in_progress] to finish")
	s.Require().NoError(err)

	marked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnTodo, marked.Column, "work_order parks in place, not into blocked")
	s.Equal(domain.ResourceWorkOrder, marked.BlockedResource)
	s.Equal("waiting for T-12 (API migration) [in_progress] to finish", marked.BlockedQuestion)
	s.Require().NotNil(marked.BlockedAt)
	s.Equal(domain.TaskColumn(""), marked.BlockedOriginColumn, "nothing to restore, the column never moved")
}

func (s *WorkOrderParkSuite) TestMarkWorkOrderWaitingLeavesInProgressInPlace() {
	task := s.newTask(domain.TaskColumnInProgress)

	err := s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-9 to finish")
	s.Require().NoError(err)

	marked, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnInProgress, marked.Column)
	s.Equal(domain.ResourceWorkOrder, marked.BlockedResource)
}

// A task deleted mid-dispatch is nothing to park, same as BlockOnResource.
func (s *WorkOrderParkSuite) TestMarkWorkOrderWaitingOnAMissingTaskIsANoOp() {
	err := s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, uuid.New(), "waiting for T-1 to finish")
	s.Require().NoError(err)
}

func (s *WorkOrderParkSuite) TestClearWorkOrderWaitingClearsMarkersAndLeavesColumn() {
	task := s.newTask(domain.TaskColumnTodo)
	s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-1 to finish"))

	cleared, ok, err := s.tasks.ClearWorkOrderWaiting(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(domain.TaskColumnTodo, cleared.Column, "still exactly where it was")
	// The returned row is the pre-UPDATE snapshot, same as TakeBlockedBySession
	// and TakeBlockedResourceTask — it is what lets a caller see what the task
	// was waiting for. The follow-up Get below is what shows the persisted clear.
	s.Equal(domain.ResourceWorkOrder, cleared.BlockedResource)

	fresh, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.TaskColumnTodo, fresh.Column)
	s.Equal("", fresh.BlockedResource)
	s.Equal("", fresh.BlockedQuestion)
	s.Nil(fresh.BlockedAt)
}

// A task parked on a DIFFERENT resource must not be cleared by this call — it
// is still owned by whichever resource actually set it.
func (s *WorkOrderParkSuite) TestClearWorkOrderWaitingIgnoresOtherResources() {
	task := s.newTask(domain.TaskColumnInQA)
	_, err := s.tasks.BlockOnResource(s.ctx, s.repoID, task.ID, domain.ResourceMobileDevice, "device held")
	s.Require().NoError(err)

	_, ok, err := s.tasks.ClearWorkOrderWaiting(s.ctx, task.ID)
	s.Require().NoError(err)
	s.False(ok)

	still, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(domain.ResourceMobileDevice, still.BlockedResource)
}

func (s *WorkOrderParkSuite) TestClearWorkOrderWaitingOnAnUnparkedTaskIsANoOp() {
	task := s.newTask(domain.TaskColumnTodo)

	_, ok, err := s.tasks.ClearWorkOrderWaiting(s.ctx, task.ID)
	s.Require().NoError(err)
	s.False(ok)
}
