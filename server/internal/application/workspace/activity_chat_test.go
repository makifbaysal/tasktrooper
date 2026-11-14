package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeBoardEventStore and fakeTaskAgentRunStore embed their real interfaces
// (nil) and override only ListRecent, the idiom this codebase uses for a
// single-method fake (see application/board/stage_skip_test.go).
type fakeBoardEventStore struct {
	port.BoardEventStore
	events []domain.BoardEvent
	err    error
}

func (f *fakeBoardEventStore) ListRecent(ctx context.Context, limit int) ([]domain.BoardEvent, error) {
	return f.events, f.err
}

type fakeTaskAgentRunStore struct {
	port.TaskAgentRunStore
	runs []domain.TaskAgentRun
	err  error
}

func (f *fakeTaskAgentRunStore) ListRecent(ctx context.Context, limit int) ([]domain.TaskAgentRun, error) {
	return f.runs, f.err
}

type fakeChatActivitySource struct {
	entries []domain.ChatActivityEntry
	err     error
}

func (f *fakeChatActivitySource) ListRecentChatMessages(ctx context.Context, limit int) ([]domain.ChatActivityEntry, error) {
	return f.entries, f.err
}

type ActivityChatSuite struct {
	suite.Suite
	base time.Time
}

func TestActivityChatSuite(t *testing.T) {
	suite.Run(t, new(ActivityChatSuite))
}

func (s *ActivityChatSuite) SetupTest() {
	s.base = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
}

func (s *ActivityChatSuite) TestMergesAllThreeSourcesNewestFirstAndRespectsLimit() {
	taskID := uuid.New()
	agentID := uuid.New()
	sessionID := uuid.New()

	svc := NewService(nil)
	events := &fakeBoardEventStore{events: []domain.BoardEvent{
		{ID: uuid.New(), RepositoryID: uuid.New(), TaskID: taskID, EventType: domain.BoardEventTaskAssigned, CreatedAt: s.base.Add(10 * time.Second)},
	}}
	runs := &fakeTaskAgentRunStore{runs: []domain.TaskAgentRun{
		{ID: uuid.New(), TaskID: taskID, AgentID: agentID, Status: "completed", CreatedAt: s.base.Add(20 * time.Second)},
	}}
	chats := &fakeChatActivitySource{entries: []domain.ChatActivityEntry{
		{ID: uuid.New(), SessionID: sessionID, AgentID: agentID, TaskID: &taskID, Content: "hello from the agent", CreatedAt: s.base.Add(30 * time.Second)},
		{ID: uuid.New(), SessionID: sessionID, AgentID: agentID, Content: "an older message", CreatedAt: s.base},
	}}

	items, err := svc.ListActivity(context.Background(), events, runs, chats, 3)

	s.Require().NoError(err)
	s.Require().Len(items, 3)
	s.Equal("chat_message", items[0].Kind)
	s.Equal(sessionID, *items[0].SessionID)
	s.Equal(agentID, *items[0].AgentID)
	s.Equal(taskID, items[0].TaskID)
	s.Equal("hello from the agent", items[0].Summary)
	s.Equal("agent_run", items[1].Kind)
	s.Equal("board_event", items[2].Kind)
}

func (s *ActivityChatSuite) TestChatMessageWithoutTaskLeavesTaskIDZero() {
	svc := NewService(nil)
	chats := &fakeChatActivitySource{entries: []domain.ChatActivityEntry{
		{ID: uuid.New(), SessionID: uuid.New(), AgentID: uuid.New(), CreatedAt: s.base},
	}}

	items, err := svc.ListActivity(context.Background(), &fakeBoardEventStore{}, &fakeTaskAgentRunStore{}, chats, 10)

	s.Require().NoError(err)
	s.Require().Len(items, 1)
	s.Equal(uuid.Nil, items[0].TaskID)
}
