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

// ConfirmBeforeDeploySuite covers the postgres board_tasks column the F3
// before/after-deploy runbook needs: before_deploy_confirmed_at, read through
// scanBoardTask (Get/Update) and written by ConfirmBeforeDeploy.
type ConfirmBeforeDeploySuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestConfirmBeforeDeploySuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ConfirmBeforeDeploySuite))
}

func (s *ConfirmBeforeDeploySuite) SetupSuite() {
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
	repo, err := repos.Create(s.ctx, "confirm-before-deploy-test", "", "/tmp/confirm-before-deploy-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ConfirmBeforeDeploySuite) TearDownSuite() {
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

func (s *ConfirmBeforeDeploySuite) newTask(beforeDeploy string) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	req := domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "confirm-before-deploy-test",
		TaskType:     "task",
		Column:       domain.TaskColumnDone,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	}
	if beforeDeploy != "" {
		req.BeforeDeploy = &beforeDeploy
	}
	task, err := s.tasks.Create(s.ctx, req)
	s.Require().NoError(err)
	return task
}

// A freshly created task with before_deploy steps written is unconfirmed: the
// column round-trips through Get exactly like the other deploy runbook
// fields.
func (s *ConfirmBeforeDeploySuite) TestNewTaskWithBeforeDeployIsUnconfirmed() {
	task := s.newTask("run the migration")
	s.Require().NotNil(task.BeforeDeploy)
	s.Equal("run the migration", *task.BeforeDeploy)
	s.Nil(task.BeforeDeployConfirmedAt)
	s.True(task.BeforeDeployPending())
}

// ConfirmBeforeDeploy stamps the column, and the stamp is visible on a
// subsequent Get — the same column, not a side channel.
func (s *ConfirmBeforeDeploySuite) TestConfirmBeforeDeployStampsConfirmation() {
	task := s.newTask("flip the feature flag")

	confirmed, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(confirmed.BeforeDeployConfirmedAt)
	s.False(confirmed.BeforeDeployPending())

	reread, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(reread.BeforeDeployConfirmedAt)
	s.WithinDuration(*confirmed.BeforeDeployConfirmedAt, *reread.BeforeDeployConfirmedAt, time.Second)
}

// Confirming twice must not restart the "confirmed <time>" clock the UI
// shows: the second call is a no-op on the timestamp (COALESCE), not a
// refresh.
func (s *ConfirmBeforeDeploySuite) TestConfirmBeforeDeployIsIdempotent() {
	task := s.newTask("rotate the secret")

	first, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(first.BeforeDeployConfirmedAt)

	time.Sleep(10 * time.Millisecond)

	second, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(second.BeforeDeployConfirmedAt)
	s.Equal(first.BeforeDeployConfirmedAt.UTC(), second.BeforeDeployConfirmedAt.UTC())
}

func (s *ConfirmBeforeDeploySuite) TestConfirmBeforeDeployOnMissingTaskReturnsNotFound() {
	_, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, uuid.New())
	s.Require().Error(err)
	s.ErrorIs(err, domain.ErrBoardTaskNotFound)
}

// Update() writes BeforeDeployConfirmedAt exactly like every other deploy
// runbook field: a caller that clears it (the clear-on-edit rule in
// repository.Service.UpdateTask) sees the column go back to NULL.
func (s *ConfirmBeforeDeploySuite) TestUpdateClearsBeforeDeployConfirmedAt() {
	task := s.newTask("check the dashboard")
	confirmed, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(confirmed.BeforeDeployConfirmedAt)

	edited := "check the dashboard, differently"
	confirmed.BeforeDeploy = &edited
	confirmed.BeforeDeployConfirmedAt = nil
	updated, err := s.tasks.Update(s.ctx, confirmed)
	s.Require().NoError(err)
	s.Nil(updated.BeforeDeployConfirmedAt)
	s.True(updated.BeforeDeployPending())
}
