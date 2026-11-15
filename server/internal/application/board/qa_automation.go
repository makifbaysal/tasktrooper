package board

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	automationDir        = "qa-automation"
	automationEntrypoint = "qa-automation/scripts/e2e.sh"
	automationFixRounds  = 2
	// Containers are pulled and migrated inside the suite: a cold run is
	// minutes, not the seconds a build stage gets.
	automationSuiteTimeout = 25 * time.Minute
)

// MoveReasonAutomationPassed / Failed mark the moves the automation round
// makes on QA's behalf: QA's own forward move is refused while the suite has
// not run, so the board makes it once the suite is green.
const (
	MoveReasonAutomationPassed = "automation_suite_passed"
	MoveReasonAutomationFailed = "automation_suite_failed"
)

type workingTreeScoper interface {
	ScopeWorkingTree(ctx context.Context, workspacePath, keepDir string) (kept, discarded []string, err error)
}

func qaAutomationOn(wf domain.Workflow, column domain.TaskColumn, repo domain.Repository) bool {
	return repo.AutomationTests && wf.Has(column, domain.BehaviourRequireExecutionEvidence)
}

func (r *Runner) automationOn(ctx context.Context, job RunJob, wf domain.Workflow) bool {
	repo, ok := runRepositoryFrom(ctx)
	if !ok && r.projects != nil {
		resolved, err := r.projects.ResolveRepository(ctx, job.RepositoryID)
		if err != nil {
			return false
		}
		repo = resolved
	}
	return qaAutomationOn(wf, job.Task.Column, repo)
}

// qaRoundPassed: every case run and none failed, every live criterion carrying
// QA's approval — the round the automation suite now has to confirm.
func (r *Runner) qaRoundPassed(ctx context.Context, job RunJob, wf domain.Workflow) bool {
	reader, ok := r.taskUpdater.(taskTestCaseReader)
	if !ok {
		return false
	}
	cases, err := reader.ListTestCases(ctx, job.Task.ID)
	if err != nil || len(cases) == 0 {
		return false
	}
	for _, c := range cases {
		if c.Status == domain.TestCaseStatusPlanned || c.Status == domain.TestCaseStatusFailed {
			return false
		}
	}
	role, ok := criterionReviewRole(wf, job.Task.Column)
	if !ok {
		return true
	}
	criteria, ok := r.taskUpdater.(taskCriteriaReader)
	if !ok {
		return false
	}
	items, err := criteria.ListTaskCriteria(ctx, job.Task.ID)
	if err != nil {
		return false
	}
	for _, c := range items {
		if c.Canceled {
			continue
		}
		approved := false
		for _, check := range c.Checks {
			if check.Role == role && check.Approved {
				approved = true
			}
		}
		if !approved {
			return false
		}
	}
	return true
}

// automationOutcome is where the automation round sends the task once the
// run is committed: the pass column, need_revision, or nowhere (QA moved it
// itself, or the round was not a pass to confirm).
type automationOutcome struct {
	Target  domain.TaskColumn
	Comment string
}

// runAutomationRound runs the repository's automation suite after a passing
// manual round. QA gets fix rounds for its own tests; a failure it traces to
// the product it reports itself and moves the task, which ends the round.
func (r *Runner) runAutomationRound(
	ctx context.Context,
	job RunJob,
	agentRec domain.Agent,
	history []domain.Message,
	resp domain.AgentResponse,
	model string,
	policy domain.ToolPolicy,
	wf domain.Workflow,
	workspace string,
) (automationOutcome, *domain.QuotaBlock) {
	if !r.stillInColumn(ctx, job) || !r.qaRoundPassed(ctx, job, wf) {
		return automationOutcome{}, nil
	}
	exit, ok := reviewExitColumn(wf, job.Task.Column)
	if !ok {
		return automationOutcome{}, nil
	}
	if workspace == "" || !r.verifyEnabled {
		return automationOutcome{Target: exit, Comment: prompt.Text(automationNoWorkspaceKey)}, nil
	}
	rec := activity.FromContext(ctx)
	headLen := len(history)
	current := resp
	for attempt := 0; ; attempt++ {
		if rec != nil {
			rec.Step("automation_suite_start", map[string]any{"attempt": attempt + 1})
		}
		passed, report := r.automationCheck(ctx, job, workspace)
		if passed {
			if rec != nil {
				rec.Step("automation_suite_passed", map[string]any{"attempt": attempt + 1})
			}
			return automationOutcome{Target: exit}, nil
		}
		if rec != nil {
			rec.Step("automation_suite_failed", map[string]any{"attempt": attempt + 1, "report": truncateTail(report, 2000)})
		}
		if attempt >= automationFixRounds {
			return automationOutcome{
				Target:  domain.TaskColumnNeedRevision,
				Comment: automationFailedCommentKey.Render(automationFailedCommentInput{Rounds: automationFixRounds, Report: truncateTail(report, 2500)}),
			}, nil
		}
		ask := domain.Message{Role: domain.RoleUser, Content: automationFixPromptKey.Render(automationFixPromptInput{Report: truncateTail(report, 6000)})}
		turn := append(followUpHistory(history, current), ask)
		fixed, err := r.agentLoop.RunTask(ctx, turn, model, agentRec.ProviderType, policy,
			agent.WithLightModel(agentRec.Model),
			agent.WithStableHead(headLen),
			agent.WithCLILabel(fmt.Sprintf("%s automation-fix %d", job.Task.Key, attempt+1), job.Task.Title))
		if err != nil {
			if quotaErr, ok := domain.QuotaBlockOf(err); ok {
				return automationOutcome{}, quotaErr
			}
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("automation fix round failed; the task stays in QA")
			return automationOutcome{}, nil
		}
		if len(fixed.Transcript) > 0 {
			current = fixed
		}
		if !r.stillInColumn(ctx, job) {
			r.scopeToAutomation(ctx, job, workspace)
			return automationOutcome{}, nil
		}
	}
}

func (r *Runner) stillInColumn(ctx context.Context, job RunJob) bool {
	reader, ok := r.taskUpdater.(taskColumnReader)
	if !ok {
		return false
	}
	fresh, err := reader.GetTask(ctx, job.RepositoryID, job.Task.ID)
	return err == nil && fresh.Column == job.Task.Column
}

// scopeToAutomation undoes whatever the QA run changed outside the
// automation project, so commit-on-finish pushes only the tests.
func (r *Runner) scopeToAutomation(ctx context.Context, job RunJob, workspace string) []string {
	scoper, ok := r.git.(workingTreeScoper)
	if !ok || workspace == "" {
		return nil
	}
	kept, discarded, err := scoper.ScopeWorkingTree(ctx, workspace, automationDir)
	if err != nil {
		log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Msg("automation: scoping the workspace to qa-automation failed")
	}
	if len(discarded) > 0 {
		log.Info().Str("task_id", job.Task.ID.String()).Strs("discarded", discarded).
			Msg("automation: QA changes outside qa-automation were undone")
	}
	return kept
}

func (r *Runner) automationCheck(ctx context.Context, job RunJob, workspace string) (bool, string) {
	kept := r.scopeToAutomation(ctx, job, workspace)
	if len(kept) == 0 && !r.branchTouchesAutomation(ctx, workspace) {
		return false, prompt.Text(automationNoTestsKey)
	}
	if !markerExists(workspace, automationEntrypoint) {
		return false, prompt.Text(automationNoEntrypointKey)
	}
	stages := append(nodeSetupStages(workspace), Stage{
		Name:    "automation",
		Command: []string{"bash", automationEntrypoint},
		Timeout: automationSuiteTimeout,
	})
	ok, report, _ := runStagesObserved(ctx, workspace, stages, nil, nil)
	return ok, report
}

func (r *Runner) branchTouchesAutomation(ctx context.Context, workspace string) bool {
	if r.git == nil {
		return false
	}
	files, err := r.git.TaskChangedFiles(ctx, workspace)
	if err != nil {
		return false
	}
	for _, f := range files {
		if strings.HasPrefix(f, automationDir+"/") {
			return true
		}
	}
	return false
}

// applyAutomationOutcome makes the move the automation round decided, after
// the run's tests were committed and pushed.
func (r *Runner) applyAutomationOutcome(ctx context.Context, job RunJob, wf domain.Workflow, out automationOutcome) {
	switch {
	case out.Target == "":
		return
	case out.Target == domain.TaskColumnNeedRevision:
		r.sendBackForRevision(ctx, job, wf, out.Comment, MoveReasonAutomationFailed)
	default:
		if out.Comment != "" {
			r.commentSystem(ctx, job, out.Comment)
		}
		target := out.Target
		if _, err := r.taskUpdater.UpdateTask(ctx, job.RepositoryID, job.Task.ID, domain.UpdateBoardTaskRequest{
			Column:       &target,
			SystemReason: MoveReasonAutomationPassed,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", job.Task.ID.String()).Str("target", string(target)).
				Msg("automation: moving the task on after a green suite was refused")
		}
	}
}
