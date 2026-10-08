package board

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// questionsReader is the board-runner's half of an analiz task's open
// questions — read-only, and by task id alone (no repository check), the
// same way analysisReader/reviewAnnotationReader read across a task
// boundary for run context.
type questionsReader interface {
	ListQuestionsByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error)
}

func (r *Runner) questionsFor(ctx context.Context, taskID uuid.UUID) []domain.TaskQuestion {
	reader, ok := r.taskUpdater.(questionsReader)
	if !ok {
		return nil
	}
	items, err := reader.ListQuestionsByTask(ctx, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("list open questions for run context failed")
		return nil
	}
	return items
}

func pendingBlockingQuestions(items []domain.TaskQuestion) []domain.TaskQuestion {
	var pending []domain.TaskQuestion
	for _, q := range items {
		if q.PendingBlocking() {
			pending = append(pending, q)
		}
	}
	return pending
}

// blockOnPendingQuestions is runner.go's end-of-run check, called right
// before advanceToAnalizReview and only when the run produced no
// clarification or resource block: an analiz run that recorded at least one
// still-open blocking question parks the task on analysis_questions instead
// of advancing — the report it attached stays attached, and SKIPping the
// advance means the non-blocking questions that rode along do not reach
// analiz_review until the blocking one is answered too.
func (r *Runner) blockOnPendingQuestions(ctx context.Context, job RunJob) bool {
	if !job.Task.TaskType.IsDocumentWork() || r.blocker == nil {
		return false
	}
	pending := pendingBlockingQuestions(r.questionsFor(ctx, job.Task.ID))
	if len(pending) == 0 {
		return false
	}
	detail := questionsBlockDetail(pending)
	previous, err := r.blocker.BlockOnResource(ctx, job.RepositoryID, job.Task.ID, domain.ResourceAnalysisQuestions, detail)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("blocking task on pending open questions failed")
		return false
	}
	r.parks.Record(ctx, job.RepositoryID, job.Task, previous, domain.ResourceAnalysisQuestions, domain.MoveReasonResourceBlocked)
	log.Info().Str("task_id", job.Task.ID.String()).Int("pending_blocking", len(pending)).
		Msg("analiz task parked on unanswered blocking question(s)")
	return true
}

func questionsBlockDetail(items []domain.TaskQuestion) string {
	lines := make([]string, 0, len(items))
	for _, q := range items {
		lines = append(lines, q.Key+": "+oneLineQuestion(q.Prompt))
	}
	return openQuestionsBlockDetailKey.Render(openQuestionsBlockDetailInput{Lines: lines})
}

func oneLineQuestion(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

const (
	questionsContextLimit = 8000
)

// openQuestionsContext is the "Open questions" run-context block: every run
// on an analiz task sees its OWN questions, and every run on a task derived
// from one (via AnalysisReferences, the same relation analysisContext reads)
// sees that analiz task's questions too — both with their answers, so a
// revision run or a decomposition honours what the human already said.
func (r *Runner) openQuestionsContext(ctx context.Context, job RunJob) string {
	if _, ok := r.taskUpdater.(questionsReader); !ok {
		return ""
	}
	var sb strings.Builder
	if job.Task.TaskType.IsDocumentWork() {
		sb.WriteString(renderQuestionsBlock(r.questionsFor(ctx, job.Task.ID), "", job.Task.Key))
	}
	if refReader, ok := r.taskUpdater.(analysisReader); ok {
		refs, err := refReader.AnalysisReferences(ctx, job.Task.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("analysis references for question context failed")
		}
		for _, ref := range refs {
			label := domain.RelationLabel(ref.Key, ref.Title, ref.TaskID)
			sb.WriteString(renderQuestionsBlock(r.questionsFor(ctx, ref.TaskID), label, ref.Key))
		}
	}
	return sb.String()
}

func renderQuestionsBlock(items []domain.TaskQuestion, label, taskRef string) string {
	items = domain.NonWithdrawn(items)
	if len(items) == 0 {
		return ""
	}
	blocks := make([]analysisQuestionBlock, 0, len(items))
	for _, q := range items {
		blocks = append(blocks, analysisQuestionBlock{
			Key: q.Key, Kind: string(q.Kind), Prompt: q.Prompt,
			Blocking: q.Blocking, Answered: q.Status == domain.QuestionStatusAnswered,
			Answer: q.Answer, RecommendedAnswer: q.RecommendedAnswer,
		})
	}
	rendered := analysisQuestionsKey.Render(analysisQuestionsData{Label: label, TaskRef: taskRef, Questions: blocks})
	if len(rendered) > questionsContextLimit {
		omitted := 0
		for len(rendered) > questionsContextLimit && len(blocks) > 1 {
			blocks = blocks[:len(blocks)-1]
			omitted++
			rendered = analysisQuestionsKey.Render(analysisQuestionsData{Label: label, TaskRef: taskRef, Questions: blocks, OmittedCount: omitted})
		}
	}
	return rendered
}
