package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

// stubSessionStore serves a fixed transcript; only ListMessages is exercised.
type stubSessionStore struct {
	messages []domain.SessionMessage
}

func (s *stubSessionStore) Create(context.Context, string, string, string, *uuid.UUID, *uuid.UUID, *time.Time) (domain.Session, error) {
	return domain.Session{}, nil
}
func (s *stubSessionStore) Get(context.Context, uuid.UUID) (domain.Session, error) {
	return domain.Session{}, nil
}
func (s *stubSessionStore) Delete(context.Context, uuid.UUID) error { return nil }
func (s *stubSessionStore) List(context.Context, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) ListByProject(context.Context, uuid.UUID, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) ListByAgent(context.Context, uuid.UUID, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) ListMessages(context.Context, uuid.UUID) ([]domain.SessionMessage, error) {
	return s.messages, nil
}
func (s *stubSessionStore) AppendMessage(context.Context, uuid.UUID, domain.Role, string, []byte, []byte) (domain.SessionMessage, error) {
	return domain.SessionMessage{}, nil
}
func (s *stubSessionStore) UpdateWorkspaceDir(context.Context, uuid.UUID, string) error { return nil }
func (s *stubSessionStore) UpdateProjectRoot(context.Context, uuid.UUID, string) error  { return nil }
func (s *stubSessionStore) UpdateCLISessionID(context.Context, uuid.UUID, string) error { return nil }
func (s *stubSessionStore) BindTask(context.Context, uuid.UUID, uuid.UUID) error        { return nil }
func (s *stubSessionStore) FindByTask(context.Context, uuid.UUID) (domain.Session, bool, error) {
	return domain.Session{}, false, nil
}
func (s *stubSessionStore) ParkPendingTurn(context.Context, uuid.UUID, domain.SessionMessageRequest, domain.ToolPolicy, time.Time) error {
	return nil
}
func (s *stubSessionStore) TakePendingSessionTurn(context.Context, time.Time) (domain.PendingSessionTurn, bool, error) {
	return domain.PendingSessionTurn{}, false, nil
}

type stubActionStore struct {
	actions []domain.SessionAction
	err     error
}

func (s *stubActionStore) AppendAction(context.Context, domain.SessionAction) (domain.SessionAction, error) {
	return domain.SessionAction{}, nil
}
func (s *stubActionStore) ListActions(context.Context, uuid.UUID) ([]domain.SessionAction, error) {
	return s.actions, s.err
}

type HistorySuite struct {
	suite.Suite
}

func (s *HistorySuite) TestActionLedgerLandsAheadOfTheNewestUserTurn() {
	taskID := uuid.New()
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{Role: domain.RoleUser, Content: "open a task for the ops console"},
		{Role: domain.RoleAssistant, Content: "Task opened."},
		{Role: domain.RoleUser, Content: "move that task to the sprint"},
	}}
	actions := &stubActionStore{actions: []domain.SessionAction{{
		ToolName: "create_board_task", Verb: domain.ActionVerbCreated,
		EntityKind: domain.ActionEntityBoardTask, EntityID: &taskID,
		EntityKey: "TT-42", Title: "Ops console", Column: "backlog",
	}}}

	history, err := session.BuildMessageHistoryForTest(context.Background(), store, actions, uuid.New())
	s.Require().NoError(err)
	s.Require().Len(history, 4)

	// Directly ahead of the newest user turn, where the model will act on it.
	s.Equal(domain.RoleSystem, history[2].Role)
	s.Contains(history[2].Content, taskID.String())
	s.Contains(history[2].Content, "TT-42")
	s.Equal("move that task to the sprint", history[3].Content)
}

func (s *HistorySuite) TestHistoryUnchangedWithoutActions() {
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{Role: domain.RoleUser, Content: "hello"},
	}}

	history, err := session.BuildMessageHistoryForTest(context.Background(), store, &stubActionStore{}, uuid.New())
	s.Require().NoError(err)
	s.Require().Len(history, 1)
	s.Equal(domain.RoleUser, history[0].Role)
}

func (s *HistorySuite) TestLedgerReadFailureDoesNotBreakTheConversation() {
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{Role: domain.RoleUser, Content: "hello"},
	}}
	actions := &stubActionStore{err: context.DeadlineExceeded}

	history, err := session.BuildMessageHistoryForTest(context.Background(), store, actions, uuid.New())
	s.Require().NoError(err)
	s.Require().Len(history, 1)
}

func (s *HistorySuite) TestAnsweredQuestionsStayVisibleToTheModel() {
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{
			Role:    domain.RoleAssistant,
			Content: "I need a detail first.",
			Clarification: &domain.ClarificationRequest{
				Context: "I need a detail first.",
				Questions: []domain.ClarificationQuestion{{
					ID: "q1", Prompt: "Which column?",
					Options: []domain.ClarificationOption{
						{ID: "backlog", Label: "Backlog"},
						{ID: "sprint", Label: "Sprint"},
					},
				}},
			},
		},
		{Role: domain.RoleUser, Content: "Which column?: Sprint"},
	}}

	history, err := session.BuildMessageHistoryForTest(context.Background(), store, nil, uuid.New())
	s.Require().NoError(err)
	s.Require().Len(history, 2)
	// Without this the model saw the answer but never the question it answered.
	s.Contains(history[0].Content, "Which column?")
	s.Contains(history[0].Content, "Backlog")
	s.Contains(history[0].Content, "Sprint")
}

func TestHistorySuite(t *testing.T) {
	suite.Run(t, new(HistorySuite))
}
