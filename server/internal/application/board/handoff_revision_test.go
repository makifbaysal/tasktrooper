package board

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workflow/workflowtest"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type columnRefusingUpdater struct {
	fakeTaskUpdater
	refuse map[domain.TaskColumn]error
	fresh  []domain.BoardTask
	reads  int
}

func (u *columnRefusingUpdater) UpdateTask(ctx context.Context, repoID, taskID uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	if req.Column != nil {
		if err := u.refuse[*req.Column]; err != nil {
			u.calls = append(u.calls, req)
			return domain.BoardTask{}, err
		}
	}
	return u.fakeTaskUpdater.UpdateTask(ctx, repoID, taskID, req)
}

func (u *columnRefusingUpdater) GetTask(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	i := u.reads
	if i >= len(u.fresh) {
		i = len(u.fresh) - 1
	}
	u.reads++
	return u.fresh[i], nil
}

func runnerWithWorkflows(updater TaskUpdater, without bool) *Runner {
	fx := workflowtest.Default()
	if without {
		fx.Workflows[domain.TaskType("task")] = workflowWithoutNeedRevision()
		fx.Workflows[domain.TaskType("analiz")] = withoutNeedRevision(analizWF)
	}
	return &Runner{taskUpdater: updater, workflows: fx.Reader(), git: &handoffGit{diff: "diff"}}
}

func withoutNeedRevision(wf domain.Workflow) domain.Workflow {
	out := domain.Workflow{Type: wf.Type}
	for _, s := range wf.Stages {
		if s.Column != domain.TaskColumnNeedRevision {
			out.Stages = append(out.Stages, s)
		}
	}
	return out
}

func requireMovedTo(t *testing.T, updater *fakeTaskUpdater, column domain.TaskColumn, reason string) {
	t.Helper()
	require.NotEmpty(t, updater.calls)
	last := updater.calls[len(updater.calls)-1]
	require.NotNil(t, last.Column)
	assert.Equal(t, column, *last.Column)
	assert.Equal(t, reason, last.SystemReason)
	assert.Equal(t, domain.TaskActorSystem, last.Actor)
}

func TestReportVerificationFailureLeavesTheMoveToTheRunnerWhenRevisionExists(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: "task"}
	updater := &fakeTaskUpdater{task: task}
	r := runnerWithWorkflows(updater, false)

	r.reportVerificationFailure(context.Background(), runJobFor(task, uuid.New()), "exit status 1")

	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "need_revision")
	assert.Contains(t, updater.comments[0].Content, "exit status 1")
	assert.Empty(t, updater.calls, "the runner sends the task back once, not this helper")
}

func TestReportVerificationFailureFallsBackToTheWorkColumnWithoutRevision(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: "task"}
	updater := &fakeTaskUpdater{task: task}
	r := runnerWithWorkflows(updater, true)

	r.reportVerificationFailure(context.Background(), runJobFor(task, uuid.New()), "exit status 1")

	require.Len(t, updater.comments, 1)
	assert.NotContains(t, updater.comments[0].Content, "need_revision")
	requireMovedTo(t, updater, domain.TaskColumnInProgress, domain.MoveReasonVerificationFailed)
}

func TestSendBackForRevisionForBuildAndPlanVerification(t *testing.T) {
	for _, reason := range []string{domain.MoveReasonVerificationFailed, domain.MoveReasonPlanVerificationFailed} {
		t.Run(reason, func(t *testing.T) {
			task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: "task"}
			updater := &fakeTaskUpdater{task: task}
			r := runnerWithWorkflows(updater, false)

			moved := r.sendBackForRevision(context.Background(), runJobFor(task, uuid.New()), taskWF, "", reason)

			assert.True(t, moved)
			assert.Empty(t, updater.comments, "the comment is optional")
			requireMovedTo(t, updater, domain.TaskColumnNeedRevision, reason)
		})
	}
}

func TestSendBackForRevisionStaysWithoutTheStage(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &fakeTaskUpdater{task: task}
	r := runnerWithWorkflows(updater, true)

	moved := r.sendBackForRevision(context.Background(), runJobFor(task, uuid.New()), workflowWithoutNeedRevision(), "", domain.MoveReasonPlanVerificationFailed)

	assert.False(t, moved)
	assert.Empty(t, updater.calls)
}

func TestSendBackForRevisionLeavesATaskThatMovedElsewhere(t *testing.T) {
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress}
	updater := &readableUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		fresh:           domain.BoardTask{ID: task.ID, Column: domain.TaskColumnBlocked},
	}
	r := &Runner{taskUpdater: updater}

	moved := r.sendBackForRevision(context.Background(), runJobFor(task, uuid.New()), taskWF, "a comment", domain.MoveReasonVerificationFailed)

	assert.False(t, moved)
	assert.Empty(t, updater.calls)
	assert.Empty(t, updater.comments)
}

func gateRefusal() error {
	return domain.NewCriteriaGateError(domain.TaskColumnCodeReview, domain.CriteriaGateReasonIncomplete, nil, "criteria are open")
}

func TestAdvanceToCodeReviewSendsARefusedHandoffBack(t *testing.T) {
	refusals := map[string]error{
		"criteria gate":        gateRefusal(),
		"work order gate":      domain.NewWorkOrderGateError(domain.TaskColumnCodeReview, nil, "blocked"),
		"stage not configured": domain.NewStageNotOnWorkflowError("task", domain.TaskColumnCodeReview, "no stage"),
		"wrapped sentinel":     fmt.Errorf("%w — x", domain.ErrReviewStageSkipped),
		"typed rule":           domain.RefuseMove(errors.New("transition not allowed")),
		"wrapped gate":         fmt.Errorf("outer: %w", gateRefusal()),
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			agentID := uuid.New()
			task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
			updater := &columnRefusingUpdater{
				fakeTaskUpdater: fakeTaskUpdater{task: task},
				refuse:          map[domain.TaskColumn]error{domain.TaskColumnCodeReview: refusal},
				fresh:           []domain.BoardTask{task},
			}
			r := runnerWithWorkflows(updater, false)

			r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), taskWF, "/w/task-1", verifiedUsage())

			require.Len(t, updater.comments, 1)
			assert.Contains(t, updater.comments[0].Content, refusal.Error())
			assert.Contains(t, updater.comments[0].Content, "need_revision")
			require.Len(t, updater.calls, 2)
			requireMovedTo(t, &updater.fakeTaskUpdater, domain.TaskColumnNeedRevision, domain.MoveReasonHandoffRefused)
		})
	}
}

func TestAdvanceToCodeReviewDoesNotBounceOnInfraErrors(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	for _, infra := range []error{errors.New("connection reset"), context.Canceled, fmt.Errorf("tx: %w", errors.New("deadlock"))} {
		updater := &columnRefusingUpdater{
			fakeTaskUpdater: fakeTaskUpdater{task: task},
			refuse:          map[domain.TaskColumn]error{domain.TaskColumnCodeReview: infra},
			fresh:           []domain.BoardTask{task},
		}
		r := runnerWithWorkflows(updater, false)

		r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), taskWF, "/w/task-1", verifiedUsage())

		require.Len(t, updater.comments, 1)
		assert.Contains(t, updater.comments[0].Content, infra.Error())
		assert.NotContains(t, updater.comments[0].Content, "need_revision")
		assert.Len(t, updater.calls, 1, "only the refused code_review attempt")
	}
}

func TestAdvanceToCodeReviewRefusalWithoutRevisionStageStays(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &columnRefusingUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		refuse:          map[domain.TaskColumn]error{domain.TaskColumnCodeReview: gateRefusal()},
		fresh:           []domain.BoardTask{task},
	}
	r := runnerWithWorkflows(updater, true)

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), workflowWithoutNeedRevision(), "/w/task-1", verifiedUsage())

	require.Len(t, updater.comments, 1)
	assert.NotContains(t, updater.comments[0].Content, "need_revision")
	assert.Len(t, updater.calls, 1)
}

func TestAdvanceToCodeReviewRefusalDoesNotBounceATaskMovedMeanwhile(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, AssigneeAgentID: &agentID}
	updater := &columnRefusingUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		refuse:          map[domain.TaskColumn]error{domain.TaskColumnCodeReview: gateRefusal()},
		fresh:           []domain.BoardTask{task, {ID: task.ID, Column: domain.TaskColumnBlocked}},
	}
	r := runnerWithWorkflows(updater, false)

	r.advanceToCodeReview(context.Background(), runJobFor(task, agentID), taskWF, "/w/task-1", verifiedUsage())

	assert.Empty(t, updater.comments)
	assert.Len(t, updater.calls, 1, "only the refused code_review attempt")
}

func TestAdvanceToAnalizReviewSendsARefusedHandoffBack(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: "analiz"}
	updater := &columnRefusingUpdater{
		fakeTaskUpdater: fakeTaskUpdater{task: task},
		refuse:          map[domain.TaskColumn]error{domain.TaskColumnAnalizReview: gateRefusal()},
		fresh:           []domain.BoardTask{task},
	}
	r := runnerWithWorkflows(updater, false)
	_, ok := analizWF.Stage(domain.TaskColumnNeedRevision)
	require.True(t, ok, "the analiz fixture must have need_revision for this test to mean anything")

	r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), analizWF, documentedUsage())

	require.Len(t, updater.comments, 1)
	assert.Contains(t, updater.comments[0].Content, "need_revision")
	require.Len(t, updater.calls, 2)
	requireMovedTo(t, &updater.fakeTaskUpdater, domain.TaskColumnNeedRevision, domain.MoveReasonHandoffRefused)
}

func TestAdvanceToAnalizReviewRefusalStaysWithoutRevisionOrOnInfraError(t *testing.T) {
	agentID := uuid.New()
	task := domain.BoardTask{ID: uuid.New(), Column: domain.TaskColumnInProgress, TaskType: "analiz"}
	cases := map[string]struct {
		wf  domain.Workflow
		err error
	}{
		"no need_revision stage": {withoutNeedRevision(analizWF), gateRefusal()},
		"infra error":            {analizWF, errors.New("connection reset")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			updater := &columnRefusingUpdater{
				fakeTaskUpdater: fakeTaskUpdater{task: task},
				refuse:          map[domain.TaskColumn]error{domain.TaskColumnAnalizReview: tc.err},
				fresh:           []domain.BoardTask{task},
			}
			r := runnerWithWorkflows(updater, false)

			r.advanceToAnalizReview(context.Background(), runJobFor(task, agentID), tc.wf, documentedUsage())

			require.Len(t, updater.comments, 1)
			assert.NotContains(t, updater.comments[0].Content, "need_revision")
			assert.Len(t, updater.calls, 1)
		})
	}
}

func TestIsMoveRefusal(t *testing.T) {
	assert.False(t, domain.IsMoveRefusal(nil))
	assert.False(t, domain.IsMoveRefusal(errors.New("boom")))
	assert.False(t, domain.IsMoveRefusal(context.DeadlineExceeded))
	assert.True(t, domain.IsMoveRefusal(gateRefusal()))
	assert.Equal(t, "plain", domain.RefuseMove(errors.New("plain")).Error())
}
