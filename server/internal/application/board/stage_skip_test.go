package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// skipTestUpdater is a TaskUpdater that also answers the two optional reads
// skipUnchangedDiffGate needs: the fresh task (taskColumnReader) and the
// code_review gate's last approval (reviewApprovalReader). Criteria gates
// (QA/PM) are read through taskCriteriaReader instead.
type skipTestUpdater struct {
	fakeTaskUpdater
	fresh    domain.BoardTask
	criteria []domain.AcceptanceCriterion
	// existingComments backs ListComments (the task's history); distinct from
	// fakeTaskUpdater.comments, which records AddComment calls THIS run made.
	existingComments []domain.TaskComment

	approvalPatchID string
	approvedAt      time.Time
	approvalFound   bool
}

func (u *skipTestUpdater) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return u.fresh, nil
}

func (u *skipTestUpdater) ListTaskCriteria(context.Context, uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	return u.criteria, nil
}

func (u *skipTestUpdater) ListComments(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskComment, error) {
	return u.existingComments, nil
}

func (u *skipTestUpdater) LatestApprovedReviewPatchID(context.Context, uuid.UUID, domain.TaskColumn) (string, time.Time, bool) {
	return u.approvalPatchID, u.approvedAt, u.approvalFound
}

type skipGit struct {
	port.GitClient
	patchID string
	err     error
}

func (g *skipGit) TaskPatchID(context.Context, string) (string, error) {
	return g.patchID, g.err
}

// capturingRunStore is countingRunStore (drain_test.go) plus the last run it
// was asked to persist, so a test can assert the completed status/summary.
type capturingRunStore struct {
	countingRunStore
	last domain.TaskAgentRun
}

func (c *capturingRunStore) Update(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error) {
	c.last = run
	return c.countingRunStore.Update(ctx, run)
}

func skipRunner(updater *skipTestUpdater, git port.GitClient, runs port.TaskAgentRunStore) *Runner {
	return &Runner{
		taskUpdater: updater,
		git:         git,
		workflows:   workflowtest.Default().Reader(),
		runs:        runs,
	}
}

func criterionApproval(role domain.CriterionReviewRole, approved bool, patchID string, checkedAt time.Time) domain.AcceptanceCriterion {
	return domain.AcceptanceCriterion{
		ID: uuid.New(),
		Checks: []domain.CriterionCheck{
			{Role: role, Approved: approved, VerifiedPatchID: patchID, CheckedAt: checkedAt},
		},
	}
}

const testPatchID = "abc123def456"

func TestSkipUnchangedDiffGateCodeReview(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	approvedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		approvalPatchID: testPatchID,
		approvedAt:      approvedAt,
		approvalFound:   true,
	}
	runs := &capturingRunStore{}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, runs)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.True(t, skipped)
	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnReadyForQA, *updater.calls[0].Column)
	assert.Equal(t, domain.TaskActorSystem, updater.calls[0].Actor)
	assert.Equal(t, domain.MoveReasonUnchangedDiff, updater.calls[0].SystemReason)
	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "Skipped")
	assert.Contains(t, updater.comments[0].Content, testPatchID[:12])
	assert.Equal(t, "system", updater.comments[0].AuthorType)
	assert.Equal(t, int32(1), runs.updates.Load())
	assert.Equal(t, domain.TaskAgentRunStatusCompleted, runs.last.Status)
	assert.Contains(t, runs.last.Summary, "Skipped")
}

func TestSkipUnchangedDiffGateQA(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	approvedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		criteria: []domain.AcceptanceCriterion{
			criterionApproval(domain.CriterionReviewRoleQA, true, testPatchID, approvedAt),
			criterionApproval(domain.CriterionReviewRoleQA, true, testPatchID, approvedAt.Add(time.Minute)),
		},
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.True(t, skipped)
	require.Len(t, updater.calls, 1)
	require.NotNil(t, updater.calls[0].Column)
	assert.Equal(t, domain.TaskColumnHumanUAT, *updater.calls[0].Column)
}

func TestSkipUnchangedDiffGateDoesNotSkipWhenPatchIDDiffers(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		approvalPatchID: "some-old-patch-id",
		approvedAt:      time.Now().Add(-time.Hour),
		approvalFound:   true,
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.False(t, skipped)
	assert.Empty(t, updater.calls)
}

func TestSkipUnchangedDiffGateDoesNotSkipWhenACriterionLacksTheApproval(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	approvedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		criteria: []domain.AcceptanceCriterion{
			criterionApproval(domain.CriterionReviewRoleQA, true, testPatchID, approvedAt),
			{ID: uuid.New()}, // no QA check at all
		},
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.False(t, skipped)
	assert.Empty(t, updater.calls)
}

func TestSkipUnchangedDiffGateDoesNotSkipWhenAHumanCommentedAfterApproval(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	approvedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		approvalPatchID: testPatchID,
		approvedAt:      approvedAt,
		approvalFound:   true,
		existingComments: []domain.TaskComment{
			{AuthorType: "human", Content: "hold on", CreatedAt: approvedAt.Add(time.Minute)},
		},
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.False(t, skipped)
	assert.Empty(t, updater.calls)
}

func TestSkipUnchangedDiffGateDoesNotSkipOnFirstPass(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		// approvalFound left false: this gate has never approved anything yet.
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.False(t, skipped)
	assert.Empty(t, updater.calls)
}

func TestSkipUnchangedDiffGateDoesNotSkipWhenGitLookupFails(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview, TaskType: "task"}
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           task,
		approvalPatchID: testPatchID,
		approvedAt:      time.Now().Add(-time.Hour),
		approvalFound:   true,
	}
	r := skipRunner(updater, &skipGit{err: errors.New("git unavailable")}, nil)
	job := runJobFor(task, agentID)

	skipped := r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1")

	assert.False(t, skipped)
	assert.Empty(t, updater.calls)
}

func TestSkipUnchangedDiffGateNilSafe(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnCodeReview}

	assert.NotPanics(t, func() {
		skipped := (&Runner{}).skipUnchangedDiffGate(context.Background(), runJobFor(task, agentID), domain.TaskAgentRun{}, "/w/task-1")
		assert.False(t, skipped)
	})
}

func TestSkipUnchangedDiffGateRunsTheAgentWhenTheMoveIsRefused(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	approvedAt := time.Now().Add(-time.Hour)
	updater := &skipTestUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task, err: errors.New("pipeline is red")},
		fresh:           task,
		criteria: []domain.AcceptanceCriterion{
			criterionApproval(domain.CriterionReviewRoleQA, true, testPatchID, approvedAt),
		},
	}
	r := skipRunner(updater, &skipGit{patchID: testPatchID}, nil)
	job := runJobFor(task, agentID)

	assert.False(t, r.skipUnchangedDiffGate(context.Background(), job, job.Run, "/w/task-1"))
	assert.Empty(t, updater.comments)
}
