package board

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A clarification session: an answer resumes exactly one task exactly once.
type BlockedTaskTaker interface {
	TakeBlockedBySession(ctx context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error)
}

type TaskCommenter interface {
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

type AnswerResumer struct {
	tasks      BlockedTaskTaker
	dispatcher *Dispatcher
	comments   TaskCommenter
}

func NewAnswerResumer(tasks BlockedTaskTaker, dispatcher *Dispatcher, comments TaskCommenter) *AnswerResumer {
	return &AnswerResumer{tasks: tasks, dispatcher: dispatcher, comments: comments}
}

// A fresh prompt could only re-ask the same question; the session is what makes the answer stick.
func (r *AnswerResumer) ResumeOnAnswer(ctx context.Context, sessionID uuid.UUID, answer string) bool {
	if r == nil || r.tasks == nil || r.dispatcher == nil {
		return false
	}
	task, ok, err := r.tasks.TakeBlockedBySession(ctx, sessionID)
	if err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("resume: blocked task lookup failed")
		return false
	}
	if !ok {
		return false
	}
	r.recordAnswer(ctx, task, answer)
	if err := r.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: task.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"resumed":                 "question_answered",
			"question":                task.BlockedQuestion,
			"answer":                  answer,
			domain.EventPayloadActor:  domain.EventActorSystem,
			domain.EventPayloadReason: domain.MoveReasonQuestionAnswered,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("resume: redispatch failed")
		return true
	}
	log.Info().Str("task_id", task.ID.String()).Msg("blocked task resumed after answer")
	return true
}

// RecordTaskAnswer is recordAnswer for a task-bound chat: the chat carries the
// conversation on, so the comment is informational and wakes no board run.
func (r *AnswerResumer) RecordTaskAnswer(ctx context.Context, repositoryID, taskID uuid.UUID, question, answer string) {
	if r == nil || r.comments == nil || strings.TrimSpace(answer) == "" {
		return
	}
	if _, err := r.comments.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
		AuthorType:    "system",
		Content:       prompt.ClarificationAnswerComment(question, answer),
		Informational: true,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("task chat: recording the answer on the task failed")
	}
}

func (r *AnswerResumer) recordAnswer(ctx context.Context, task domain.BoardTask, answer string) {
	if r.comments == nil || strings.TrimSpace(answer) == "" {
		return
	}
	content := prompt.ClarificationAnswerComment(task.BlockedQuestion, answer)
	if _, err := r.comments.AddComment(ctx, task.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    content,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("resume: recording the answer on the task failed")
	}
}
