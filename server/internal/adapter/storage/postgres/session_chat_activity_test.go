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

// SessionChatActivitySuite covers ListRecentChatMessages, the feed
// workspace.Service.ListActivity folds into "chat_message" activity items —
// it needs a real Postgres because the assertion is about the JOIN filtering
// (role = assistant, agent_id IS NOT NULL) a fake store could get right by
// accident.
type SessionChatActivitySuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.SessionStore
	agents *postgres.CatalogStore
	tasks  *postgres.BoardTaskStore
	repoID uuid.UUID
}

func TestSessionChatActivitySuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(SessionChatActivitySuite))
}

func (s *SessionChatActivitySuite) SetupSuite() {
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
	s.store = postgres.NewSessionStore(s.db)
	s.agents = postgres.NewCatalogStore(s.db)
	s.tasks = postgres.NewBoardTaskStore(s.db)

	repo, err := postgres.NewRepositoryStore(s.db).Create(s.ctx, "chat-activity-test", "", "/tmp/chat-activity-test-"+uuid.New().String(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *SessionChatActivitySuite) TearDownSuite() {
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

func (s *SessionChatActivitySuite) SetupTest() {
	_, err := s.pool.Exec(s.ctx, `DELETE FROM session_messages`)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `DELETE FROM sessions`)
	s.Require().NoError(err)
}

func (s *SessionChatActivitySuite) newAgent() uuid.UUID {
	agent, err := s.agents.CreateAgent(s.ctx, domain.Agent{
		Name:         "chat-activity-" + uuid.NewString(),
		ProviderType: domain.LLMProviderAnthropic,
		Model:        "test-model",
	})
	s.Require().NoError(err)
	return agent.ID
}

func (s *SessionChatActivitySuite) newTask() uuid.UUID {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID,
		TaskNumber:   num,
		Title:        "chat-activity-test",
		TaskType:     "task",
		Column:       domain.TaskColumn("backlog"),
		Priority:     domain.TaskPriorityMedium,
		CreatedBy:    "test",
	})
	s.Require().NoError(err)
	return task.ID
}

func (s *SessionChatActivitySuite) TestOnlyAssistantMessagesFromAgentOwnedSessionsAreReturned() {
	agentID := s.newAgent()
	taskID := s.newTask()

	agentSession, err := s.store.Create(s.ctx, "agent chat", "sonnet", "/tmp/agent-chat", nil, &agentID, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.store.BindTask(s.ctx, agentSession.ID, taskID))

	ungroupedSession, err := s.store.Create(s.ctx, "ungrouped chat", "sonnet", "/tmp/ungrouped-chat", nil, nil, nil)
	s.Require().NoError(err)

	_, err = s.store.AppendMessage(s.ctx, agentSession.ID, domain.RoleUser, "ping the agent", nil, nil)
	s.Require().NoError(err)
	assistantMsg, err := s.store.AppendMessage(s.ctx, agentSession.ID, domain.RoleAssistant, "pong from the agent", nil, nil)
	s.Require().NoError(err)
	_, err = s.store.AppendMessage(s.ctx, agentSession.ID, domain.RoleTool, "tool output", nil, nil)
	s.Require().NoError(err)
	_, err = s.store.AppendMessage(s.ctx, ungroupedSession.ID, domain.RoleAssistant, "pong from an ungrouped chat", nil, nil)
	s.Require().NoError(err)

	entries, err := s.store.ListRecentChatMessages(s.ctx, 50)

	s.Require().NoError(err)
	s.Require().Len(entries, 1)
	s.Equal(assistantMsg.ID, entries[0].ID)
	s.Equal(agentSession.ID, entries[0].SessionID)
	s.Equal(agentID, entries[0].AgentID)
	s.Require().NotNil(entries[0].TaskID)
	s.Equal(taskID, *entries[0].TaskID)
	s.Equal("pong from the agent", entries[0].Content)
}

func (s *SessionChatActivitySuite) TestOrdersNewestFirstAndRespectsLimit() {
	agentID := s.newAgent()
	sess, err := s.store.Create(s.ctx, "agent chat", "sonnet", "/tmp/agent-chat-order", nil, &agentID, nil)
	s.Require().NoError(err)

	first, err := s.store.AppendMessage(s.ctx, sess.ID, domain.RoleAssistant, "first", nil, nil)
	s.Require().NoError(err)
	second, err := s.store.AppendMessage(s.ctx, sess.ID, domain.RoleAssistant, "second", nil, nil)
	s.Require().NoError(err)
	_, err = s.pool.Exec(s.ctx, `UPDATE session_messages SET created_at = created_at - interval '1 minute' WHERE id = $1`, first.ID)
	s.Require().NoError(err)

	entries, err := s.store.ListRecentChatMessages(s.ctx, 1)

	s.Require().NoError(err)
	s.Require().Len(entries, 1)
	s.Equal(second.ID, entries[0].ID)
}
