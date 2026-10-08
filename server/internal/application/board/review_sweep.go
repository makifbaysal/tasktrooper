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

// pass_to read off the stage's review verdict sweeps, not a fixed table: technical goes straight to human_uat, analiz_review is human-only.
func reviewExitColumn(wf domain.Workflow, column domain.TaskColumn) (domain.TaskColumn, bool) {
	target, ok := wf.Param(column, domain.BehaviourReviewVerdictSweep, "pass_to")
	if !ok || target == "" {
		return "", false
	}
	return domain.TaskColumn(target), true
}

// Must mirror Service.criteriaReviewGate: a disagreement makes the sweep ask the wrong role for a verdict.
func criterionReviewRole(wf domain.Workflow, column domain.TaskColumn) (domain.CriterionReviewRole, bool) {
	channel, ok := wf.Param(column, domain.BehaviourCriterionVerdict, "channel")
	if !ok {
		return "", false
	}
	switch channel {
	case "qa":
		return domain.CriterionReviewRoleQA, true
	case "pm":
		return domain.CriterionReviewRolePM, true
	default:
		return "", false
	}
}

func (r *Runner) missingVerdicts(ctx context.Context, job RunJob, role domain.CriterionReviewRole) []domain.AcceptanceCriterion {
	reader, ok := r.taskUpdater.(taskCriteriaReader)
	if !ok {
		return nil
	}
	items, err := reader.ListTaskCriteria(ctx, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review sweep: acceptance criteria unreadable")
		return nil
	}
	var missing []domain.AcceptanceCriterion
	for _, c := range items {
		ruled := false
		for _, check := range c.Checks {
			if check.Role == role {
				ruled = true
				break
			}
		}
		if !ruled {
			missing = append(missing, c)
		}
	}
	return missing
}

// stuckColumnComment tells a person the review ran but never moved the
// card, so nudging it forward (or back to need_revision) needs a human hand.
func stuckColumnComment(column, exit domain.TaskColumn, note string) string {
	return stuckColumnCommentKey.Render(stuckColumnCommentInput{Column: string(column), Exit: string(exit), Note: note})
}

func (r *Runner) stuckVerdictNote(ctx context.Context, job RunJob) string {
	role, ok := criterionReviewRole(r.workflowFor(ctx, job.Task.TaskType), job.Task.Column)
	if !ok {
		return ""
	}
	missing := r.missingVerdicts(ctx, job, role)
	if len(missing) == 0 {
		return ""
	}
	texts := make([]string, 0, len(missing))
	for _, c := range missing {
		texts = append(texts, c.Text)
	}
	return stuckVerdictNoteKey.Render(stuckVerdictNoteInput{Count: len(missing), Role: string(role), Texts: strings.Join(texts, "; ")})
}

func (r *Runner) finalizeReviewVerdict(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	headLen int,
	model string,
	policy domain.ToolPolicy,
	exit domain.TaskColumn,
) (bool, *domain.QuotaBlock) {
	if r.taskUpdater == nil {
		return false, nil
	}
	if role, ok := criterionReviewRole(r.workflowFor(ctx, job.Task.TaskType), job.Task.Column); ok {
		if missing := r.missingVerdicts(ctx, job, role); len(missing) > 0 {
			return false, nil
		}
	}

	ask := reviewVerdictAskPrompt(exit)
	turn := append(append([]domain.Message{}, history...), domain.Message{Role: domain.RoleUser, Content: ask})

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("review_verdict_finalize_start", map[string]any{"column": string(job.Task.Column)})
	}
	resp, err := r.agentLoop.RunTask(ctx, turn, model, agentRec.ProviderType, policy,
		agent.WithLightModel(agentRec.Model),
		agent.WithStableHead(headLen),
		agent.WithCLILabel(job.Task.Key+" review-verdict", job.Task.Title))
	if err != nil {
		if quotaErr, ok := domain.QuotaBlockOf(err); ok {
			return false, quotaErr
		}
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review verdict finalize failed; task stays in its review column")
		return false, nil
	}

	target, ok := verdictColumn(resp.Message.Content, exit)
	if !ok {
		log.Info().Str("task_id", job.Task.ID.String()).Msg("review verdict finalize: no verdict in the answer, leaving the column alone")
		return false, nil
	}

	agentID := job.Run.AgentID
	if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
		Column:       &target,
		Actor:        domain.TaskActorAgent,
		ActorAgentID: &agentID,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Str("target", string(target)).
			Msg("review verdict finalize: move refused")
		return false, nil
	}

	held := false
	if reader, ok := r.taskUpdater.(taskColumnReader); ok {
		if after, rerr := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); rerr == nil {
			held = after.Column == job.Task.Column
		}
	}
	if held {
		log.Info().Str("task_id", job.Task.ID.String()).
			Msg("review verdict finalize: approval recorded, task held for human review")
		return true, nil
	}
	log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(target)).
		Msg("review verdict finalize: verdict recorded, task moved")
	return true, nil
}

// reviewVerdictSweepPrompt nudges a reviewer who left the task in its review
// column back toward recording the move it already decided on. missing is
// nil whenever the stage has no criterion-verdict gate or nothing is left
// unruled — role is only read when missing is non-empty.
func reviewVerdictSweepPrompt(column, exit domain.TaskColumn, role domain.CriterionReviewRole, missing []domain.AcceptanceCriterion) string {
	var sb strings.Builder
	sb.WriteString(reviewVerdictSweepIntroKey.Render(reviewVerdictSweepIntroData{Column: string(column)}))
	sb.WriteString("\n")

	if len(missing) > 0 {
		sb.WriteString("\n")
		sb.WriteString(reviewVerdictSweepMissingIntroKey.Render(reviewVerdictSweepMissingIntroData{Role: string(role)}))
		sb.WriteString("\n")
		for _, c := range missing {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", c.ID, c.Text))
		}
		sb.WriteString(prompt.Text(reviewVerdictSweepMissingFooterKey))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(reviewVerdictSweepClosingKey.Render(reviewVerdictSweepClosingData{Exit: string(exit)}))
	return sb.String()
}

// reviewVerdictAskPrompt is the one-word verdict question asked right after
// a reviewer's own review turn; the answer is parsed back by verdictColumn.
func reviewVerdictAskPrompt(exit domain.TaskColumn) string {
	return reviewVerdictAskKey.Render(reviewVerdictAskData{Exit: string(exit)})
}

func verdictColumn(answer string, exit domain.TaskColumn) (domain.TaskColumn, bool) {
	word := strings.ToUpper(strings.Trim(strings.TrimSpace(answer), "`*_.!\"' \n\t"))
	switch {
	case word == "APPROVE":
		return exit, true
	case word == "REVISE":
		return domain.TaskColumnNeedRevision, true
	default:
		return "", false
	}
}

// A review run's only exit is the reviewer calling move_board_task; a verdict that is stated but not acted on still becomes the move it decided on.
func (r *Runner) sweepReviewVerdict(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
) *domain.QuotaBlock {
	if resp.Clarification != nil || resp.ResourceBlock != nil {
		return nil
	}
	wf := r.workflowFor(ctx, job.Task.TaskType)
	// No type check on purpose: only a stage whose review verdict sweeps name a pass_to ever sweeps, and analiz_review never carries one.
	exit, ok := reviewExitColumn(wf, job.Task.Column)
	if !ok {
		return nil
	}
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return nil
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review sweep: task re-read failed, leaving the column to the agent")
		return nil
	}
	if fresh.Column != job.Task.Column {
		return nil
	}
	if r.reviewQuorum.HasDecided(ctx, job.Task.ID, job.Task.Column, job.Run.AgentID) {
		return nil
	}

	var missing []domain.AcceptanceCriterion
	var role domain.CriterionReviewRole
	if reviewRole, ok := criterionReviewRole(wf, job.Task.Column); ok {
		role = reviewRole
		missing = r.missingVerdicts(ctx, job, reviewRole)
	}
	prompt := reviewVerdictSweepPrompt(job.Task.Column, exit, role, missing)

	rec := activity.FromContext(ctx)
	if rec != nil {
		rec.Step("review_verdict_sweep_start", map[string]any{"column": string(job.Task.Column)})
	}
	headLen := len(history)
	history = append(followUpHistory(history, resp), domain.Message{Role: domain.RoleUser, Content: prompt})
	swept, err := r.agentLoop.RunTask(ctx, history, model, agentRec.ProviderType, policy,
		agent.WithLightModel(agentRec.Model),
		agent.WithStableHead(headLen),
		agent.WithCLILabel(job.Task.Key+" review-sweep", job.Task.Title))
	if err != nil {
		if quotaErr, ok := domain.QuotaBlockOf(err); ok {
			return quotaErr
		}
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("review verdict sweep failed; task stays in its review column")
		return nil
	}
	if after, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID); err == nil && after.Column == job.Task.Column {
		log.Info().Str("task_id", job.Task.ID.String()).Str("column", string(job.Task.Column)).
			Msg("review verdict sweep ran and the task is still in its review column")
		if len(swept.Transcript) > 0 {
			history = swept.Transcript
		}
		moved, quotaErr := r.finalizeReviewVerdict(ctx, job, agentRec, history, headLen, model, policy, exit)
		if quotaErr != nil {
			return quotaErr
		}
		if moved {
			return nil
		}
		if r.taskUpdater != nil {
			if _, cErr := r.taskUpdater.AddComment(ctx, job.RepositoryID, job.Task.ID, domain.CreateTaskCommentRequest{
				AuthorType: "system",
				Content:    stuckColumnComment(job.Task.Column, exit, r.stuckVerdictNote(ctx, job)),
			}); cErr != nil {
				log.Warn().Err(cErr).Str("task_id", job.Task.ID.String()).Msg("review sweep: stuck-column comment failed")
			}
		}
	}
	return nil
}
