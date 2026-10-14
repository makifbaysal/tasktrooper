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
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type FindingStoreSuite struct {
	suite.Suite
	ctx      context.Context
	cancel   context.CancelFunc
	pg       *database.Embedded
	pool     *pgxpool.Pool
	db       *postgres.DB
	store    *postgres.FindingStore
	model    *postgres.ProjectModelStore
	tasks    *postgres.BoardTaskStore
	repoID   uuid.UUID
	compID   uuid.UUID
	scanID   uuid.UUID
	repoName string
}

func TestFindingStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(FindingStoreSuite))
}

func (s *FindingStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewFindingStore(s.db)
	s.model = postgres.NewProjectModelStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)
}

func (s *FindingStoreSuite) SetupTest() {
	repos := postgres.NewRepositoryStore(s.db)
	s.repoName = "findings-" + uuid.NewString()
	repo, err := repos.Create(s.ctx, s.repoName, "", "/tmp/"+s.repoName, "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
	c, err := s.model.SaveComponent(s.ctx, domain.Component{RepositoryID: repo.ID, Path: "api", Status: domain.ComponentStatusActive})
	s.Require().NoError(err)
	s.compID = c.ID
	s.scanID = uuid.New()
}

func (s *FindingStoreSuite) TearDownSuite() {
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

func (s *FindingStoreSuite) finding(rule string, sev domain.FindingSeverity, component bool) domain.ComponentFinding {
	f := domain.ComponentFinding{
		ID: uuid.New(), RepositoryID: s.repoID, Rule: rule, Category: domain.FindingLogging, Severity: sev,
		Occurrences: 3, Status: domain.FindingOpen, LastScanID: &s.scanID,
		Evidence: []domain.SourceEvidence{{Path: "api/main.go", Line: 7, Note: "fmt.Println(x)"}},
	}
	if component {
		f.ComponentID = &s.compID
	}
	return f
}

func (s *FindingStoreSuite) TestApplyListAndOrder() {
	low := s.finding("print_instead_of_logger", domain.FindingSeverityLow, true)
	high := s.finding("hardcoded_secret", domain.FindingSeverityHigh, true)
	repoWide := s.finding("committed_env_file", domain.FindingSeverityHigh, false)
	s.Require().NoError(s.store.ApplyFindings(s.ctx, s.repoID, []domain.ComponentFinding{low, high, repoWide}, nil))

	got, err := s.store.ListFindings(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(got, 3)
	s.Equal(repoWide.ID, got[0].ID, "repository-wide findings first")
	s.Nil(got[0].ComponentID)
	s.Equal(high.ID, got[1].ID, "then by severity")
	s.Equal(low.ID, got[2].ID)
	s.Equal([]domain.SourceEvidence{{Path: "api/main.go", Line: 7, Note: "fmt.Println(x)"}}, got[2].Evidence)
	s.Equal(s.scanID, *got[2].LastScanID)

	resolvedAt := time.Now().UTC().Truncate(time.Second)
	low.Status = domain.FindingResolved
	low.ResolvedAt = &resolvedAt
	s.Require().NoError(s.store.ApplyFindings(s.ctx, s.repoID, []domain.ComponentFinding{low}, []uuid.UUID{high.ID}))
	got, err = s.store.ListFindings(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Require().Len(got, 2)
	s.Equal(domain.FindingResolved, got[1].Status)
	s.True(resolvedAt.Equal(*got[1].ResolvedAt))
}

func (s *FindingStoreSuite) TestSaveStatusAndLinkTask() {
	f := s.finding("console_logs_in_production", domain.FindingSeverityMedium, true)
	f.Status = domain.FindingFalsePositive
	f.StatusReason = "a logger shim"
	saved, err := s.store.SaveFinding(s.ctx, f)
	s.Require().NoError(err)
	s.Equal(domain.FindingFalsePositive, saved.Status)
	s.Equal("a logger shim", saved.StatusReason)
	s.Nil(saved.Task)

	num, err := s.tasks.NextTaskNumber(s.ctx, "technical")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: "Remove console logs", TaskType: domain.TaskTypeTechnical,
		Column: domain.TaskColumnBacklog, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	s.Require().NoError(s.store.LinkFindingsToTask(s.ctx, []uuid.UUID{f.ID}, task.ID))

	got, err := s.store.GetFinding(s.ctx, f.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got.Task)
	s.Equal(task.ID, got.Task.ID)
	s.Equal(domain.FormatTaskKey(domain.TaskTypeTechnical, num), got.Task.Key)
	s.Equal(domain.TaskColumnBacklog, got.Task.Column)

	_, err = s.pool.Exec(s.ctx, `DELETE FROM board_tasks WHERE id = $1`, task.ID)
	s.Require().NoError(err)
	got, err = s.store.GetFinding(s.ctx, f.ID)
	s.Require().NoError(err)
	s.Nil(got.TaskID, "deleting the task unlinks the finding")
	s.Nil(got.Task)

	_, err = s.store.GetFinding(s.ctx, uuid.New())
	s.ErrorIs(err, port.ErrNotFound)
}

func (s *FindingStoreSuite) TestComponentDeleteCascades() {
	f := s.finding("empty_catch_block", domain.FindingSeverityMedium, true)
	s.Require().NoError(s.store.ApplyFindings(s.ctx, s.repoID, []domain.ComponentFinding{f}, nil))
	s.Require().NoError(s.model.ApplyReconcile(s.ctx, port.ModelReconcile{RepositoryID: s.repoID, DeleteComponents: []uuid.UUID{s.compID}}))
	got, err := s.store.ListFindings(s.ctx, s.repoID)
	s.Require().NoError(err)
	s.Empty(got)
}
