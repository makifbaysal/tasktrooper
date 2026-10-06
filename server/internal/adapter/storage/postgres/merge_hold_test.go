package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// MergeHoldSuite covers the merge-hold park release.Service.MergeGate writes on
// a done task: like work_order it never moves board_column, and unlike every
// other park it survives an Update that leaves the task in its column.
type MergeHoldSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestMergeHoldSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(MergeHoldSuite))
}

func (s *MergeHoldSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)

	repos := postgres.NewRepositoryStore(s.db)
	repo, err := repos.Create(s.ctx, "merge-hold-test", "", "/tmp/merge-hold-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *MergeHoldSuite) TearDownSuite() {
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

func (s *MergeHoldSuite) newDoneTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	steps := "flip the feature flag"
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "merge-hold-test",
		TaskType:     "task",
		Column:       domain.TaskColumnDone,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
		BeforeDeploy: &steps,
	})
	s.Require().NoError(err)
	return task
}

func (s *MergeHoldSuite) get(id uuid.UUID) domain.BoardTask {
	task, err := s.tasks.Get(s.ctx, s.repoID, id)
	s.Require().NoError(err)
	return task
}

func (s *MergeHoldSuite) TestHoldMergeRecordsTheReasonWithoutMovingTheTask() {
	task := s.newDoneTask()

	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 (API) [done]"))

	held := s.get(task.ID)
	s.Equal(domain.TaskColumnDone, held.Column)
	s.Equal(domain.ResourceDeployOrder, held.BlockedResource)
	s.Equal("T-1 (API) [done]", held.BlockedQuestion)
	s.NotNil(held.BlockedAt)
}

func (s *MergeHoldSuite) TestHoldMergeKeepsTheWaitClockWhileTheReasonStaysTheSame() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [todo]"))
	first := s.get(task.ID).BlockedAt
	time.Sleep(10 * time.Millisecond)

	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [done]"))
	s.Equal(first.UTC(), s.get(task.ID).BlockedAt.UTC())

	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceBeforeDeploy, ""))
	moved := s.get(task.ID)
	s.True(moved.BlockedAt.After(*first))
	s.Empty(moved.BlockedQuestion)
}

func (s *MergeHoldSuite) TestHoldMergeNeverOverwritesAnotherPark() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-1"))

	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [done]"))

	s.Equal(domain.ResourceWorkOrder, s.get(task.ID).BlockedResource)
}

func (s *MergeHoldSuite) TestReleaseMergeHoldClearsOnlyTheNamedResources() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [done]"))

	s.Require().NoError(s.tasks.ReleaseMergeHold(s.ctx, task.ID, domain.ResourceDeliveryProfile))
	s.Equal(domain.ResourceDeployOrder, s.get(task.ID).BlockedResource)

	s.Require().NoError(s.tasks.ReleaseMergeHold(s.ctx, task.ID, domain.ResourceDeployOrder))
	cleared := s.get(task.ID)
	s.Empty(cleared.BlockedResource)
	s.Empty(cleared.BlockedQuestion)
	s.Nil(cleared.BlockedAt)
}

func (s *MergeHoldSuite) TestReleaseMergeHoldLeavesAnotherParkAlone() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-1"))

	s.Require().NoError(s.tasks.ReleaseMergeHold(s.ctx, task.ID, domain.ResourceWorkOrder))

	s.Equal(domain.ResourceWorkOrder, s.get(task.ID).BlockedResource)
}

// Editing a done task (or regenerating its order note) goes through Update
// without a move; that resolves nothing the merge waits for.
func (s *MergeHoldSuite) TestUpdateInTheSameColumnKeepsTheHold() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [done]"))

	edited := s.get(task.ID)
	edited.Description = "clarified"
	updated, err := s.tasks.Update(s.ctx, edited)
	s.Require().NoError(err)

	s.Equal(domain.ResourceDeployOrder, updated.BlockedResource)
	s.Equal("T-1 [done]", updated.BlockedQuestion)
	s.NotNil(updated.BlockedAt)
}

func (s *MergeHoldSuite) TestMovingTheTaskClearsTheHold() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, task.ID, domain.ResourceDeployOrder, "T-1 [done]"))

	moved := s.get(task.ID)
	moved.Column = domain.TaskColumnNeedRevision
	updated, err := s.tasks.Update(s.ctx, moved)
	s.Require().NoError(err)

	s.Empty(updated.BlockedResource)
	s.Empty(updated.BlockedQuestion)
	s.Nil(updated.BlockedAt)
}

func (s *MergeHoldSuite) TestUpdateStillClearsANonMergeParkInTheSameColumn() {
	task := s.newDoneTask()
	s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "waiting for T-1"))

	updated, err := s.tasks.Update(s.ctx, s.get(task.ID))
	s.Require().NoError(err)

	s.Empty(updated.BlockedResource)
}

func (s *MergeHoldSuite) TestConfirmingBeforeDeployClearsOnlyItsOwnHold() {
	waitingOnSteps := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, waitingOnSteps.ID, domain.ResourceBeforeDeploy, "flip the feature flag"))
	confirmed, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, waitingOnSteps.ID)
	s.Require().NoError(err)
	s.Empty(confirmed.BlockedResource)
	s.Nil(confirmed.BlockedAt)

	waitingOnOrder := s.newDoneTask()
	s.Require().NoError(s.tasks.HoldMerge(s.ctx, s.repoID, waitingOnOrder.ID, domain.ResourceDeployOrder, "T-1 [done]"))
	confirmed, err = s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, waitingOnOrder.ID)
	s.Require().NoError(err)
	s.Equal(domain.ResourceDeployOrder, confirmed.BlockedResource)
}
