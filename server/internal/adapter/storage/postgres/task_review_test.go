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

// TaskReviewSuite covers the SQL behind the code_review quorum: the upsert a
// reviewer that changes its mind relies on, the enabled-subscriber roster that
// decides who the card waits for, and the role migration 180 adds.
type TaskReviewSuite struct {
	suite.Suite
	ctx     context.Context
	cancel  context.CancelFunc
	pg      *database.Embedded
	pool    *pgxpool.Pool
	db      *postgres.DB
	reviews *postgres.TaskReviewStore
	spans   *postgres.TaskColumnSpanStore
	tasks   *postgres.BoardTaskStore
	agents  *postgres.CatalogStore
	board   *postgres.BoardConfigStore
	repoID  uuid.UUID
}

func TestTaskReviewSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(TaskReviewSuite))
}

func (s *TaskReviewSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.reviews = postgres.NewTaskReviewStore(s.db)
	s.spans = postgres.NewTaskColumnSpanStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.board = postgres.NewBoardConfigStore(s.db)

	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, "task-review-test", "", "/tmp/task-review-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *TaskReviewSuite) TearDownSuite() {
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

func (s *TaskReviewSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "task-review-test",
		TaskType: "task", Column: domain.TaskColumnCodeReview,
		Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *TaskReviewSuite) newAgent(name string, enabled bool) domain.Agent {
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{
		Name: name + "-" + uuid.NewString()[:8], ProviderType: domain.LLMProviderAnthropic,
		Model: "test-model", Enabled: enabled,
	})
	s.Require().NoError(err)
	return agent
}

func (s *TaskReviewSuite) TestTheSecurityRoleIsSeeded() {
	var name string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT name FROM roles WHERE key = 'security'`).Scan(&name))
	s.Equal("Security Reviewer", name)
}

func (s *TaskReviewSuite) TestRequiredReviewersAreTheEnabledSubscribers() {
	column := "code_review"
	architect := s.newAgent("a-architect", true)
	security := s.newAgent("b-security", true)
	disabled := s.newAgent("c-disabled", false)
	for _, a := range []domain.Agent{architect, security, disabled} {
		s.Require().NoError(s.board.SetAgentSubscriptions(s.ctx, a.ID, []string{column}))
	}
	defer func() {
		for _, a := range []domain.Agent{architect, security, disabled} {
			_ = s.board.SetAgentSubscriptions(s.ctx, a.ID, nil)
		}
	}()

	got, err := s.reviews.RequiredReviewers(s.ctx, column, "task")
	s.Require().NoError(err)

	ids := map[uuid.UUID]string{}
	for _, r := range got {
		ids[r.ID] = r.Name
	}
	s.Contains(ids, architect.ID)
	s.Contains(ids, security.ID)
	s.NotContains(ids, disabled.ID, "a disabled reviewer never runs, so the card must not wait for it")
	s.Equal(architect.Name, ids[architect.ID])
}

func (s *TaskReviewSuite) TestAVerdictIsUpsertedPerReviewerAndSpan() {
	task := s.newTask()
	reviewer := s.newAgent("reviewer", true)
	at := time.Now().UTC().Truncate(time.Second)
	s.Require().NoError(s.spans.RecordMove(s.ctx, s.repoID, task.ID, "in_progress", at.Add(-time.Hour)))
	s.Require().NoError(s.spans.RecordMove(s.ctx, s.repoID, task.ID, "code_review", at))

	spans, err := s.reviews.TaskSpans(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(spans, 2)
	s.Equal("in_progress", spans[0].BoardColumn, "oldest first")
	review := spans[1]
	s.Nil(review.LeftAt)

	agentID := reviewer.ID
	s.Require().NoError(s.reviews.RecordReviewVerdict(s.ctx, domain.TaskReviewVerdict{
		TaskID: task.ID, SpanID: review.ID, AgentID: &agentID, Verdict: domain.ReviewVerdictReject,
	}))
	s.Require().NoError(s.reviews.RecordReviewVerdict(s.ctx, domain.TaskReviewVerdict{
		TaskID: task.ID, SpanID: review.ID, AgentID: &agentID, Verdict: domain.ReviewVerdictApprove,
	}))

	verdicts, err := s.reviews.ListReviewVerdicts(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(verdicts, 1, "the second verdict replaces the first")
	s.Equal(domain.ReviewVerdictApprove, verdicts[0].Verdict)
	s.Equal(reviewer.Name, verdicts[0].AgentName, "the name is read off the agent row")
	s.Equal(review.ID, verdicts[0].SpanID)

	err = s.reviews.RecordReviewVerdict(s.ctx, domain.TaskReviewVerdict{
		TaskID: task.ID, SpanID: review.ID, AgentID: &agentID, Verdict: "maybe",
	})
	s.Error(err, "the CHECK constraint keeps the ledger to approve/reject")
}
