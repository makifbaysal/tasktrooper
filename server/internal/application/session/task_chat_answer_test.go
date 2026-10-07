package session_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type recordedAnswer struct {
	repositoryID, taskID uuid.UUID
	question, answer     string
}

type recordingResumer struct {
	answers []recordedAnswer
}

func (r *recordingResumer) ResumeOnAnswer(context.Context, uuid.UUID, string) bool { return false }

func (r *recordingResumer) RecordTaskAnswer(_ context.Context, repositoryID, taskID uuid.UUID, question, answer string) {
	r.answers = append(r.answers, recordedAnswer{repositoryID, taskID, question, answer})
}

var hostingQuestion = &domain.ClarificationRequest{
	Context: "Deploy target",
	Questions: []domain.ClarificationQuestion{{
		ID: "host", Prompt: "Where should it be hosted?",
		Options: []domain.ClarificationOption{{ID: "free_text", Label: "Type"}, {ID: "skip", Label: "Skip"}},
	}},
}

func taskBoundSession() domain.Session {
	repositoryID, taskID := uuid.New(), uuid.New()
	return domain.Session{ID: uuid.New(), ProjectID: &repositoryID, TaskID: &taskID}
}

func TestTaskChatAnswerToAQuestionIsRecordedOnTheTask(t *testing.T) {
	sess := taskBoundSession()
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{Role: domain.RoleAssistant, Content: "One question first.", Clarification: hostingQuestion},
		{Role: domain.RoleUser, Content: "Where should it be hosted?: Fly.io"},
	}}
	resumer := &recordingResumer{}

	session.RecordTaskChatAnswerForTest(context.Background(), store, resumer, sess, domain.SessionMessageRequest{
		Role: domain.RoleUser, Content: "Where should it be hosted?: Fly.io",
	})

	require.Len(t, resumer.answers, 1)
	got := resumer.answers[0]
	assert.Equal(t, *sess.ProjectID, got.repositoryID)
	assert.Equal(t, *sess.TaskID, got.taskID)
	assert.Contains(t, got.question, "Where should it be hosted?")
	assert.Equal(t, "Where should it be hosted?: Fly.io", got.answer)
}

func TestTaskChatRecordsNothingWithoutAQuestion(t *testing.T) {
	cases := map[string][]domain.SessionMessage{
		"plain assistant turn": {
			{Role: domain.RoleAssistant, Content: "Done, pushed."},
			{Role: domain.RoleUser, Content: "thanks"},
		},
		"second reply after the answer": {
			{Role: domain.RoleAssistant, Content: "One question first.", Clarification: hostingQuestion},
			{Role: domain.RoleUser, Content: "Fly.io"},
			{Role: domain.RoleUser, Content: "and use the eu region"},
		},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			resumer := &recordingResumer{}
			session.RecordTaskChatAnswerForTest(context.Background(), &stubSessionStore{messages: msgs}, resumer,
				taskBoundSession(), domain.SessionMessageRequest{Role: domain.RoleUser, Content: msgs[len(msgs)-1].Content})
			assert.Empty(t, resumer.answers)
		})
	}
}

func TestUnboundChatAnswerStaysInTheChat(t *testing.T) {
	store := &stubSessionStore{messages: []domain.SessionMessage{
		{Role: domain.RoleAssistant, Clarification: hostingQuestion},
		{Role: domain.RoleUser, Content: "Fly.io"},
	}}
	resumer := &recordingResumer{}

	session.RecordTaskChatAnswerForTest(context.Background(), store, resumer, domain.Session{ID: uuid.New()},
		domain.SessionMessageRequest{Role: domain.RoleUser, Content: "Fly.io"})

	assert.Empty(t, resumer.answers)
}
