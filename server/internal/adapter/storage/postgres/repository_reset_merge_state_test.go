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

// ResetMergeStateSuite covers the postgres implementation of the one method a
// release rollback needs on the board-task store: undo what SetTaskPullRequest
// and SetTaskMergeCommit recorded, so the reopened task looks unmerged again
// and can pick up a fresh PR.
type ResetMergeStateSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestResetMergeStateSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ResetMergeStateSuite))
}

func (s *ResetMergeStateSuite) SetupSuite() {
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
	repo, err := repos.Create(s.ctx, "reset-merge-state-test", "", "/tmp/reset-merge-state-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ResetMergeStateSuite) TearDownSuite() {
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

func (s *ResetMergeStateSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "reset-merge-state-test",
		TaskType:     "task",
		Column:       domain.TaskColumnDone,
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task
}

// A merged, reviewed, verified task must come back exactly as if it had never
// gone through review: no PR, no merge commit, no verified sha. Those are the
// three fields a rollback's revert has to erase before the task can go through
// review again and land a fresh PR (and so the deploy sweeper — which keys off
// merge_commit_sha IS NOT NULL — stops watching a commit production no longer
// runs).
func (s *ResetMergeStateSuite) TestResetMergeStateClearsPRMergeAndVerifiedSHA() {
	task := s.newTask()

	err := s.tasks.SetTaskPullRequest(s.ctx, task.ID, "https://github.com/acme/repo/pull/42", 42)
	s.Require().NoError(err)
	err = s.tasks.SetTaskMergeCommit(s.ctx, task.ID, "abc123def456")
	s.Require().NoError(err)
	task.VerifiedSHA = "abc123def456"
	_, err = s.tasks.Update(s.ctx, task)
	s.Require().NoError(err)

	merged, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal("https://github.com/acme/repo/pull/42", merged.PRURL)
	s.Equal(42, merged.PRNumber)
	s.Equal("abc123def456", merged.MergeCommitSHA)
	s.Equal("abc123def456", merged.VerifiedSHA)

	err = s.tasks.ResetMergeState(s.ctx, task.ID)
	s.Require().NoError(err)

	reset, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal("", reset.PRURL)
	s.Equal(0, reset.PRNumber)
	s.Equal("", reset.MergeCommitSHA)
	s.Equal("", reset.VerifiedSHA)

	// merge_commit_sha's partial index (migration 105) is WHERE NOT NULL — the
	// deploy watch's whole "find the task for this commit" query relies on a
	// reset task no longer matching, not just reading back as "".
	_, err = s.tasks.FindTaskByMergeCommit(s.ctx, s.repoID, "abc123def456")
	s.Require().Error(err, "a reset task's old merge commit must no longer resolve")
}

// A task that never merged has nothing to reset; the call is a harmless no-op,
// the same shape as SetTaskMergeCommit on a missing task.
func (s *ResetMergeStateSuite) TestResetMergeStateOnAnUnmergedTaskIsANoOp() {
	task := s.newTask()

	err := s.tasks.ResetMergeState(s.ctx, task.ID)
	s.Require().NoError(err)

	got, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal("", got.PRURL)
	s.Equal("", got.MergeCommitSHA)
	s.Equal("", got.VerifiedSHA)
}

// A missing task is nothing to reset, not an error — the same contract
// SetTaskMergeCommit and BlockOnResource already give a deleted task.
func (s *ResetMergeStateSuite) TestResetMergeStateOnAMissingTaskIsANoOp() {
	err := s.tasks.ResetMergeState(s.ctx, uuid.New())
	s.Require().NoError(err)
}

// L6: a rollback's revert sends the task through review again for a fresh PR
// against reworked code. The before-deploy confirmation a human gave the OLD
// change must not silently carry over and let the new PR skip the gate.
func (s *ResetMergeStateSuite) TestResetMergeStateClearsBeforeDeployConfirmation() {
	task := s.newTask()

	confirmed, err := s.tasks.ConfirmBeforeDeploy(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Require().NotNil(confirmed.BeforeDeployConfirmedAt)

	err = s.tasks.ResetMergeState(s.ctx, task.ID)
	s.Require().NoError(err)

	reset, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Nil(reset.BeforeDeployConfirmedAt, "before-deploy confirmation must not survive a merge-state reset")
}
