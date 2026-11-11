package board

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// patchIDGit is the optional half of port.GitClient: adding TaskPatchID to
// the interface itself would force every test fake that embeds it to grow
// the method too. Its absence (a fake that doesn't implement it) is exactly
// the "lookup unavailable" case this stage fails open on.
type patchIDGit interface {
	TaskPatchID(ctx context.Context, workspacePath string) (string, error)
}

// reviewApprovalReader is the optional half of TaskUpdater that can answer
// "what diff did code_review last approve, and when" — only
// *repository.Service implements it, backed by task_column_spans.
type reviewApprovalReader interface {
	LatestApprovedReviewPatchID(ctx context.Context, taskID uuid.UUID, column domain.TaskColumn) (patchID string, approvedAt time.Time, ok bool)
}

// skipUnchangedDiffGate is the token-saving short-circuit: a task bouncing
// back into a gate column (code_review, ready_for_qa/in_qa) with the
// exact diff that gate already approved is moved forward the way that
// approval would move it, without spending a run re-reviewing/re-testing
// code nobody changed. It returns true only once it has itself completed the
// run and moved the task — the caller's execute() then returns immediately.
//
// Every condition below must hold, and any failure to determine one — a
// git error, a missing reader, an unreadable comment list — returns false
// (run the agent) rather than true (skip it): a false skip ships an
// unreviewed diff, a missed skip only costs the tokens this feature exists
// to save.
func (r *Runner) skipUnchangedDiffGate(ctx context.Context, job RunJob, run domain.TaskAgentRun, workspacePath string) bool {
	if r.taskUpdater == nil || r.git == nil || workspacePath == "" {
		return false
	}
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return false
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	if err != nil || fresh.Column != job.Task.Column {
		return false
	}
	if fresh.BlockedQuestion != "" {
		return false
	}

	pg, ok := r.git.(patchIDGit)
	if !ok {
		return false
	}
	patchID, err := pg.TaskPatchID(ctx, workspacePath)
	if err != nil || patchID == "" {
		return false
	}

	wf := r.workflowFor(ctx, job.Task.TaskType)
	target, ok := reviewExitColumn(wf, job.Task.Column)
	if !ok {
		return false
	}

	approvedAt, ok := r.gateAlreadyApproved(ctx, job, wf, patchID)
	if !ok || approvedAt.IsZero() {
		return false
	}

	if r.humanCommentedAfter(ctx, job, approvedAt) {
		return false
	}

	return r.applyUnchangedDiffSkip(ctx, job, run, target, patchID, approvedAt)
}

// gateAlreadyApproved reports whether the current column's gate has already
// approved patchID, and the earliest moment at which it did — the criteria
// gate (QA/PM) requires every criterion to carry that role's approval of
// this exact patch id; the code_review gate reads the span's last approval.
func (r *Runner) gateAlreadyApproved(ctx context.Context, job RunJob, wf domain.Workflow, patchID string) (time.Time, bool) {
	if role, ok := criterionReviewRole(wf, job.Task.Column); ok {
		items := r.allCriteria(ctx, job)
		if len(items) == 0 {
			return time.Time{}, false
		}
		var earliest time.Time
		for _, c := range items {
			check, found := criterionApprovalFor(c, role)
			if !found || !check.Approved || check.VerifiedPatchID == "" || check.VerifiedPatchID != patchID {
				return time.Time{}, false
			}
			if earliest.IsZero() || check.CheckedAt.Before(earliest) {
				earliest = check.CheckedAt
			}
		}
		return earliest, true
	}

	approver, ok := r.taskUpdater.(reviewApprovalReader)
	if !ok {
		return time.Time{}, false
	}
	approvedPatchID, approvedAt, found := approver.LatestApprovedReviewPatchID(ctx, job.Task.ID, job.Task.Column)
	if !found || approvedPatchID == "" || approvedPatchID != patchID {
		return time.Time{}, false
	}
	return approvedAt, true
}

func criterionApprovalFor(c domain.AcceptanceCriterion, role domain.CriterionReviewRole) (domain.CriterionCheck, bool) {
	for _, check := range c.Checks {
		if check.Role == role {
			return check, true
		}
	}
	return domain.CriterionCheck{}, false
}

// humanCommentedAfter fails open (true = "assume a human weighed in") on any
// read error, so a comment-list outage blocks the skip instead of silently
// ignoring feedback that may have landed after the approval.
func (r *Runner) humanCommentedAfter(ctx context.Context, job RunJob, after time.Time) bool {
	comments, err := r.taskUpdater.ListComments(ctx, job.RepositoryID, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
			Msg("diff-skip: comment list unreadable, not skipping the gate")
		return true
	}
	for _, c := range comments {
		if (c.AuthorType == "human" || c.AuthorType == "user") && c.CreatedAt.After(after) {
			return true
		}
	}
	return false
}

// diffSkipSummary is the run summary (and system comment) posted when a gate
// is skipped because the diff exactly matches what it already approved.
func diffSkipSummary(column domain.TaskColumn, shortPatchID string, approvedAt time.Time) string {
	return diffSkipSummaryKey.Render(diffSkipSummaryInput{
		Column:     string(column),
		ShortID:    shortPatchID,
		ApprovedAt: approvedAt.UTC().Format("2006-01-02 15:04Z"),
	})
}

func (r *Runner) applyUnchangedDiffSkip(ctx context.Context, job RunJob, run domain.TaskAgentRun, target domain.TaskColumn, patchID string, approvedAt time.Time) bool {
	short := patchID
	if len(short) > 12 {
		short = short[:12]
	}
	summary := diffSkipSummary(job.Task.Column, short, approvedAt)

	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &target,
		Actor:        domain.TaskActorSystem,
		SystemReason: domain.MoveReasonUnchangedDiff,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Str("target", string(target)).
			Msg("diff-skip: move refused, leaving the task for the agent")
		return false
	}

	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    summary,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("diff-skip: system comment failed")
	}

	run.Status = domain.TaskAgentRunStatusCompleted
	run.Summary = truncateHead(summary, 500)
	if r.runs != nil {
		pctx, cancel := persistCtx(ctx)
		defer cancel()
		if _, err := r.runs.Update(pctx, run); err != nil {
			log.Warn().Err(err).Str("run_id", run.ID.String()).Msg("diff-skip: run completion failed to persist")
		}
	}

	log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(job.Task.Column)).
		Str("target", string(target)).Str("patch_id", short).
		Msg("diff-skip: gate already approved this exact diff, task moved without starting the agent")
	return true
}
