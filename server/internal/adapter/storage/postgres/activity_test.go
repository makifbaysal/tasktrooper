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
)

type ActivityStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.ActivityStore
	repoID uuid.UUID
}

func TestActivityStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ActivityStoreSuite))
}

func (s *ActivityStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	startupCtx, cancelStartup := context.WithTimeout(s.ctx, 3*time.Minute)
	defer cancelStartup()

	pg, err := newTestDatabase(startupCtx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewActivityStore(s.db)

	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, "activity-test", "", "/tmp/activity-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *ActivityStoreSuite) TearDownSuite() {
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

func (s *ActivityStoreSuite) SetupTest() {
	_, err := s.pool.Exec(s.ctx, `DELETE FROM session_runs`)
	s.Require().NoError(err)
}

func (s *ActivityStoreSuite) newRun() domain.SessionRun {
	sess, err := postgres.NewSessionStore(s.db).Create(s.ctx, "activity chat", "sonnet", "/tmp/activity-chat", nil, nil, nil)
	s.Require().NoError(err)
	run, err := s.store.CreateRun(s.ctx, &sess.ID, "req-"+uuid.NewString(), "sonnet")
	s.Require().NoError(err)
	return run
}

func (s *ActivityStoreSuite) startedAgo(runID uuid.UUID, age time.Duration) {
	_, err := s.pool.Exec(s.ctx, `UPDATE session_runs SET started_at = now() - $2::interval WHERE id = $1`, runID, age.String())
	s.Require().NoError(err)
}

func (s *ActivityStoreSuite) stepAgo(runID uuid.UUID, age time.Duration) {
	s.Require().NoError(s.store.AppendStep(s.ctx, runID, "llm_call", []byte(`{}`)))
	_, err := s.pool.Exec(s.ctx, `UPDATE session_steps SET created_at = now() - $2::interval WHERE run_id = $1`, runID, age.String())
	s.Require().NoError(err)
}

func (s *ActivityStoreSuite) boardRunHeartbeatAgo(sessionRunID uuid.UUID, age time.Duration) {
	tasks := postgres.NewBoardTaskStore(s.db)
	num, err := tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "activity-test",
		TaskType:     "task",
		Column:       domain.TaskColumn("backlog"),
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	agent, err := postgres.NewCatalogStore(s.db).CreateAgent(s.ctx, domain.Agent{
		Name:         "activity-test-" + uuid.NewString(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	event, err := postgres.NewBoardEventStore(s.db).Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID,
		TaskID:       task.ID,
		EventType:    domain.BoardEventTaskAssigned,
		Payload:      json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	run, err := postgres.NewTaskAgentRunStore(s.db).Create(s.ctx, domain.TaskAgentRun{
		TaskID:       task.ID,
		AgentID:      agent.ID,
		BoardEventID: event.ID,
		SessionRunID: &sessionRunID,
		Status:       domain.TaskAgentRunStatusRunning,
	})
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `UPDATE task_agent_runs SET updated_at = now() - $2::interval WHERE id = $1`, run.ID, age.String())
	s.Require().NoError(err)
}

func (s *ActivityStoreSuite) runStatus(runID uuid.UUID) (string, *time.Time) {
	var status string
	var completedAt *time.Time
	s.Require().NoError(s.pool.QueryRow(s.ctx,
		`SELECT status, completed_at FROM session_runs WHERE id = $1`, runID).Scan(&status, &completedAt))
	return status, completedAt
}

func (s *ActivityStoreSuite) TestFailInterruptedRunsSettlesOnlyRunsStillRunning() {
	interrupted := s.newRun()
	completed := s.newRun()
	s.Require().NoError(s.store.CompleteRun(s.ctx, completed.ID, domain.SessionRunStatusCompleted))
	cancelled := s.newRun()
	_, err := s.store.CancelRun(s.ctx, cancelled.ID)
	s.Require().NoError(err)

	n, err := s.store.FailInterruptedRuns(s.ctx)

	s.Require().NoError(err)
	s.Equal(1, n)
	status, completedAt := s.runStatus(interrupted.ID)
	s.Equal(domain.SessionRunStatusFailed, status)
	s.NotNil(completedAt)
	status, _ = s.runStatus(completed.ID)
	s.Equal(domain.SessionRunStatusCompleted, status)
	status, _ = s.runStatus(cancelled.ID)
	s.Equal(domain.SessionRunStatusCancelled, status)

	active, err := s.store.ListActiveRuns(s.ctx)
	s.Require().NoError(err)
	s.Empty(active)
}

func (s *ActivityStoreSuite) TestListActiveRunsLeavesOutLongRunsWithNoSignOfLife() {
	tests := []struct {
		name   string
		setup  func(runID uuid.UUID)
		listed bool
	}{
		{"just started", func(uuid.UUID) {}, true},
		{"started 5h ago with no step yet", func(id uuid.UUID) { s.startedAgo(id, 5*time.Hour) }, true},
		{"started 7h ago, step a minute ago", func(id uuid.UUID) {
			s.startedAgo(id, 7*time.Hour)
			s.stepAgo(id, time.Minute)
		}, true},
		{"started 7h ago, board heartbeat a minute ago", func(id uuid.UUID) {
			s.startedAgo(id, 7*time.Hour)
			s.boardRunHeartbeatAgo(id, time.Minute)
		}, true},
		{"started 7h ago, last step 31m ago", func(id uuid.UUID) {
			s.startedAgo(id, 7*time.Hour)
			s.stepAgo(id, 31*time.Minute)
		}, false},
		{"started 7h ago, board heartbeat 31m ago", func(id uuid.UUID) {
			s.startedAgo(id, 7*time.Hour)
			s.boardRunHeartbeatAgo(id, 31*time.Minute)
		}, false},
		{"started 7h ago, never stepped", func(id uuid.UUID) { s.startedAgo(id, 7*time.Hour) }, false},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			run := s.newRun()
			tt.setup(run.ID)

			active, err := s.store.ListActiveRuns(s.ctx)

			s.Require().NoError(err)
			ids := make([]uuid.UUID, 0, len(active))
			for _, r := range active {
				ids = append(ids, r.ID)
			}
			if tt.listed {
				s.Equal([]uuid.UUID{run.ID}, ids)
			} else {
				s.Empty(ids)
			}
		})
	}
}
