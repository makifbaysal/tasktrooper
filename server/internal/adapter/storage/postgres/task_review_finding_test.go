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

// ReviewFindingSuite covers task_review_findings and task_review_flag_acks
// (migration 202).
type ReviewFindingSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	tasks  *postgres.BoardTaskStore
	store  *postgres.ReviewFindingStore
	repoID uuid.UUID
}

func TestReviewFindingSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ReviewFindingSuite))
}

func (s *ReviewFindingSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	db := postgres.NewDB(s.pool)
	s.tasks = postgres.NewBoardTaskStore(db)
	s.store = postgres.NewReviewFindingStore(db)
	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "review-findings", "", "/tmp/review-findings-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ReviewFindingSuite) TearDownSuite() {
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

func (s *ReviewFindingSuite) newTask() domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "t", TaskType: "task",
		Column: domain.TaskColumnCodeReview, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *ReviewFindingSuite) TestARoundTripKeepsEveryKindOfItem() {
	task := s.newTask()
	other := s.newTask()
	now := time.Now().UTC().Truncate(time.Millisecond)
	crit := uuid.New()
	report := domain.ReviewReportInput{
		Findings:         []domain.ReviewFindingInput{{Key: "A-1", File: "a.go", Line: 3, Severity: "high", Category: "bug", Message: "nil deref", Confidence: "low", HumanReviewReason: "could not run it", Status: "open"}},
		NeedsHumanReview: []domain.HumanReviewInput{{File: "pay.go", Line: 9, Reason: "payment path", Question: "retry?"}},
		NotReviewed:      []domain.NotReviewedInput{{Scope: "web/", Reason: "no browser"}},
		ReviewedAspects:  []string{"security"},
	}
	s.Require().NoError(report.Validate())
	rows := report.Rows(task.ID, "code_review", nil, "arch", "abc123", now)
	rows = append(rows, domain.TaskReviewFinding{
		TaskID: task.ID, Kind: domain.ReviewItemFinding, Gate: "in_qa", Fingerprint: domain.CriterionFingerprint(crit),
		Category: domain.ReviewCategoryAcceptanceCriterion, Message: "fails", Status: domain.ReviewFindingOpen, CriterionID: &crit, CreatedAt: now,
	})
	s.Require().NoError(s.store.Insert(s.ctx, rows))
	s.Require().NoError(s.store.Insert(s.ctx, []domain.TaskReviewFinding{{
		TaskID: other.ID, Kind: domain.ReviewItemFinding, Gate: "code_review", Fingerprint: "x", Message: "other task", Status: "open", CreatedAt: now,
	}}))

	got, err := s.store.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Require().Len(got, 5)
	byKind := map[string]domain.TaskReviewFinding{}
	for _, r := range got {
		byKind[r.Kind+"/"+r.Gate] = r
	}
	f := byKind["finding/code_review"]
	s.Equal("nil deref", f.Message)
	s.Equal("low", f.Confidence)
	s.Equal("could not run it", f.HumanReviewReason)
	s.Equal("abc123", f.HeadSHA)
	s.Equal(domain.ReviewFingerprint("code_review", "A-1", "a.go", "bug", "nil deref"), f.Fingerprint)
	s.Equal("retry?", byKind["human_review/code_review"].Question)
	s.Equal("web/", byKind["not_reviewed/code_review"].Scope)
	s.Equal("no browser", byKind["not_reviewed/code_review"].Reason)
	s.Equal("security", byKind["aspect/code_review"].Message)
	s.Equal(&crit, byKind["finding/in_qa"].CriterionID)
}

func (s *ReviewFindingSuite) TestAckIsIdempotentAndKeepsTheFirstReviewer() {
	task := s.newTask()
	first, err := s.store.Ack(s.ctx, domain.ReviewFlagAck{TaskID: task.ID, FlagKey: "auto-large_diff-40", ReviewedBy: "ayse", ReviewedAt: time.Now().UTC()})
	s.Require().NoError(err)
	second, err := s.store.Ack(s.ctx, domain.ReviewFlagAck{TaskID: task.ID, FlagKey: "auto-large_diff-40", ReviewedBy: "mehmet", ReviewedAt: time.Now().UTC().Add(time.Hour)})
	s.Require().NoError(err)
	s.Equal("ayse", second.ReviewedBy)
	s.True(first.ReviewedAt.Equal(second.ReviewedAt))
	acks, err := s.store.ListAcks(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Len(acks, 1)
}

func (s *ReviewFindingSuite) TestFindingsGoWithTheirTask() {
	task := s.newTask()
	s.Require().NoError(s.store.Insert(s.ctx, []domain.TaskReviewFinding{{
		TaskID: task.ID, Kind: "finding", Fingerprint: "x", Message: "m", Status: "open", CreatedAt: time.Now(),
	}}))
	_, err := s.pool.Exec(s.ctx, `DELETE FROM board_tasks WHERE id = $1`, task.ID)
	s.Require().NoError(err)
	got, err := s.store.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	s.Empty(got)
}
