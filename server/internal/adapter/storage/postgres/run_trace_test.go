package postgres_test

import (
	"context"
	"encoding/json"
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

type RunTraceStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.RunTraceStore
	runs   *postgres.TaskAgentRunStore
	tasks  *postgres.BoardTaskStore
	agents *postgres.CatalogStore
	events *postgres.BoardEventStore
	repoID uuid.UUID
}

func TestRunTraceStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(RunTraceStoreSuite))
}

func (s *RunTraceStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.db = postgres.NewDB(s.pool)
	s.store = postgres.NewRunTraceStore(s.db)
	s.runs = postgres.NewTaskAgentRunStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.events = postgres.NewBoardEventStore(s.db)
	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, "run-trace-test", "", "/tmp/run-trace-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *RunTraceStoreSuite) TearDownSuite() {
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

func (s *RunTraceStoreSuite) newRun() (domain.BoardTask, domain.TaskAgentRun) {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{RepositoryID: s.repoID, TaskNumber: num, Title: "trace", TaskType: "task",
		Column: domain.TaskColumn("backlog"), Priority: domain.TaskPriorityMedium, CreatedBy: "test"})
	s.Require().NoError(err)
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{Name: "trace-" + uuid.NewString(), ProviderType: domain.LLMProviderAnthropic, Model: "m"})
	s.Require().NoError(err)
	ev, err := s.events.Create(s.ctx, domain.BoardEvent{RepositoryID: s.repoID, TaskID: task.ID, EventType: domain.BoardEventTaskAssigned, Payload: json.RawMessage(`{}`)})
	s.Require().NoError(err)
	run, err := s.runs.Create(s.ctx, domain.TaskAgentRun{TaskID: task.ID, AgentID: agent.ID, BoardEventID: ev.ID, Status: domain.TaskAgentRunStatusPending})
	s.Require().NoError(err)
	return task, run
}

func (s *RunTraceStoreSuite) event(run domain.TaskAgentRun, seq int, category, status, detail string) domain.RunTraceEvent {
	return domain.RunTraceEvent{RunID: run.ID, TaskID: run.TaskID, AgentID: run.AgentID, Seq: seq, At: time.Now(),
		Tool: "t", Source: domain.TraceSourceLoop, Category: category, Status: status, Detail: json.RawMessage(detail)}
}

func (s *RunTraceStoreSuite) TestAppendListCountsAndEffects() {
	task, run := s.newRun()
	s.Require().NoError(s.store.Begin(s.ctx, domain.RunTrace{RunID: run.ID, TaskID: task.ID, AgentID: run.AgentID, BaseSHA: "abc", StartedAt: time.Now()}))
	s.Require().NoError(s.store.Begin(s.ctx, domain.RunTrace{RunID: run.ID, TaskID: task.ID, AgentID: run.AgentID, BaseSHA: "other", StartedAt: time.Now()}))

	n, err := s.store.NextSeq(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Equal(1, n)

	s.Require().NoError(s.store.Append(s.ctx, []domain.RunTraceEvent{
		s.event(run, 1, domain.TraceCategoryRead, "ok", `{"path":"a.go"}`),
		s.event(run, 2, domain.TraceCategoryChange, "ok", `{"path":"a.go","changed":true}`),
		s.event(run, 3, domain.TraceCategoryExec, "error", `{"command":"go test"}`),
		s.event(run, 4, domain.TraceCategoryExternal, "ok", `{"effect":"http_request"}`),
	}))
	s.Require().NoError(s.store.Append(s.ctx, []domain.RunTraceEvent{s.event(run, 2, domain.TraceCategoryRead, "ok", `{}`)}))

	header, err := s.store.Header(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Equal("abc", header.BaseSHA)
	s.Equal(4, header.Events)

	counts, err := s.store.Counts(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Equal(domain.RunTraceCounts{Read: 1, Change: 1, Exec: 1, External: 1, Failed: 1, Total: 4, ReadFiles: 1, ChangedFiles: 1}, counts)

	all, err := s.store.List(s.ctx, run.ID, port.RunTraceFilter{})
	s.Require().NoError(err)
	s.Len(all, 4)
	paged, err := s.store.List(s.ctx, run.ID, port.RunTraceFilter{After: 2, Limit: 1})
	s.Require().NoError(err)
	s.Require().Len(paged, 1)
	s.Equal(3, paged[0].Seq)
	execOnly, err := s.store.List(s.ctx, run.ID, port.RunTraceFilter{Category: domain.TraceCategoryExec})
	s.Require().NoError(err)
	s.Len(execOnly, 1)

	effects, err := s.store.Effects(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Require().Len(effects, 1)
	s.Equal(4, effects[0].Seq)

	changes, err := s.store.Changes(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Len(changes, 1)

	v := &domain.RunTraceVerification{Available: true, Unrecorded: []string{"x.txt"}}
	s.Require().NoError(s.store.Finish(s.ctx, run.ID, "head", 2, v))
	header, err = s.store.Header(s.ctx, run.ID)
	s.Require().NoError(err)
	s.Equal("head", header.HeadSHA)
	s.Equal(2, header.Dropped)
	s.Require().NotNil(header.Verification)
	s.Equal([]string{"x.txt"}, header.Verification.Unrecorded)
	s.NotNil(header.FinishedAt)
}

func (s *RunTraceStoreSuite) TestEventsGoWithTheTaskAndAgeOut() {
	task, run := s.newRun()
	old := time.Now().Add(-100 * 24 * time.Hour)
	s.Require().NoError(s.store.Begin(s.ctx, domain.RunTrace{RunID: run.ID, TaskID: task.ID, AgentID: run.AgentID, StartedAt: old}))
	s.Require().NoError(s.store.Append(s.ctx, []domain.RunTraceEvent{s.event(run, 1, domain.TraceCategoryRead, "ok", `{}`)}))

	n, err := s.store.DeleteBefore(s.ctx, time.Now().Add(-90*24*time.Hour), 100)
	s.Require().NoError(err)
	s.GreaterOrEqual(n, 1)
	_, err = s.store.Header(s.ctx, run.ID)
	s.ErrorIs(err, domain.ErrRunTraceNotFound)
	rest, err := s.store.List(s.ctx, run.ID, port.RunTraceFilter{})
	s.Require().NoError(err)
	s.Empty(rest)

	task2, run2 := s.newRun()
	s.Require().NoError(s.store.Begin(s.ctx, domain.RunTrace{RunID: run2.ID, TaskID: task2.ID, AgentID: run2.AgentID, StartedAt: time.Now()}))
	s.Require().NoError(s.store.Append(s.ctx, []domain.RunTraceEvent{s.event(run2, 1, domain.TraceCategoryRead, "ok", `{}`)}))
	s.Require().NoError(s.tasks.Delete(s.ctx, s.repoID, task2.ID))
	_, err = s.store.Header(s.ctx, run2.ID)
	s.ErrorIs(err, domain.ErrRunTraceNotFound)
}
