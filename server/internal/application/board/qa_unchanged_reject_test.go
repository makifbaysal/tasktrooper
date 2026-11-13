package board

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func qaRejection(text, note, patchID string, at time.Time) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{
		ID:        uuid.New(),
		Text:      text,
		Completed: true, // the developer's mark; it does not answer QA
		Checks: []domain.CriterionCheck{
			{Role: domain.CriterionReviewRoleQA, Approved: false, Note: note, VerifiedPatchID: patchID, CheckedAt: at},
		},
	}
}

func unchangedRejectRunner(updater *skipTestUpdater, patchID string, blocker *blockRecorder, runs *capturingRunStore) *Runner {
	r := skipRunner(updater, &skipGit{patchID: patchID}, runs)
	r.blocker = blocker
	return r
}

// T-114: QA could not exercise a criterion in its environment (no Vercel
// preview), rejected it, the developer had nothing to change, and the same
// diff came back to QA again and again.
func TestParkUnchangedRejectedDiffParksInsteadOfRerunningQA(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	rejectedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		criteria: []domain.AcceptanceCriterion{
			qaRejection("hosted preview leads", "not exercised: no Vercel preview in this install", testPatchID, rejectedAt),
			criterionApproval(domain.CriterionReviewRoleQA, true, testPatchID, rejectedAt),
		},
	}
	blocker := &blockRecorder{previous: domain.TaskColumnInQA}
	runs := &capturingRunStore{}
	r := unchangedRejectRunner(updater, testPatchID, blocker, runs)
	job := runJobFor(task, uuid.New())

	parked := r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1")

	require.True(t, parked)
	resource, detail := blocker.parked()
	assert.Equal(t, domain.ResourceHumanDecision, resource)
	assert.Contains(t, detail, testPatchID[:12])
	assert.Empty(t, updater.calls, "QA's verdict is not replayed as a move")
	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "hosted preview leads — not exercised: no Vercel preview")
	assert.Equal(t, domain.TaskAgentRunStatusCompleted, runs.last.Status)
}

func TestParkUnchangedRejectedDiffRunsQAWhenTheDiffChanged(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		criteria: []domain.AcceptanceCriterion{
			qaRejection("x", "broken", "old-patch-id", time.Now().Add(-time.Hour)),
		},
	}
	blocker := &blockRecorder{}
	r := unchangedRejectRunner(updater, testPatchID, blocker, &capturingRunStore{})
	job := runJobFor(task, uuid.New())

	assert.False(t, r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1"))
	resource, _ := blocker.parked()
	assert.Empty(t, resource)
}

func TestParkUnchangedRejectedDiffRunsQAAfterAHumanWeighedIn(t *testing.T) {
	rejectedAt := time.Now().Add(-time.Hour)
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	criteria := []domain.AcceptanceCriterion{qaRejection("x", "no preview", testPatchID, rejectedAt)}

	t.Run("comment", func(t *testing.T) {
		updater := &skipTestUpdater{
			fakeTaskUpdater: fakeTaskUpdater{task: task},
			fresh:           task,
			criteria:        criteria,
			existingComments: []domain.TaskComment{
				{AuthorType: "user", Content: "Vercel is connected now, retry", CreatedAt: rejectedAt.Add(time.Minute)},
			},
		}
		r := unchangedRejectRunner(updater, testPatchID, &blockRecorder{}, &capturingRunStore{})
		job := runJobFor(task, uuid.New())
		assert.False(t, r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1"))
	})

	t.Run("human move", func(t *testing.T) {
		updater := &skipTestUpdater{fakeTaskUpdater: fakeTaskUpdater{task: task}, fresh: task, criteria: criteria}
		r := unchangedRejectRunner(updater, testPatchID, &blockRecorder{}, &capturingRunStore{})
		job := runJobFor(task, uuid.New())
		user := "user-1"
		job.Event = domain.BoardEvent{ID: uuid.New(), EventType: domain.BoardEventTaskMoved, ActorUserID: &user}
		assert.False(t, r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1"))
	})
}

func TestParkUnchangedRejectedDiffIgnoresNonQAColumnsAndCanceledCriteria(t *testing.T) {
	rejectedAt := time.Now().Add(-time.Hour)

	review := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: review},
		fresh:           review,
		criteria:        []domain.AcceptanceCriterion{qaRejection("x", "n", testPatchID, rejectedAt)},
	}
	r := unchangedRejectRunner(updater, testPatchID, &blockRecorder{}, &capturingRunStore{})
	job := runJobFor(review, uuid.New())
	assert.False(t, r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1"))

	qa := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	canceled := qaRejection("x", "n", testPatchID, rejectedAt)
	canceled.Canceled = true
	updater = &skipTestUpdater{fakeTaskUpdater: fakeTaskUpdater{task: qa}, fresh: qa, criteria: []domain.AcceptanceCriterion{canceled}}
	r = unchangedRejectRunner(updater, testPatchID, &blockRecorder{}, &capturingRunStore{})
	job = runJobFor(qa, uuid.New())
	assert.False(t, r.parkUnchangedRejectedDiff(context.Background(), job, job.Run, "/w/task-1"))
}
