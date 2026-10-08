package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var errQuestionsDisabled = errors.New("open questions not enabled")

var openQuestionsNonAnalizKey = prompt.Define[struct{}]("guard.open_questions_non_analiz", struct{}{})

type openQuestionsSummaryLinesInput struct{ Lines []string }

var openQuestionsSubmitSummaryKey = prompt.Define("board.open_questions_submit_summary",
	openQuestionsSummaryLinesInput{Lines: []string{"Q1: Which queue? → Use SQS."}})

// ListQuestions is the repo-scoped read GET /questions uses: it exists so a
// task from another repository answers 404 rather than leaking another
// board's questions.
func (s *Service) ListQuestions(ctx context.Context, repositoryID, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
		return nil, err
	}
	if s.questions == nil {
		return []domain.TaskQuestion{}, nil
	}
	return s.questions.ListByTask(ctx, taskID)
}

// ListQuestionsByTask skips the repository-ownership check ListQuestions
// makes: the board runner's context assembly (an analiz task's own run, and
// every run on a task derived from one) only ever has a task id to work
// from, the same reason AnalysisReferences/TaskDocumentStore.ListByTask read
// straight through by task id.
func (s *Service) ListQuestionsByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	if s.questions == nil {
		return nil, nil
	}
	return s.questions.ListByTask(ctx, taskID)
}

// AnswerQuestion is PATCH /questions/:id. Allowed only while the human is
// actually expected to be looking at this question: the task is blocked on
// it, or the report carrying it is under review.
func (s *Service) AnswerQuestion(ctx context.Context, repositoryID, taskID, questionID uuid.UUID, answer string) (domain.TaskQuestion, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.TaskQuestion{}, err
	}
	if s.questions == nil {
		return domain.TaskQuestion{}, errQuestionsDisabled
	}
	if err := requireQuestionsAnswerable(task); err != nil {
		return domain.TaskQuestion{}, err
	}
	q, err := s.questions.Get(ctx, taskID, questionID)
	if err != nil {
		return domain.TaskQuestion{}, err
	}
	next, err := domain.ApplyQuestionAnswer(q, answer, time.Now().UTC())
	if err != nil {
		return domain.TaskQuestion{}, err
	}
	return s.questions.Update(ctx, next)
}

// requireQuestionsAnswerable is PATCH /questions/:id and POST
// /questions/submit's shared column guard: a human answers these either
// while the task is blocked waiting on them, or later, while the report
// carrying them is under review.
func requireQuestionsAnswerable(task domain.BoardTask) error {
	if task.Column == domain.TaskColumnBlocked && task.BlockedResource == domain.ResourceAnalysisQuestions {
		return nil
	}
	if task.Column == domain.TaskColumnAnalizReview {
		return nil
	}
	return fmt.Errorf("%w: questions can only be answered while the task is blocked on %s or in %s (it is in %s)",
		domain.ErrQuestionConflict, domain.ResourceAnalysisQuestions, domain.TaskColumnAnalizReview, task.Column)
}

// RecordQuestions is record_open_questions' write path: add up to
// domain.MaxQuestionsPerTask new questions, edit ones already recorded, and
// withdraw ones an answer made moot. Returns the task's full current list,
// same as the tool.
func (s *Service) RecordQuestions(ctx context.Context, repositoryID, taskID uuid.UUID, add []domain.NewQuestionInput, update []domain.UpdateQuestionInput, withdraw []string) ([]domain.TaskQuestion, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return nil, err
	}
	if s.questions == nil {
		return nil, errQuestionsDisabled
	}
	if !task.TaskType.IsDocumentWork() {
		return nil, errors.New(prompt.Text(openQuestionsNonAnalizKey))
	}
	if len(add) > domain.MaxQuestionsPerTask {
		return nil, fmt.Errorf("%w: questions has %d entries, the limit per call is %d", domain.ErrQuestionInvalid, len(add), domain.MaxQuestionsPerTask)
	}

	for _, in := range add {
		if err := in.Validate(); err != nil {
			return nil, err
		}
		if _, err := s.questions.Create(ctx, domain.TaskQuestion{
			TaskID: taskID, Prompt: in.Prompt, Kind: in.Kind, Blocking: in.Blocking, RecommendedAnswer: in.RecommendedAnswer,
		}); err != nil {
			return nil, err
		}
	}

	for _, upd := range update {
		current, err := s.questions.GetByKey(ctx, taskID, upd.Key)
		if err != nil {
			return nil, err
		}
		next, err := domain.ApplyQuestionUpdate(current, upd)
		if err != nil {
			return nil, err
		}
		if _, err := s.questions.Update(ctx, next); err != nil {
			return nil, err
		}
	}

	for _, key := range withdraw {
		current, err := s.questions.GetByKey(ctx, taskID, key)
		if err != nil {
			return nil, err
		}
		if current.Status == domain.QuestionStatusWithdrawn {
			continue
		}
		current.Status = domain.QuestionStatusWithdrawn
		if _, err := s.questions.Update(ctx, current); err != nil {
			return nil, err
		}
	}

	return s.questions.ListByTask(ctx, taskID)
}

// SubmitQuestions is POST /questions/submit: the human sent answers to every
// pending blocking question, so the block clears, the task returns to the
// column it came from, and the analyst is re-dispatched with them — the
// questions analogue of board.AnswerResumer.ResumeOnAnswer, written here
// rather than in package board because the release (ReleaseAnalysisQuestionsBlock)
// and the dispatch both go through this Service already (see UpdateTask/emit).
func (s *Service) SubmitQuestions(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, int, error) {
	if s.questions == nil {
		return domain.BoardTask{}, 0, errQuestionsDisabled
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, 0, err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.BoardTask{}, 0, err
	}
	if task.Column != domain.TaskColumnBlocked || task.BlockedResource != domain.ResourceAnalysisQuestions {
		return domain.BoardTask{}, 0, fmt.Errorf("%w: questions can only be submitted while the task is blocked on %s (it is in %s)",
			domain.ErrQuestionConflict, domain.ResourceAnalysisQuestions, task.Column)
	}

	all, err := s.questions.ListByTask(ctx, taskID)
	if err != nil {
		return domain.BoardTask{}, 0, err
	}
	if pending := domain.PendingBlockingKeys(all); len(pending) > 0 {
		return domain.BoardTask{}, 0, fmt.Errorf("%w: %s", domain.ErrPendingBlockingQuestions, strings.Join(pending, ", "))
	}

	now := time.Now().UTC()
	submitted, err := s.questions.MarkSubmitted(ctx, taskID, now)
	if err != nil {
		return domain.BoardTask{}, 0, err
	}

	if len(submitted) > 0 && s.comments != nil {
		if _, cErr := s.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    questionsSubmitSummaryComment(submitted),
		}); cErr != nil {
			log.Warn().Err(cErr).Str("task_id", taskID.String()).Msg("submit questions: posting the answers summary comment failed")
		}
	}

	released, ok, err := s.tasks.ReleaseAnalysisQuestionsBlock(ctx, taskID)
	if err != nil {
		return domain.BoardTask{}, 0, err
	}
	if !ok {
		return domain.BoardTask{}, 0, fmt.Errorf("%w: the task is no longer blocked on %s", domain.ErrQuestionConflict, domain.ResourceAnalysisQuestions)
	}

	payload := map[string]interface{}{
		"resumed":                 "questions_answered",
		domain.EventPayloadActor:  domain.EventActorSystem,
		domain.EventPayloadReason: domain.MoveReasonQuestionsAnswered,
	}
	if err := s.emit(ctx, repo, released, domain.BoardEventTaskMoved, payload); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("submit questions: redispatch failed")
	}

	return released, len(submitted), nil
}

// markQuestionsSubmitted is the done-approval half: answered-but-unsubmitted
// questions are told to the agent the same as a submit, and their summary is
// left on the task — an approval posts no comment of its own, so without it
// the answers lived only in the question records. Informational: the move to
// done already dispatches whoever acts on the approval.
func (s *Service) markQuestionsSubmitted(ctx context.Context, repositoryID, taskID uuid.UUID) {
	if s.questions == nil {
		return
	}
	submitted, err := s.questions.MarkSubmitted(ctx, taskID, time.Now().UTC())
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("mark questions submitted on analiz approval failed")
		return
	}
	if len(submitted) == 0 || s.comments == nil {
		return
	}
	if _, err := s.AddComment(ctx, repositoryID, taskID, domain.CreateTaskCommentRequest{
		AuthorType:    "system",
		Content:       questionsSubmitSummaryComment(submitted),
		Informational: true,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("analiz approval: posting the answers summary comment failed")
	}
}

func questionsSubmitSummaryComment(items []domain.TaskQuestion) string {
	lines := make([]string, 0, len(items))
	for _, q := range items {
		lines = append(lines, fmt.Sprintf("%s: %s → %s", q.Key, oneLine(q.Prompt), oneLine(q.Answer)))
	}
	return openQuestionsSubmitSummaryKey.Render(openQuestionsSummaryLinesInput{Lines: lines})
}
