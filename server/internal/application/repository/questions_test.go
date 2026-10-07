package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeQuestionsTaskStore overrides fakePackageTaskStore's no-op
// ReleaseAnalysisQuestionsBlock with one that actually restores the
// task — the service tests below exercise the real release/restore logic,
// not just the column guard.
type fakeQuestionsTaskStore struct {
	*fakePackageTaskStore
}

func (f *fakeQuestionsTaskStore) ReleaseAnalysisQuestionsBlock(_ context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	task, ok := f.tasks[taskID]
	if !ok || task.BlockedResource != domain.ResourceAnalysisQuestions {
		return domain.BoardTask{}, false, nil
	}
	target := task.BlockedOriginColumn
	if target == "" {
		target = domain.TaskColumnInProgress
	}
	task.Column = target
	task.BlockedResource = ""
	task.BlockedOriginColumn = ""
	f.tasks[taskID] = task
	return task, true, nil
}

// fakeQuestionStore is a minimal in-memory port.TaskQuestionStore.
type fakeQuestionStore struct {
	byID map[uuid.UUID]domain.TaskQuestion
}

func newFakeQuestionStore() *fakeQuestionStore {
	return &fakeQuestionStore{byID: map[uuid.UUID]domain.TaskQuestion{}}
}

func (f *fakeQuestionStore) Create(_ context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	n := 0
	for _, existing := range f.byID {
		if existing.TaskID == q.TaskID {
			n++
		}
	}
	q.ID = uuid.New()
	q.Key = "Q" + itoaQ(n+1)
	if q.Status == "" {
		q.Status = domain.QuestionStatusOpen
	}
	f.byID[q.ID] = q
	return q, nil
}

func itoaQ(n int) string {
	digits := "0123456789"
	if n < 10 {
		return string(digits[n])
	}
	return itoaQ(n/10) + string(digits[n%10])
}

func (f *fakeQuestionStore) Get(_ context.Context, taskID, id uuid.UUID) (domain.TaskQuestion, error) {
	q, ok := f.byID[id]
	if !ok || q.TaskID != taskID {
		return domain.TaskQuestion{}, domain.ErrQuestionNotFound
	}
	return q, nil
}

func (f *fakeQuestionStore) GetByKey(_ context.Context, taskID uuid.UUID, key string) (domain.TaskQuestion, error) {
	for _, q := range f.byID {
		if q.TaskID == taskID && q.Key == key {
			return q, nil
		}
	}
	return domain.TaskQuestion{}, domain.ErrQuestionNotFound
}

func (f *fakeQuestionStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskQuestion, error) {
	var out []domain.TaskQuestion
	for _, q := range f.byID {
		if q.TaskID == taskID {
			out = append(out, q)
		}
	}
	return out, nil
}

func (f *fakeQuestionStore) Update(_ context.Context, q domain.TaskQuestion) (domain.TaskQuestion, error) {
	if _, ok := f.byID[q.ID]; !ok {
		return domain.TaskQuestion{}, domain.ErrQuestionNotFound
	}
	f.byID[q.ID] = q
	return q, nil
}

func (f *fakeQuestionStore) MarkSubmitted(_ context.Context, taskID uuid.UUID, at time.Time) ([]domain.TaskQuestion, error) {
	var out []domain.TaskQuestion
	for id, q := range f.byID {
		if q.TaskID != taskID || q.Status != domain.QuestionStatusAnswered || q.SubmittedAt != nil {
			continue
		}
		q.SubmittedAt = &at
		f.byID[id] = q
		out = append(out, q)
	}
	return out, nil
}

func newQuestionsService(task domain.BoardTask) (*Service, *fakeQuestionsTaskStore, *fakeQuestionStore, *fakeReleaseComments) {
	repos := &fakeReleaseRepoStore{repo: domain.Repository{ID: task.RepositoryID}}
	tasks := &fakeQuestionsTaskStore{&fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{task.ID: task}}}
	questions := newFakeQuestionStore()
	comments := &fakeReleaseComments{}
	svc := &Service{repos: repos, tasks: tasks, questions: questions, comments: comments}
	return svc, tasks, questions, comments
}

func TestAnswerQuestionColumnGuard(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnInProgress}
	svc, _, questions, _ := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "x", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	_, err = svc.AnswerQuestion(context.Background(), repoID, taskID, q.ID, "an answer")
	assert.ErrorIs(t, err, domain.ErrQuestionConflict, "in_progress is not a column where answers are accepted")
}

func TestAnswerQuestionAllowedWhileBlockedOnAnalysisQuestions(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked, BlockedResource: domain.ResourceAnalysisQuestions}
	svc, _, questions, _ := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	updated, err := svc.AnswerQuestion(context.Background(), repoID, taskID, q.ID, "  Use SQS.  ")
	require.NoError(t, err)
	assert.Equal(t, domain.QuestionStatusAnswered, updated.Status)
	assert.Equal(t, "Use SQS.", updated.Answer)
}

func TestAnswerQuestionAllowedInAnalizReview(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnAnalizReview}
	svc, _, questions, _ := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Keep old format?", Kind: domain.QuestionKindProduct, RecommendedAnswer: "keep it"})
	require.NoError(t, err)

	_, err = svc.AnswerQuestion(context.Background(), repoID, taskID, q.ID, "yes")
	assert.NoError(t, err)
}

func TestAnswerQuestionRefusesWithdrawn(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnAnalizReview}
	svc, _, questions, _ := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "x", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)
	q.Status = domain.QuestionStatusWithdrawn
	_, err = questions.Update(context.Background(), q)
	require.NoError(t, err)

	_, err = svc.AnswerQuestion(context.Background(), repoID, taskID, q.ID, "answer")
	assert.ErrorIs(t, err, domain.ErrQuestionConflict)
}

func TestSubmitQuestionsColumnGuard(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnInProgress}
	svc, _, _, _ := newQuestionsService(task)

	_, _, err := svc.SubmitQuestions(context.Background(), repoID, taskID)
	assert.ErrorIs(t, err, domain.ErrQuestionConflict)
}

func TestSubmitQuestionsRefusesPendingBlocking(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceAnalysisQuestions, BlockedOriginColumn: domain.TaskColumnInProgress}
	svc, _, questions, _ := newQuestionsService(task)
	_, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)

	_, _, err = svc.SubmitQuestions(context.Background(), repoID, taskID)
	assert.ErrorIs(t, err, domain.ErrPendingBlockingQuestions)
	assert.Contains(t, err.Error(), "Q1")
}

func TestSubmitQuestionsHappyPathRestoresOriginAndPostsSummary(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceAnalysisQuestions, BlockedOriginColumn: domain.TaskColumnNeedRevision}
	svc, tasks, questions, comments := newQuestionsService(task)
	q1, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)
	q1, err = domain.ApplyQuestionAnswer(q1, "Use SQS.", time.Now().UTC())
	require.NoError(t, err)
	_, err = questions.Update(context.Background(), q1)
	require.NoError(t, err)
	// A non-blocking question with no answer (recommended answer stands) must
	// not block or appear in the submit summary.
	_, err = questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Keep old format?", Kind: domain.QuestionKindProduct, RecommendedAnswer: "keep it"})
	require.NoError(t, err)

	updated, n, err := svc.SubmitQuestions(context.Background(), repoID, taskID)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, domain.TaskColumnNeedRevision, updated.Column, "the task returns to its origin column")
	assert.Empty(t, updated.BlockedResource)

	require.Len(t, comments.comments, 1)
	assert.Contains(t, comments.comments[0].Content, "Q1")
	assert.Contains(t, comments.comments[0].Content, "Use SQS.")

	after := tasks.tasks[taskID]
	assert.Equal(t, domain.TaskColumnNeedRevision, after.Column)
}

func TestSubmitQuestionsFallsBackToInProgressWithNoOrigin(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnBlocked,
		BlockedResource: domain.ResourceAnalysisQuestions}
	svc, _, questions, _ := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "x", Kind: domain.QuestionKindTechnical, Blocking: true})
	require.NoError(t, err)
	q, err = domain.ApplyQuestionAnswer(q, "y", time.Now().UTC())
	require.NoError(t, err)
	_, err = questions.Update(context.Background(), q)
	require.NoError(t, err)

	updated, _, err := svc.SubmitQuestions(context.Background(), repoID, taskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskColumnInProgress, updated.Column)
}

func TestRecordQuestionsRefusesNonAnaliz(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, TaskType: "task", Column: domain.TaskColumnInProgress}
	svc, _, _, _ := newQuestionsService(task)

	_, err := svc.RecordQuestions(context.Background(), repoID, taskID,
		[]domain.NewQuestionInput{{Prompt: "x", Kind: domain.QuestionKindTechnical, Blocking: true}}, nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "analiz")
}

func TestRecordQuestionsAddUpdateWithdrawRoundTrip(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, TaskType: domain.TaskTypeAnaliz, Column: domain.TaskColumnInProgress}
	svc, _, _, _ := newQuestionsService(task)

	items, err := svc.RecordQuestions(context.Background(), repoID, taskID, []domain.NewQuestionInput{
		{Prompt: "Which queue?", Kind: domain.QuestionKindTechnical, Blocking: true},
		{Prompt: "Keep old format?", Kind: domain.QuestionKindProduct, RecommendedAnswer: "keep it"},
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, items, 2)

	newPrompt := "Which message queue should the worker use?"
	items, err = svc.RecordQuestions(context.Background(), repoID, taskID, nil,
		[]domain.UpdateQuestionInput{{Key: "Q1", Prompt: &newPrompt}}, nil)
	require.NoError(t, err)
	var q1 domain.TaskQuestion
	for _, it := range items {
		if it.Key == "Q1" {
			q1 = it
		}
	}
	assert.Equal(t, newPrompt, q1.Prompt)

	items, err = svc.RecordQuestions(context.Background(), repoID, taskID, nil, nil, []string{"Q2"})
	require.NoError(t, err)
	for _, it := range items {
		if it.Key == "Q2" {
			assert.Equal(t, domain.QuestionStatusWithdrawn, it.Status)
		}
	}
}

func TestAnalizApprovalLeavesTheAnswersOnTheTask(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	task := domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnDone, TaskType: domain.TaskTypeAnaliz}
	svc, _, questions, comments := newQuestionsService(task)
	q, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Keep the old export format?", Kind: domain.QuestionKindProduct, RecommendedAnswer: "keep it"})
	require.NoError(t, err)
	q, err = domain.ApplyQuestionAnswer(q, "Drop it, nobody uses it.", time.Now().UTC())
	require.NoError(t, err)
	_, err = questions.Update(context.Background(), q)
	require.NoError(t, err)

	svc.markQuestionsSubmitted(context.Background(), repoID, taskID)

	require.Len(t, comments.comments, 1)
	assert.Equal(t, "system", comments.comments[0].AuthorType)
	assert.Contains(t, comments.comments[0].Content, "Keep the old export format?")
	assert.Contains(t, comments.comments[0].Content, "Drop it, nobody uses it.")

	svc.markQuestionsSubmitted(context.Background(), repoID, taskID)
	assert.Len(t, comments.comments, 1, "answers already on the task are not posted twice")
}

func TestAnalizApprovalWithNoAnswersPostsNothing(t *testing.T) {
	repoID, taskID := uuid.New(), uuid.New()
	svc, _, questions, comments := newQuestionsService(domain.BoardTask{ID: taskID, RepositoryID: repoID, Column: domain.TaskColumnDone})
	_, err := questions.Create(context.Background(), domain.TaskQuestion{TaskID: taskID, Prompt: "Keep it?", Kind: domain.QuestionKindProduct, RecommendedAnswer: "yes"})
	require.NoError(t, err)

	svc.markQuestionsSubmitted(context.Background(), repoID, taskID)

	assert.Empty(t, comments.comments)
}
