package board

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// parkUnchangedRejectedDiff is the reject-side mirror of skipUnchangedDiffGate.
// A task arriving at a criteria gate (QA) with the exact diff that gate
// already rejected cannot get a different verdict: re-running QA only repeats
// the rejection and sends the developer a task with nothing new to fix. That
// is how a criterion QA cannot exercise in its environment (no integration,
// credential, device) bounced between QA and the developer round after round.
// Instead of running QA, the task parks on a human decision with QA's
// rejection notes on it.
//
// A person's comment after the rejection, or a person moving the task into the
// gate, means they changed something QA should look at again, so the gate
// runs. Any read failure runs the gate too: a missed park only costs a round.
func (r *Runner) parkUnchangedRejectedDiff(ctx context.Context, job RunJob, run domain.TaskAgentRun, workspacePath string) bool {
	if r.taskUpdater == nil || r.git == nil || r.blocker == nil || workspacePath == "" {
		return false
	}
	role, ok := criterionReviewRole(r.workflowFor(ctx, job.Task.TaskType), job.Task.Column)
	if !ok {
		return false
	}
	if eventIsHuman(job.Event, decodeEventPayload(job.Event.Payload)) {
		return false
	}
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return false
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	if err != nil || fresh.Column != job.Task.Column || fresh.BlockedQuestion != "" {
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

	rejections, rejectedAt := rejectionsOfPatch(r.allCriteria(ctx, job), role, patchID)
	if len(rejections) == 0 {
		return false
	}
	if r.humanCommentedAfter(ctx, job, rejectedAt) {
		return false
	}

	short := patchID
	if len(short) > 12 {
		short = short[:12]
	}
	previous, err := r.blocker.BlockOnResource(ctx, job.RepositoryID, job.Task.ID, domain.ResourceHumanDecision,
		qaUnchangedRejectDetailKey.Render(qaUnchangedRejectDetailInput{ShortID: short}))
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).
			Msg("qa unchanged-reject: parking failed, running the gate instead")
		return false
	}
	r.parks.Record(ctx, job.RepositoryID, job.Task, previous,
		domain.ResourceHumanDecision, domain.MoveReasonQAUnchangedRejectParked)

	summary := qaUnchangedRejectCommentKey.Render(qaUnchangedRejectCommentInput{
		Column: string(job.Task.Column), ShortID: short, Rejections: rejections,
	})
	r.commentSystem(ctx, job, summary)

	run.Status = domain.TaskAgentRunStatusCompleted
	run.Summary = truncateHead(summary, 500)
	if r.runs != nil {
		pctx, cancel := persistCtx(ctx)
		defer cancel()
		if _, err := r.runs.Update(pctx, run); err != nil {
			log.Warn().Err(err).Str("run_id", run.ID.String()).Msg("qa unchanged-reject: run completion failed to persist")
		}
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(job.Task.Column)).
		Str("patch_id", short).Int("rejections", len(rejections)).
		Msg("qa unchanged-reject: gate already rejected this exact diff, task parked for a person")
	return true
}

// rejectionsOfPatch lists the role's standing rejections recorded against
// patchID, as "criterion — note" lines, and the latest of their times. The
// developer's Completed mark does not answer a QA rejection; only a canceled
// criterion no longer holds the task.
func rejectionsOfPatch(items []domain.AcceptanceCriterion, role domain.CriterionReviewRole, patchID string) ([]string, time.Time) {
	var lines []string
	var latest time.Time
	for _, c := range items {
		if c.Canceled {
			continue
		}
		check, found := criterionApprovalFor(c, role)
		if !found || check.Approved || check.VerifiedPatchID == "" || check.VerifiedPatchID != patchID {
			continue
		}
		line := strings.TrimSpace(c.Text)
		if note := strings.TrimSpace(check.Note); note != "" {
			line += " — " + truncateHead(note, 400)
		}
		lines = append(lines, line)
		if check.CheckedAt.After(latest) {
			latest = check.CheckedAt
		}
	}
	return lines, latest
}
