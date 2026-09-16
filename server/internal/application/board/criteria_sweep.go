package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// criteriaSweepRounds is how many times a run is sent back to its own unticked
// criteria before the board stops asking.
//
// THREE, and the shape of the three is the reason. The first pass asks the
// question ("you did not tick these — tick, or cancel with a reason"), and a
// run that merely forgot to record finished work answers it there. The second
// is the one that does WORK: by then the run has been told that leaving a
// criterion open parks the task, so the honest answer to "I skipped it" is to
// implement it now. The third exists because the second one's work has to be
// ticked, and a run that implements a criterion and then ends without recording
// it would leave the card in exactly the state the loop was built to prevent.
//
// It stops at three rather than looping until clean because the loop cannot
// distinguish "not done yet" from "cannot be done here": a criterion needing a
// credential nobody has, or an answer only a human can give, would otherwise
// re-drive the agent forever at full model cost. What the cap produces is a
// parked task with a written explanation, which is a state a person can act on.
const criteriaSweepRounds = 3

// sweepOpenCriteria is the end-of-run completion check: every acceptance
// criterion the run left unticked is put back in front of the agent, in the
// same conversation, before the run is allowed to close.
//
// Each round asks for one of three answers, and the third is what makes the
// loop terminate honestly:
//
//  1. It IS done and was not recorded → set_criterion_completed.
//  2. It is deliberately not being done → cancel_criterion with a reason, which
//     both settles the criterion and posts the reason as a task comment.
//  3. It was overlooked → do the work NOW, in this run, and then tick it.
//
// The third answer is the one the old single-pass sweep could not get. It asked
// the agent to "leave it open and say why", so an overlooked criterion produced
// a comment explaining that it had been overlooked, and the work never happened;
// the card then sat in front of the criteria gate until a human noticed. Re-
// asking after each round is what converts that comment into either the work or
// an explicit cancellation.
// sweepOpenCriteria's second return value is settled: false only when the loop
// ran every round of criteriaSweepRounds and at least one criterion was still
// neither ticked nor cancelled when it gave up. That is the one outcome the
// caller must not record as a clean finish — see the run.Status assignment in
// Runner.runTask, which is what turns "settled=false" into
// domain.TaskAgentRunStatusFailed so the reconciler's retry path picks the task
// back up instead of it sitting in this column with nothing watching it.
func (r *Runner) sweepOpenCriteria(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
) (domain.AgentResponse, bool) {
	if isReviewColumn(job.Task.Column) {
		return resp, true
	}
	switch job.Task.Column {
	case domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision:
	default:
		return resp, true
	}
	open := r.openCriteria(ctx, job)
	if len(open) == 0 {
		return resp, true
	}

	rec := activity.FromContext(ctx)
	if rec != nil {
		rec.Step("criteria_sweep_start", map[string]any{"open": len(open)})
	}

	history = append(history, domain.Message{Role: domain.RoleAssistant, Content: resp.Message.Content})

	for round := 1; round <= criteriaSweepRounds; round++ {
		history = append(history, domain.Message{Role: domain.RoleUser, Content: criteriaSweepPrompt(open, round)})
		if _, err := r.agentLoop.RunTask(ctx, history, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithCLILabel(fmt.Sprintf("%s criteria-sweep %d", job.Task.Key, round), job.Task.Title)); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Int("round", round).
				Msg("acceptance criteria sweep failed; leaving the criteria as they stand")
			// A tool/agent-loop error here is not the round cap running out —
			// the loop never got to ask three times, so this is not the
			// "unattended machine" signature the reconciler retry exists for.
			return resp, true
		}

		still := r.openCriteria(ctx, job)
		if len(still) == 0 {
			if rec != nil {
				rec.Step("criteria_sweep_settled", map[string]any{"rounds": round})
			}
			return resp, true
		}
		// Progress is not "fewer open": a round that cancelled one criterion
		// and ignored two others still moved, and the next round is asked about
		// the two that are left rather than the original list.
		open = still
		if rec != nil {
			rec.Step("criteria_sweep_round", map[string]any{"round": round, "open": len(still)})
		}
	}

	log.Info().Str("task_id", job.Task.ID.String()).Int("open", len(open)).Int("rounds", criteriaSweepRounds).
		Msg("acceptance criteria still open after the sweep loop")
	r.reportUnsettledCriteria(ctx, job, open)
	return resp, false
}

// criteriaSweepPrompt escalates. The first round assumes bookkeeping was
// missed; every round after it assumes the WORK was missed, because the first
// round already gave the agent the chance to say otherwise.
func criteriaSweepPrompt(open []domain.AcceptanceCriterion, round int) string {
	var sb strings.Builder
	if round == 1 {
		sb.WriteString("Before this run is closed, settle its acceptance criteria. These are still open:\n")
	} else {
		sb.WriteString(fmt.Sprintf("These acceptance criteria are STILL open after round %d of this check:\n", round-1))
	}
	for _, c := range open {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
	}
	sb.WriteString("\nFor EACH id above, do exactly one of three things now:\n" +
		"1. You implemented it in this run → call set_criterion_completed with that id.\n" +
		"2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. " +
		"That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.\n" +
		"3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.\n")
	if round == 1 {
		sb.WriteString("Do not tick anything you did not implement. " +
			"The hand-off to code_review is refused while any criterion is open, so a criterion you silently skip parks your finished work in this column.")
	} else {
		sb.WriteString("Answer 3 is the expected one at this point: you have already had a round to say the criterion was out of scope, " +
			"and you did not. Implement what is missing and tick it, or cancel it with a reason — an unanswered criterion parks this task " +
			"and a human has to come and find out why. Do not reply with a summary of what you would do; make the change.")
	}
	return sb.String()
}

// reportUnsettledCriteria writes the loop's own last word on the card. Without
// it the task parks in front of the criteria gate with the only explanation
// living inside a run transcript nobody opens.
func (r *Runner) reportUnsettledCriteria(ctx context.Context, job RunJob, open []domain.AcceptanceCriterion) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("This run ended with %d acceptance criterion/criteria unsettled after %d completion checks:\n", len(open), criteriaSweepRounds))
	for _, c := range open {
		sb.WriteString("- " + c.Text + "\n")
	}
	sb.WriteString("\nThey were neither implemented nor cancelled with a reason, so the task stays in this column: the hand-off to code_review is refused while a criterion is open. Either the work is still missing, or the criterion needs a decision only a person can make.")
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		Content:    sb.String(),
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("unsettled criteria comment failed")
	}
}

// unsettledCriteriaMarker prefixes the run.Summary a sweep that exhausted its
// rounds writes. There is no dedicated "why did this run fail" column on
// task_agent_runs, so this text IS the signal isUnsettledCriteriaRun reads back
// to tell a criteria-sweep failure apart from a crash — the distinction
// CriteriaLoopGuard needs before it may park a task on ResourceHumanDecision
// instead of leaving the reconciler's ordinary crash-retry path to keep
// spinning it.
const unsettledCriteriaMarker = "unsettled acceptance criteria"

// unsettledCriteriaSummary is the run.Summary a run gets when its criteria
// sweep exhausted every round with criteria still open.
func unsettledCriteriaSummary(open int) string {
	return fmt.Sprintf("%s: %d still open after the criteria sweep", unsettledCriteriaMarker, open)
}

// isUnsettledCriteriaRun reports whether a Failed run failed because its
// criteria sweep ran out of rounds, as opposed to a crash, a max-iteration cut
// off, or a git error — the other paths that also leave a run Failed.
func isUnsettledCriteriaRun(run domain.TaskAgentRun) bool {
	return run.Status == domain.TaskAgentRunStatusFailed && strings.HasPrefix(run.Summary, unsettledCriteriaMarker)
}
