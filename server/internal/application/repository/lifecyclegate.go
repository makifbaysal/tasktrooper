package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type StageEvidence interface {
	LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error)
	// SetReviewPatchID stamps the diff a column's currently open span is being
	// approved on; called just before the move that closes it, so it lands on
	// the span the approval actually belongs to.
	SetReviewPatchID(ctx context.Context, taskID uuid.UUID, column, patchID string) error
	// LatestApprovedPatchID is the most recent CLOSED span of this column that
	// carries a patch id, and when it closed (its left_at) — the moment of
	// that approval. ok is false when the column was never approved this way.
	LatestApprovedPatchID(ctx context.Context, taskID uuid.UUID, column string) (patchID string, approvedAt time.Time, ok bool, err error)
}

func (s *Service) SetSpanStore(spans StageEvidence) {
	s.spans = spans
}

// recordCodeReviewApprovalPatchID stamps the current diff onto the code_review
// span that is about to close on this approving move. A patch id lookup
// failure (no git, no workspace) just leaves the span without one — the
// diff-skip stage that reads it back fails open on the same absence.
func (s *Service) recordCodeReviewApprovalPatchID(ctx context.Context, taskID uuid.UUID) {
	if s.spans == nil {
		return
	}
	patchID := s.currentTaskPatchID(ctx, taskID)
	if patchID == "" {
		return
	}
	if err := s.spans.SetReviewPatchID(ctx, taskID, string(domain.TaskColumnCodeReview), patchID); err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("recording the code review approval's patch id failed")
	}
}

// LatestApprovedReviewPatchID is the diff-skip stage's read of the most
// recent approval this column closed on: what patch id it approved, and
// when. ok is false when the column has no such approval, the span store is
// unavailable, or the read failed — every one of those must fail the caller
// open (run the agent), never open (skip it).
func (s *Service) LatestApprovedReviewPatchID(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn) (patchID string, approvedAt time.Time, ok bool) {
	if s.spans == nil {
		return "", time.Time{}, false
	}
	patchID, approvedAt, ok, err := s.spans.LatestApprovedPatchID(ctx, taskID, string(column))
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("reading the latest code review approval's patch id failed")
		return "", time.Time{}, false
	}
	return patchID, approvedAt, ok
}

func (s *Service) reviewChainGate(ctx context.Context, repo domain.Repository, task domain.BoardTask, prev, target domain.TaskColumn) error {
	if target != domain.TaskColumnDone && target != domain.TaskColumnReleased {
		return nil
	}

	if target == domain.TaskColumnReleased && prev == domain.TaskColumnDone {
		return nil
	}
	wf, err := s.workflow(ctx, task.TaskType)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read the task's workflow")
		return fmt.Errorf("%w: %s", domain.ErrReviewChainIncomplete,
			reviewChainWorkflowUnreadableKey.Render(reviewChainErrInput{Err: err.Error()}))
	}
	stages := wf.ReviewChain()
	if len(stages) == 0 {
		return nil
	}
	if s.spans == nil {
		return fmt.Errorf("%w: %s", domain.ErrReviewChainIncomplete, prompt.Text(reviewChainNoSpanStoreKey))
	}
	verdicts, err := s.spans.LatestVerdicts(ctx, task.ID)
	if err != nil {

		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("review-chain gate could not read span history")
		return fmt.Errorf("%w: %s", domain.ErrReviewChainIncomplete,
			reviewChainHistoryUnreadableKey.Render(reviewChainErrInput{Err: err.Error()}))
	}

	var missing, rejected []string
	for _, stage := range stages {

		if !s.boardHasColumn(ctx, stage.Column) {
			continue
		}
		verdict, visited := verdicts[string(stage.Column)]
		switch {
		case !visited:
			missing = append(missing, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		case verdict == domain.ReviewVerdictReject:
			rejected = append(rejected, fmt.Sprintf("%s (%s) — %s", stage.Label, stage.Column, stage.Remedy))
		}
	}

	if len(rejected) > 0 {
		return fmt.Errorf("%w — %s", domain.ErrReviewStageRejected,
			reviewChainStageRejectedKey.Render(reviewChainStageRejectedInput{
				Task: taskLabel(task), Target: string(target), Rejected: strings.Join(rejected, "; "),
			}))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w — %s", domain.ErrReviewChainIncomplete,
			reviewChainMissingStagesKey.Render(reviewChainMissingStagesInput{
				Task: taskLabel(task), Target: string(target), Missing: strings.Join(missing, "; "),
			}))
	}
	return nil
}

// reviewStageSkipGate refuses an agent handing a task out of a review stage
// past the next stage of its review chain — QA moving a task/bug from in_qa
// straight to human_uat because the work "looked technical". Visited or not
// does not matter here: the stage has to see this round's diff. Only a stage
// with a review_verdict_sweep is a hand-off point; a developer's in_progress
// → human_uat on an incident suggestion is not one.
//
// It fails open on an unreadable workflow because reviewChainGate still
// fails closed at done.
func (s *Service) reviewStageSkipGate(ctx context.Context, task domain.BoardTask, prev, target domain.TaskColumn, actor domain.TaskActor) error {
	if actor != domain.TaskActorAgent || prev == target {
		return nil
	}
	wf, err := s.workflow(ctx, task.TaskType)
	if err != nil {
		return nil
	}
	if passTo, ok := wf.Param(prev, domain.BehaviourReviewVerdictSweep, "pass_to"); !ok || passTo == "" {
		return nil
	}
	var skipped []string
	var next domain.TaskColumn
	for _, stage := range wf.ReviewStagesBetween(prev, target) {
		if !s.boardHasColumn(ctx, stage.Column) {
			continue
		}
		if next == "" {
			next = stage.Column
		}
		skipped = append(skipped, fmt.Sprintf("%s (%s)", stage.Label, stage.Column))
	}
	if len(skipped) == 0 {
		return nil
	}
	return fmt.Errorf("%w — %s", domain.ErrReviewStageSkipped,
		reviewStageSkippedKey.Render(reviewStageSkippedInput{
			Task: taskLabel(task), From: string(prev), Target: string(target), TaskType: string(task.TaskType),
			Skipped: strings.Join(skipped, ", "), Next: string(next),
		}))
}

func (s *Service) CheckReviewChain(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	if s.repos == nil || s.tasks == nil {
		return fmt.Errorf("repository store unavailable")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return err
	}
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return err
	}

	return s.reviewChainGate(ctx, repo, task, task.Column, domain.TaskColumnDone)
}

func (s *Service) boardHasColumn(ctx context.Context, col domain.TaskColumn) bool {
	if s.columns == nil {
		return domain.ValidTaskColumn(col)
	}
	return s.columns.ValidateColumn(ctx, string(col)) == nil
}

func taskLabel(task domain.BoardTask) string {
	if strings.TrimSpace(task.Key) != "" {
		return task.Key
	}
	return task.ID.String()
}
