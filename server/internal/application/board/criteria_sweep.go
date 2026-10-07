package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const criteriaSweepRounds = 3

// The stages where a run is expected to move the task's own criteria forward:
// the queue it is claimed from, the column the work happens in, and the
// rework bounce. Not intake/parked/terminal (nothing runs there), not
// review/approval, and not a stage that records verdicts — there the criteria
// are someone else's work being judged, not the runner's to tick off.
func sweepsOwnCriteria(wf domain.Workflow, column domain.TaskColumn) bool {
	if wf.Has(column, domain.BehaviourCriterionVerdict) {
		return false
	}
	switch wf.KindOf(column) {
	case domain.StageKindQueue, domain.StageKindWork, domain.StageKindRework:
		return true
	default:
		return false
	}
}

func (r *Runner) sweepOpenCriteria(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
) (domain.AgentResponse, bool, *domain.QuotaBlock) {
	// Sweeping is for the stages where the work itself happens: a reviewer is
	// judging someone else's criteria, and a terminal/parked card has nobody
	// to nag. Derived from the stage's kind rather than carried as a flag —
	// "should this stage chase its own open criteria" has exactly one right
	// answer per kind, so it was never a decision worth exposing.
	wf := r.workflowFor(ctx, job.Task.TaskType)
	if !sweepsOwnCriteria(wf, job.Task.Column) {
		return resp, true, nil
	}
	open := r.openCriteria(ctx, job)
	if len(open) == 0 {
		return resp, true, nil
	}

	rec := activity.FromContext(ctx)
	if rec != nil {
		rec.Step("criteria_sweep_start", map[string]any{"open": len(open)})
	}

	// A host-executed session keeps its own transcript, so its follow-ups stack
	// the prompts on the opening context as before; the in-process loop hands
	// back its transcript and each round continues the previous one.
	stacked := followUpHistory(history, domain.AgentResponse{Message: resp.Message})
	current := resp
	for round := 1; round <= criteriaSweepRounds; round++ {
		ask := domain.Message{Role: domain.RoleUser, Content: criteriaSweepPrompt(open, round)}
		stacked = append(stacked, ask)
		turn := stacked
		if len(current.Transcript) > 0 {
			turn = append(followUpHistory(history, current), ask)
		}
		swept, err := r.agentLoop.RunTask(ctx, turn, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithStableHead(len(history)),
			agent.WithCLILabel(fmt.Sprintf("%s criteria-sweep %d", job.Task.Key, round), job.Task.Title))
		if err != nil {
			if quotaErr, ok := domain.QuotaBlockOf(err); ok {
				return resp, true, quotaErr
			}
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Int("round", round).
				Msg("acceptance criteria sweep failed; leaving the criteria as they stand")
			return resp, true, nil
		}
		if len(swept.Transcript) > 0 {
			current = swept
		}
		still := r.openCriteria(ctx, job)
		if len(still) == 0 {
			if rec != nil {
				rec.Step("criteria_sweep_settled", map[string]any{"rounds": round})
			}
			return resp, true, nil
		}
		open = still
		if rec != nil {
			rec.Step("criteria_sweep_round", map[string]any{"round": round, "open": len(still)})
		}
	}

	log.Info().Str("task_id", job.Task.ID.String()).Int("open", len(open)).Int("rounds", criteriaSweepRounds).
		Msg("acceptance criteria still open after the sweep loop")
	r.reportUnsettledCriteria(ctx, job, open)
	return resp, false, nil
}

func criteriaSweepPrompt(open []domain.AcceptanceCriterion, round int) string {
	var sb strings.Builder
	if round == 1 {
		sb.WriteString(prompt.Text(criteriaSweepOpenFirstKey))
	} else {
		sb.WriteString(criteriaSweepOpenRepeatKey.Render(criteriaSweepOpenRepeatData{Round: round - 1}))
	}
	sb.WriteString("\n")
	for _, c := range open {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
	}
	sb.WriteString(prompt.Text(criteriaSweepInstructionsKey))
	sb.WriteString("\n")
	if round == 1 {
		sb.WriteString(prompt.Text(criteriaSweepClosingFirstKey))
	} else {
		sb.WriteString(prompt.Text(criteriaSweepClosingRepeatKey))
	}
	return sb.String()
}

func (r *Runner) reportUnsettledCriteria(ctx context.Context, job RunJob, open []domain.AcceptanceCriterion) {
	if _, err := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
		Content:    unsettledCriteriaReport(open, criteriaSweepRounds),
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("unsettled criteria comment failed")
	}
}

// unsettledCriteriaReport is the system comment posted when the sweep loop
// runs out of rounds with criteria still open — it names each one so a
// person can act without re-reading the whole task.
func unsettledCriteriaReport(open []domain.AcceptanceCriterion, rounds int) string {
	var sb strings.Builder
	sb.WriteString(unsettledCriteriaHeaderKey.Render(unsettledCriteriaHeaderInput{Count: len(open), Rounds: rounds}))
	for _, c := range open {
		sb.WriteString("- " + c.Text + "\n")
	}
	sb.WriteString("\n" + prompt.Text(unsettledCriteriaFooterKey))
	return sb.String()
}

const unsettledCriteriaMarker = "unsettled acceptance criteria"

func unsettledCriteriaSummary(open int) string {
	return unsettledCriteriaSummaryKey.Render(unsettledCriteriaSummaryInput{Marker: unsettledCriteriaMarker, Open: open})
}

func isUnsettledCriteriaRun(run domain.TaskAgentRun) bool {
	return run.Status == domain.TaskAgentRunStatusFailed && strings.HasPrefix(run.Summary, unsettledCriteriaMarker)
}
