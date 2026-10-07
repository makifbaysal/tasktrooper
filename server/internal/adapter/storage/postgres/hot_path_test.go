package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// HotPathStoreSuite pins the store reads behind the endpoints the UI polls:
// that their single-query forms answer exactly what the per-row forms did,
// and that the board version moves on every write the board list can see and
// on nothing it cannot.
type HotPathStoreSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	pg        *database.Embedded
	pool      *pgxpool.Pool
	db        *postgres.DB
	tasks     *postgres.BoardTaskStore
	runs      *postgres.TaskAgentRunStore
	agents    *postgres.CatalogStore
	events    *postgres.BoardEventStore
	comments  *postgres.TaskCommentStore
	spans     *postgres.TaskColumnSpanStore
	pipelines *postgres.PipelineStore
	repoID    uuid.UUID
}

func TestHotPathStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(HotPathStoreSuite))
}

func (s *HotPathStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	s.pool, err = pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.db = postgres.NewDB(s.pool)
	s.tasks = postgres.NewBoardTaskStore(s.db)
	s.runs = postgres.NewTaskAgentRunStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.events = postgres.NewBoardEventStore(s.db)
	s.comments = postgres.NewTaskCommentStore(s.db)
	s.spans = postgres.NewTaskColumnSpanStore(s.db)
	s.pipelines = postgres.NewPipelineStore(s.db)

	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, "hot-path-test", "", "/tmp/hot-path-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *HotPathStoreSuite) TearDownSuite() {
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

func (s *HotPathStoreSuite) newAgent() domain.Agent {
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{
		Name:         "hot-path-" + uuid.NewString(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	return agent
}

func (s *HotPathStoreSuite) newTask(column domain.TaskColumn, assignee *uuid.UUID) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID:    s.repoID,
		TaskNumber:      num,
		Title:           "hot-path-test",
		TaskType:        "task",
		Column:          column,
		Priority:        domain.TaskPriorityMedium,
		CreatedBy:       "test",
		AssigneeAgentID: assignee,
	})
	s.Require().NoError(err)
	return task
}

func (s *HotPathStoreSuite) newRun(taskID, agentID uuid.UUID, status string) domain.TaskAgentRun {
	event, err := s.events.Create(s.ctx, domain.BoardEvent{
		RepositoryID: s.repoID, TaskID: taskID, EventType: domain.BoardEventTaskAssigned, Payload: json.RawMessage(`{}`),
	})
	s.Require().NoError(err)
	run, err := s.runs.Create(s.ctx, domain.TaskAgentRun{
		TaskID: taskID, AgentID: agentID, BoardEventID: event.ID, Status: domain.TaskAgentRunStatusPending,
	})
	s.Require().NoError(err)
	if status != domain.TaskAgentRunStatusPending {
		run.Status = status
		run, err = s.runs.Update(s.ctx, run)
		s.Require().NoError(err)
	}
	return run
}

func (s *HotPathStoreSuite) version() uint64 { return s.tasks.BoardVersion() }

func (s *HotPathStoreSuite) TestListAgentsCarriesSkillIDsInNameOrder() {
	withSkills := s.newAgent()
	without := s.newAgent()
	var created []domain.Skill
	for _, name := range []string{"zeta-" + uuid.NewString(), "alpha-" + uuid.NewString(), "mid-" + uuid.NewString()} {
		skill, err := s.agents.CreateSkill(s.ctx, domain.Skill{
			AgentID: withSkills.ID, Name: name, Content: "long skill body", Embedding: []float32{0.1, 0.2}, Enabled: true,
		})
		s.Require().NoError(err)
		created = append(created, skill)
	}

	agents, err := s.agents.ListAgents(s.ctx)
	s.Require().NoError(err)

	byID := map[uuid.UUID]domain.Agent{}
	for i, a := range agents {
		byID[a.ID] = a
		if i > 0 {
			s.LessOrEqual(agents[i-1].Name, a.Name)
		}
	}
	s.Equal([]uuid.UUID{created[1].ID, created[2].ID, created[0].ID}, byID[withSkills.ID].SkillIDs)
	s.NotNil(byID[without.ID].SkillIDs)
	s.Empty(byID[without.ID].SkillIDs)

	for _, id := range []uuid.UUID{withSkills.ID, without.ID} {
		single, err := s.agents.GetAgent(s.ctx, id)
		s.Require().NoError(err)
		s.Equal(single.SkillIDs, byID[id].SkillIDs)
		s.Equal(single.Name, byID[id].Name)
		s.Equal(single.ToolPolicy, byID[id].ToolPolicy)
	}
}

func (s *HotPathStoreSuite) TestCommentsNameTheirAgentAuthors() {
	agent := s.newAgent()
	task := s.newTask(domain.TaskColumnTodo, nil)

	byAgent, err := s.comments.Create(s.ctx, domain.TaskComment{TaskID: task.ID, AuthorType: "agent", AuthorID: agent.ID.String(), Content: "done"})
	s.Require().NoError(err)
	s.Equal(agent.Name, byAgent.AuthorName)
	_, err = s.comments.Create(s.ctx, domain.TaskComment{TaskID: task.ID, AuthorType: "user", AuthorID: agent.ID.String(), Content: "thanks"})
	s.Require().NoError(err)
	_, err = s.comments.Create(s.ctx, domain.TaskComment{TaskID: task.ID, AuthorType: "agent", AuthorID: uuid.NewString(), Content: "from a deleted agent"})
	s.Require().NoError(err)
	_, err = s.comments.Create(s.ctx, domain.TaskComment{TaskID: task.ID, AuthorType: "agent", AuthorID: "not-a-uuid", Content: "legacy"})
	s.Require().NoError(err)

	listed, err := s.comments.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)

	s.Require().Len(listed, 4)
	s.Equal([]string{agent.Name, "", "", ""}, []string{listed[0].AuthorName, listed[1].AuthorName, listed[2].AuthorName, listed[3].AuthorName})
	s.Equal([]string{"done", "thanks", "from a deleted agent", "legacy"}, []string{listed[0].Content, listed[1].Content, listed[2].Content, listed[3].Content})
}

func (s *HotPathStoreSuite) TestGetByIDReadsAnyRepositorysTask() {
	task := s.newTask(domain.TaskColumnTodo, nil)

	got, err := s.tasks.GetByID(s.ctx, task.ID)
	s.Require().NoError(err)
	scoped, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.Equal(scoped, got)

	_, err = s.tasks.GetByID(s.ctx, uuid.New())
	s.ErrorIs(err, domain.ErrBoardTaskNotFound)
}

func (s *HotPathStoreSuite) TestDispatchCandidatesFilterInSQLAndCarryTheNewestRuns() {
	agent := s.newAgent()
	assignee := agent.ID

	withHistory := s.newTask(domain.TaskColumnTodo, &assignee)
	var history []domain.TaskAgentRun
	for _, status := range []string{
		domain.TaskAgentRunStatusCompleted, domain.TaskAgentRunStatusFailed,
		domain.TaskAgentRunStatusFailed, domain.TaskAgentRunStatusFailed,
	} {
		history = append(history, s.newRun(withHistory.ID, assignee, status))
	}
	neverRun := s.newTask(domain.TaskColumnInProgress, &assignee)
	s.newTask(domain.TaskColumnBacklog, &assignee)
	s.newTask(domain.TaskColumnDone, &assignee)
	s.newTask(domain.TaskColumnTodo, nil)
	parked := s.newTask(domain.TaskColumnTodo, &assignee)
	s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, parked.ID, "waiting on T-1"))

	candidates, err := s.tasks.ListDispatchCandidates(s.ctx, 3)
	s.Require().NoError(err)

	got := map[uuid.UUID][]domain.TaskAgentRun{}
	for _, c := range candidates {
		s.NotNil(c.Task.AssigneeAgentID)
		got[c.Task.ID] = c.Runs
	}
	s.Contains(got, withHistory.ID)
	s.Contains(got, neverRun.ID)
	s.NotContains(got, parked.ID)
	for _, c := range candidates {
		s.NotContains([]domain.TaskColumn{domain.TaskColumnBacklog, domain.TaskColumnDone, domain.TaskColumnReleased, domain.TaskColumnBlocked}, c.Task.Column)
	}
	s.Empty(got[neverRun.ID])

	newestFirst, err := s.runs.ListByTask(s.ctx, withHistory.ID, 3)
	s.Require().NoError(err)
	s.Equal(newestFirst, got[withHistory.ID])
	s.Equal([]uuid.UUID{history[3].ID, history[2].ID, history[1].ID},
		[]uuid.UUID{got[withHistory.ID][0].ID, got[withHistory.ID][1].ID, got[withHistory.ID][2].ID})
}

func (s *HotPathStoreSuite) TestAgentRunningFollowsTheRunStatus() {
	agent := s.newAgent()
	task := s.newTask(domain.TaskColumnInProgress, &agent.ID)
	s.False(task.AgentRunning)

	run := s.newRun(task.ID, agent.ID, domain.TaskAgentRunStatusRunning)
	got, err := s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	s.True(got.AgentRunning)
	updated, err := s.tasks.Update(s.ctx, got)
	s.Require().NoError(err)
	s.True(updated.AgentRunning, "the RETURNING projection reports it too")
	s.True(s.boardTask(task.ID).AgentRunning)

	run.Status = domain.TaskAgentRunStatusCompleted
	_, err = s.runs.Update(s.ctx, run)
	s.Require().NoError(err)
	s.False(s.boardTask(task.ID).AgentRunning)
}

func (s *HotPathStoreSuite) boardTask(id uuid.UUID) domain.BoardTask {
	visible, err := s.tasks.ListBoardVisible(s.ctx, time.Now().Add(-domain.ReleasedBoardWindow))
	s.Require().NoError(err)
	for _, t := range visible {
		if t.ID == id {
			return t
		}
	}
	s.FailNow("task not on the board", id.String())
	return domain.BoardTask{}
}

func (s *HotPathStoreSuite) TestBoardReadsLeaveTheVersionAlone() {
	task := s.newTask(domain.TaskColumnTodo, nil)
	before := s.version()

	_, err := s.tasks.ListBoardVisible(s.ctx, time.Now().Add(-domain.ReleasedBoardWindow))
	s.Require().NoError(err)
	_, err = s.pipelines.LatestStatusByTasks(s.ctx, []uuid.UUID{task.ID})
	s.Require().NoError(err)
	_, err = s.tasks.Get(s.ctx, s.repoID, task.ID)
	s.Require().NoError(err)
	_, err = s.comments.ListByTask(s.ctx, task.ID)
	s.Require().NoError(err)
	_, err = s.events.Create(s.ctx, domain.BoardEvent{RepositoryID: s.repoID, TaskID: task.ID, EventType: domain.BoardEventTaskCommented, Payload: json.RawMessage(`{}`)})
	s.Require().NoError(err)

	s.Equal(before, s.version())
}

func (s *HotPathStoreSuite) TestEveryBoardWriteMovesTheVersion() {
	agent := s.newAgent()
	task := s.newTask(domain.TaskColumnTodo, nil)
	run := s.newRun(task.ID, agent.ID, domain.TaskAgentRunStatusRunning)

	writes := map[string]func(){
		"task update": func() {
			task.Title = "renamed"
			_, err := s.tasks.Update(s.ctx, task)
			s.Require().NoError(err)
		},
		"exec write": func() {
			s.Require().NoError(s.tasks.MarkWorkOrderWaiting(s.ctx, s.repoID, task.ID, "x"))
		},
		"span move": func() {
			s.Require().NoError(s.spans.RecordMove(s.ctx, s.repoID, task.ID, string(domain.TaskColumnInProgress), time.Now()))
		},
		"run heartbeat": func() {
			_, err := s.runs.Touch(s.ctx, run.ID)
			s.Require().NoError(err)
		},
		"pipeline": func() {
			_, err := s.pipelines.Create(s.ctx, domain.TaskPipeline{
				TaskID: task.ID, RepositoryID: s.repoID, Trigger: domain.PipelineTriggerManual, Status: domain.PipelineStatus("pending"),
			})
			s.Require().NoError(err)
		},
		"delete elsewhere cascades": func() {
			_, err := s.db.Exec(s.ctx, `DELETE FROM task_comments WHERE id = $1`, uuid.New())
			s.Require().NoError(err)
		},
	}
	for name, write := range writes {
		before := s.version()
		write()
		s.Greater(s.version(), before, name)
	}
}

func (s *HotPathStoreSuite) TestTransactionMovesTheVersionOnlyWhenItCommits() {
	task := s.newTask(domain.TaskColumnTodo, nil)

	before := s.version()
	err := s.db.InTx(s.ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(s.ctx, `UPDATE board_tasks SET title = 'in tx' WHERE id = $1`, task.ID)
		s.Equal(before, s.version(), "nothing is visible before commit")
		return err
	})
	s.Require().NoError(err)
	s.Greater(s.version(), before)

	before = s.version()
	tx, err := s.db.Begin(s.ctx)
	s.Require().NoError(err)
	_, err = tx.Exec(s.ctx, `UPDATE board_tasks SET title = 'rolled back' WHERE id = $1`, task.ID)
	s.Require().NoError(err)
	s.Require().NoError(tx.Rollback(s.ctx))
	s.Equal(before, s.version())

	before = s.version()
	err = s.db.InTx(s.ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(s.ctx, `SELECT title FROM board_tasks WHERE id = $1`, task.ID)
		return err
	})
	s.Require().NoError(err)
	s.Equal(before, s.version(), "a read-only transaction changes nothing")
}

func (s *HotPathStoreSuite) TestMigration174ReplacesTheHotPathIndexes() {
	rows, err := s.pool.Query(s.ctx, `SELECT indexname FROM pg_indexes WHERE schemaname = 'public'`)
	s.Require().NoError(err)
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	s.Require().NoError(err)

	s.Subset(names, []string{
		"idx_task_agent_runs_created",
		"idx_session_steps_run_created",
		"idx_session_messages_session_created",
		"idx_task_agent_runs_running",
	})
	s.NotContains(names, "idx_session_steps_run_id")
	s.NotContains(names, "idx_session_messages_session_id")
}
