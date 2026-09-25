package deploywatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deployops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Rollbacker interface {
	RollbackForTask(ctx context.Context, in deployops.RollbackInput, auth deployops.AgentRollbackAuthorization) (domain.DeployDispatch, error)
}

type GitReverter interface {
	// RevertOnDefaultBranch never touches the root checkout's working tree —
	// it runs in a detached worktree, so an unattended rollback cannot
	// destroy a human's uncommitted work in the repository's clone.
	RevertOnDefaultBranch(ctx context.Context, rootPath string, shas []string, message string) (string, error)
	HasGit(rootPath string) bool
}

type IncidentIngester interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
}

const (
	RollbackTriggerDeployFailed = "deploy_failed"

	RollbackTriggerHealthIncident = "health_incident"
)

type RollbackRequest struct {
	RepositoryID uuid.UUID
	TaskID       uuid.UUID
	Env          string

	Trigger string

	AgentName string

	Note string

	IncidentID uuid.UUID
}

func (s *Service) Rollback(ctx context.Context, req RollbackRequest) (domain.TaskRollbackResult, error) {
	if s.tasks == nil || s.targets == nil {
		return domain.TaskRollbackResult{}, ErrNotConfigured
	}
	env := strings.TrimSpace(req.Env)
	if env == "" {
		env = domain.DeployEnvProd
	}
	if strings.TrimSpace(req.Trigger) == "" {
		return domain.TaskRollbackResult{}, errors.New("a rollback needs a trigger: deploy_failed or health_incident")
	}

	task, err := s.tasks.Get(ctx, req.RepositoryID, req.TaskID)
	if err != nil {
		return domain.TaskRollbackResult{}, err
	}

	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		return domain.TaskRollbackResult{}, domain.ErrRollbackColumn
	}
	mergeSHA := strings.TrimSpace(task.MergeCommitSHA)
	if mergeSHA == "" {
		return domain.TaskRollbackResult{}, domain.ErrRollbackNotMerged
	}
	if err := s.assertOwnsLiveRelease(ctx, req.RepositoryID, env, mergeSHA); err != nil {
		return domain.TaskRollbackResult{}, err
	}
	if err := s.assertTrigger(ctx, task, req); err != nil {
		return domain.TaskRollbackResult{}, err
	}

	target, err := s.targets.Get(ctx, req.RepositoryID, "", env)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return domain.TaskRollbackResult{}, err
	}

	result := domain.TaskRollbackResult{
		Env:            env,
		RolledBackFrom: mergeSHA,
		IncidentID:     req.IncidentID,
		ManualSteps:    manualRollbackSteps(task),
	}

	if !target.AutoRollback {

		result.Proposed = true
		result.Message = s.proposeRollback(ctx, task, target, env, mergeSHA, req)
		return result, nil
	}

	executed, err := s.executeRollback(ctx, task, repoRollbackContext{
		Env:       env,
		MergeSHA:  mergeSHA,
		Trigger:   req.Trigger,
		AgentName: req.AgentName,
	})
	if err != nil {
		s.reportRollback(ctx, task, env, "Rollback FAILED: "+err.Error()+
			"\n\nProduction is still running this release. Escalate to a human now.", req)
		return result, err
	}
	result.RolledBack = true
	result.Mechanism = executed.Mechanism
	result.Ref = executed.Ref
	result.RevertSHA = executed.RevertSHA
	result.RolledBackTo = executed.RolledBackTo
	result.Message = executed.Message

	s.reportRollback(ctx, task, env, rollbackReport(result, task), req)
	return result, nil
}

func (s *Service) assertTrigger(ctx context.Context, task domain.BoardTask, req RollbackRequest) error {
	if req.Trigger != RollbackTriggerDeployFailed {
		return nil
	}
	status, err := s.statusForTask(ctx, task)
	if err != nil {

		return fmt.Errorf("deploy watch: could not confirm the deploy failed, refusing to roll back: %w", err)
	}
	if status.State == domain.DeployWatchFailure {
		return nil
	}
	return fmt.Errorf("%w (the deploy of %s is %s, not failed)",
		domain.ErrRollbackNoTrigger, domain.ShortSHA(status.MergeSHA), status.State)
}

func (s *Service) assertOwnsLiveRelease(ctx context.Context, repositoryID uuid.UUID, env, mergeSHA string) error {
	if s.runs == nil {
		return nil
	}
	latest, err := s.runs.Latest(ctx, repositoryID, env)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {

		return fmt.Errorf("deploy watch: reading the live deployment for %s failed, refusing to roll back on unknown state: %w", env, err)
	}
	live := strings.TrimSpace(latest.HeadSHA)
	if live == "" || strings.EqualFold(live, mergeSHA) {
		return nil
	}
	return fmt.Errorf("%w (live: %s, this task: %s)",
		domain.ErrRollbackNotOwner, domain.ShortSHA(live), domain.ShortSHA(mergeSHA))
}

type repoRollbackContext struct {
	Env       string
	MergeSHA  string
	Trigger   string
	AgentName string
}

type executedRollback struct {
	Mechanism    domain.RollbackMechanism
	Ref          string
	RevertSHA    string
	RolledBackTo string
	Message      string
}

func (s *Service) executeRollback(ctx context.Context, task domain.BoardTask, rc repoRollbackContext) (executedRollback, error) {
	auth := deployops.AgentRollbackAuthorization{
		TaskID:        task.ID,
		TaskKey:       task.Key,
		OwnedMergeSHA: rc.MergeSHA,
		Trigger:       rc.Trigger,
		AgentName:     rc.AgentName,
	}
	actor := "agent"
	if rc.AgentName != "" {
		actor = "agent:" + rc.AgentName
	}

	if s.rollbacks != nil {
		dispatch, err := s.rollbacks.RollbackForTask(ctx, deployops.RollbackInput{
			RepositoryID: task.RepositoryID,
			Env:          rc.Env,
			Actor:        actor,
		}, auth)
		switch {
		case err == nil:
			return executedRollback{
				Mechanism:    domain.RollbackMechanismWorkflow,
				Ref:          dispatch.Ref,
				RolledBackTo: dispatch.RollbackOfSHA,
				Message: fmt.Sprintf("Rolled back %s by dispatching %s at %s (tag %s), returning production to %s.",
					rc.Env, dispatch.WorkflowFile, domain.ShortSHA(dispatch.RollbackOfSHA), dispatch.Ref, domain.ShortSHA(dispatch.RollbackOfSHA)),
			}, nil
		case errors.Is(err, deployops.ErrNoWorkflowMapping):

		default:
			return executedRollback{}, err
		}
	}
	return s.revertRollback(ctx, task, rc)
}

func (s *Service) revertRollback(ctx context.Context, task domain.BoardTask, rc repoRollbackContext) (executedRollback, error) {
	if s.git == nil || s.repos == nil {
		return executedRollback{}, domain.ErrRollbackNoMechanism
	}
	repo, err := s.repos.Get(ctx, task.RepositoryID)
	if err != nil {
		return executedRollback{}, err
	}
	root := strings.TrimSpace(repo.RootPath)
	if root == "" || !s.git.HasGit(root) {
		return executedRollback{}, domain.ErrRollbackNoMechanism
	}
	message := fmt.Sprintf("revert: roll back %s (%s)\n\nThis reverts commit %s.\nRolled back automatically by TaskTrooper: %s.",
		task.Key, task.Title, rc.MergeSHA, rc.Trigger)
	revertSHA, err := s.git.RevertOnDefaultBranch(ctx, root, []string{rc.MergeSHA}, message)
	if err != nil {
		return executedRollback{}, fmt.Errorf("reverting %s on the default branch: %w", domain.ShortSHA(rc.MergeSHA), err)
	}
	return executedRollback{
		Mechanism: domain.RollbackMechanismRevert,
		Ref:       revertSHA,
		RevertSHA: revertSHA,
		Message: fmt.Sprintf("Rolled back %s by reverting %s on the default branch and pushing (%s). "+
			"This repository has no deploy workflow — it deploys on push, so the revert commit IS the rollback deploy.",
			rc.Env, domain.ShortSHA(rc.MergeSHA), domain.ShortSHA(revertSHA)),
	}, nil
}

func manualRollbackSteps(task domain.BoardTask) []string {
	var steps []string
	if task.HasMigration {
		steps = append(steps, "This task changed the DATABASE SCHEMA. Reverting the code does NOT reverse the migration — "+
			"the schema is still whatever the release left it as. Say so explicitly on the task and name who has to reverse it; do not report the rollback as complete.")
	}
	if runbook := domain.TaskRollbackRunbook(task); runbook != "" {
		steps = append(steps, "The task recorded its own rollback instructions. Perform each of them yourself and report what you did, "+
			"or say clearly which ones you could not:\n"+runbook)
	} else {
		steps = append(steps, "No rollback plan was recorded on this task, so nothing beyond the code revert has been undone. "+
			"Check by hand whether this change had a config, flag or data step, and say on the task that no plan existed.")
	}
	return steps
}

func (s *Service) proposeRollback(ctx context.Context, task domain.BoardTask, target domain.DeployTarget, env, mergeSHA string, req RollbackRequest) string {
	msg := fmt.Sprintf("Rollback PROPOSED, not executed — auto_rollback is off for %s.\n\n"+
		"What went wrong: %s (%s).\n"+
		"What is live: %s, merged from %s.\n"+
		"Proposed action: roll %s back off this commit.\n\n"+
		"A human has to confirm it: POST /v1/repositories/%s/deploy/%s/rollback with the repository name as the confirmation phrase. "+
		"Turning on auto_rollback for this target is what would let this be done automatically next time.",
		env, firstNonEmpty(req.Note, "the release failed"), req.Trigger,
		domain.ShortSHA(mergeSHA), task.Key, env, task.RepositoryID, env)
	if steps := manualRollbackSteps(task); len(steps) > 0 {
		msg += "\n\nEven once the code is rolled back, these are not automatic:\n- " + strings.Join(steps, "\n- ")
	}
	s.reportRollback(ctx, task, env, msg, req)
	_ = target
	return msg
}

func (s *Service) reportRollback(ctx context.Context, task domain.BoardTask, env, message string, req RollbackRequest) {
	if s.comments != nil {
		if _, err := s.comments.AddComment(ctx, task.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    message,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy watch: rollback comment failed")
		}
	}
	if s.incidents == nil {
		return
	}
	severity := domain.IncidentSeverityHigh
	if env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}
	if _, err := s.incidents.Ingest(ctx, domain.IncidentInput{
		RepositoryID: task.RepositoryID,
		Env:          env,
		Source:       domain.IncidentSourceDeploy,
		Severity:     severity,
		Title:        fmt.Sprintf("Release rollback: %s (%s)", task.Key, env),
		Detail:       message,

		Fingerprint: domain.IncidentFingerprint("release-rollback", env, task.ID.String()),
		Payload: map[string]any{
			"task_id":          task.ID.String(),
			"task_key":         task.Key,
			"merge_commit_sha": task.MergeCommitSHA,
			"trigger":          req.Trigger,
			"agent":            req.AgentName,
		},
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("deploy watch: rollback incident ingest failed")
	}
}

func rollbackReport(result domain.TaskRollbackResult, task domain.BoardTask) string {
	var sb strings.Builder
	sb.WriteString(result.Message)
	sb.WriteString("\n\nThis is the MECHANICAL half only. ")
	if len(result.ManualSteps) > 0 {
		sb.WriteString("The following were NOT undone by it:\n- ")
		sb.WriteString(strings.Join(result.ManualSteps, "\n- "))
	}
	if after := domain.TaskRollbackRunbook(task); after == "" {
		sb.WriteString("\n\n(The task recorded no rollback plan, which is itself worth fixing before the next release.)")
	}
	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type AutoRollbackDecision struct {
	Attribution domain.ReleaseAttribution
	Result      domain.TaskRollbackResult
	At          time.Time
}
