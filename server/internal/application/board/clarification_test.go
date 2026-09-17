package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSessionStore struct {
	existing map[uuid.UUID]domain.Session
	created  []domain.Session
	byTask   map[uuid.UUID]uuid.UUID
	appended []domain.SessionMessage
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{existing: map[uuid.UUID]domain.Session{}, byTask: map[uuid.UUID]uuid.UUID{}}
}

func (f *fakeSessionStore) Create(_ context.Context, title, model, workspaceDir string, projectID, agentID *uuid.UUID, _ *time.Time) (domain.Session, error) {
	sess := domain.Session{ID: uuid.New(), Title: title, Model: model, WorkspaceDir: workspaceDir, ProjectID: projectID, AgentID: agentID}
	f.existing[sess.ID] = sess
	f.created = append(f.created, sess)
	return sess, nil
}

func (f *fakeSessionStore) Get(_ context.Context, id uuid.UUID) (domain.Session, error) {
	sess, ok := f.existing[id]
	if !ok {
		return domain.Session{}, errors.New("session not found")
	}
	return sess, nil
}

func (f *fakeSessionStore) Delete(context.Context, uuid.UUID) error { return nil }
func (f *fakeSessionStore) List(context.Context, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ListByProject(context.Context, uuid.UUID, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ListByAgent(context.Context, uuid.UUID, int, int) ([]domain.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) ListMessages(context.Context, uuid.UUID) ([]domain.SessionMessage, error) {
	return nil, nil
}
func (f *fakeSessionStore) AppendMessage(_ context.Context, sessionID uuid.UUID, role domain.Role, content string, _ []byte, _ []byte) (domain.SessionMessage, error) {
	msg := domain.SessionMessage{ID: uuid.New(), SessionID: sessionID, Role: role, Content: content}
	f.appended = append(f.appended, msg)
	return msg, nil
}
func (f *fakeSessionStore) UpdateWorkspaceDir(context.Context, uuid.UUID, string) error { return nil }
func (f *fakeSessionStore) UpdateProjectRoot(context.Context, uuid.UUID, string) error  { return nil }
func (f *fakeSessionStore) UpdateCLISessionID(context.Context, uuid.UUID, string) error { return nil }
func (f *fakeSessionStore) ParkPendingTurn(context.Context, uuid.UUID, domain.SessionMessageRequest, domain.ToolPolicy, time.Time) error {
	return nil
}
func (f *fakeSessionStore) TakePendingSessionTurn(context.Context, time.Time) (domain.PendingSessionTurn, bool, error) {
	return domain.PendingSessionTurn{}, false, nil
}

func (f *fakeSessionStore) BindTask(_ context.Context, id, taskID uuid.UUID) error {
	sess, ok := f.existing[id]
	if !ok {
		return errors.New("session not found")
	}
	sess.TaskID = &taskID
	f.existing[id] = sess
	if f.byTask == nil {
		f.byTask = map[uuid.UUID]uuid.UUID{}
	}
	f.byTask[taskID] = id
	return nil
}

func (f *fakeSessionStore) FindByTask(_ context.Context, taskID uuid.UUID) (domain.Session, bool, error) {
	id, ok := f.byTask[taskID]
	if !ok {
		return domain.Session{}, false, nil
	}
	sess, ok := f.existing[id]
	return sess, ok, nil
}

func clarificationJob(sessionID *uuid.UUID) RunJob {
	return RunJob{
		Run:  domain.TaskAgentRun{AgentID: uuid.New()},
		Task: domain.BoardTask{ID: uuid.New(), Title: "Add the store link", ClarificationSessionID: sessionID},
	}
}

func TestClarificationSessionReusesTheTaskThread(t *testing.T) {
	sessions := newFakeSessionStore()
	existing, err := sessions.Create(context.Background(), "Question: Add the store link", "gpt", "/repo", nil, nil, nil)
	require.NoError(t, err)
	sessions.created = nil

	runner := &Runner{sessions: sessions}
	got, ok := runner.clarificationSession(context.Background(), clarificationJob(&existing.ID), domain.Agent{}, "/repo")

	assert.True(t, ok)
	assert.Equal(t, existing.ID, got)
	assert.Empty(t, sessions.created, "the recorded thread is reused, not replaced")
}

func TestClarificationSessionOpensOneWhenTaskHasNone(t *testing.T) {
	sessions := newFakeSessionStore()
	runner := &Runner{sessions: sessions}

	got, ok := runner.clarificationSession(context.Background(), clarificationJob(nil), domain.Agent{}, "/repo")

	assert.True(t, ok)
	require.Len(t, sessions.created, 1)
	assert.Equal(t, sessions.created[0].ID, got)
}

func TestClarificationSessionFallsBackWhenThreadIsGone(t *testing.T) {
	sessions := newFakeSessionStore()
	runner := &Runner{sessions: sessions}
	gone := uuid.New()

	got, ok := runner.clarificationSession(context.Background(), clarificationJob(&gone), domain.Agent{}, "/repo")

	assert.True(t, ok)
	require.Len(t, sessions.created, 1)
	assert.Equal(t, sessions.created[0].ID, got)
	assert.NotEqual(t, gone, got)
}

func TestRevisionCommentsMessageSkipsClarifications(t *testing.T) {
	comments := []domain.TaskComment{
		{AuthorType: "agent", Content: "Reviewer: the footer link is missing an aria-label."},
		{AuthorType: "system", Content: prompt.ClarificationAnswerComment("Where should the link go?", "Footer")},
	}

	msg := revisionCommentsMessage(comments)

	assert.Contains(t, msg, "aria-label")
	assert.NotContains(t, msg, "Where should the link go?")
}

func TestRevisionCommentsMessageEmptyWithoutFeedback(t *testing.T) {
	only := []domain.TaskComment{{Content: prompt.ClarificationAnswerComment("q", "a")}}

	assert.Empty(t, revisionCommentsMessage(only))
	assert.Empty(t, revisionCommentsMessage(nil))
}
