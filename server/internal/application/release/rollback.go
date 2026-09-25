package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) rollbackAllowed(ctx context.Context, r domain.Release) (bool, string) {
	switch r.Status {
	case domain.ReleaseFailed, domain.ReleaseAwaitingVerdict:
		return true, ""
	case domain.ReleaseReleased:
	default:
		return false, fmt.Sprintf("this release is %s", r.Status)
	}
	if r.FinishedAt == nil || s.now().Sub(*r.FinishedAt) > 24*time.Hour {
		return false, "this release finished more than 24 hours ago"
	}
	last, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, s.now())
	if err != nil {
		if errors.Is(err, domain.ErrReleaseNotFound) {
			return false, "no released release was found for this component"
		}
		return false, "the component's newest released release could not be resolved: " + err.Error()
	}
	if last.ID != r.ID {
		return false, fmt.Sprintf("a newer release (%s) has since shipped for this component", last.Version)
	}
	return true, ""
}

// Rollback undoes a release: it always reverts the release's merge commits
// on the default branch, then (dispatch mode) redeploys the previous good
// release or (on_merge mode) lets the revert push itself redeploy.
func (s *Service) Rollback(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, reason domain.RollbackReason, note string) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if ok, why := s.rollbackAllowed(ctx, r); !ok {
		return domain.Release{}, fmt.Errorf("%w: rollback_release only applies to a failed or awaiting-verdict release, or one released within the last 24h that is still its component's newest (%s)",
			domain.ErrReleaseWrongStatus, why)
	}
	if !reason.Valid() {
		return domain.Release{}, fmt.Errorf("invalid rollback reason %q — must be deploy_failed, verify_failed, health_incident or manual", reason)
	}

	if !r.Profile.AutoRollback && actor == domain.ReleaseActorAgent {
		s.writeRollbackProposal(ctx, r, reason, note)
		return domain.Release{}, fmt.Errorf("%w: the proposal was written as a comment on the newest task for a human to confirm",
			domain.ErrRollbackNeedsHuman)
	}

	expect := r.Status
	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}
	if s.reverter == nil {
		return domain.Release{}, fmt.Errorf("no git reverter is configured on this deployment")
	}

	shas := revertSHAsNewestFirst(r.Tasks)
	if len(shas) == 0 {
		shas = []string{r.CommitSHA}
	}
	revertSHA, revertErr := s.reverter.RevertOnDefaultBranch(ctx, repo.RootPath, shas, revertCommitMessage(r))
	if revertErr != nil {
		r.Status = domain.ReleaseFailed
		r.FailureReason = "nothing was reverted; production is unchanged: " + revertErr.Error()
		if _, uerr := s.store.Update(ctx, r, expect); uerr != nil {
			log.Warn().Err(uerr).Str("release_id", r.ID.String()).Msg("release: recording a failed revert failed")
		}
		return domain.Release{}, fmt.Errorf("reverting the release's commits on the default branch: %w", revertErr)
	}

	rollback := &domain.ReleaseRollback{
		Reason:      reason,
		Note:        note,
		RevertSHA:   revertSHA,
		ManualSteps: s.manualStepsFor(ctx, r),
		Actor:       string(actor),
		StartedAt:   s.now(),
	}

	if r.DeployedAt == nil && r.Status == domain.ReleaseFailed {
		rollback.Mechanism = domain.RollbackMechanismRevert
		rollback.RestoredRef = revertSHA
		rollback.Detail = "the release never deployed; only the revert was needed"
		r.Rollback = rollback
		r.Status = domain.ReleaseRolledBack
		now := s.now()
		r.FinishedAt = &now
		updated, uerr := s.store.Update(ctx, r, expect)
		if uerr != nil {
			return domain.Release{}, uerr
		}
		s.reopenTasks(ctx, updated)
		return updated, nil
	}

	switch r.Mode {
	case domain.DeliveryDispatch:
		s.rollbackDispatch(ctx, repo, &r, rollback)
	default:
		rollback.Mechanism = domain.RollbackMechanismRevert
		rollback.RestoredRef = revertSHA
	}

	r.Rollback = rollback
	r.Status = domain.ReleaseRollingBack
	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	return updated, nil
}

// rollbackDispatch resolves and redeploys the previous good release for a
// dispatch-mode component; a dispatch refused for a CI-unavailable reason
// fails the release immediately (the revert already landed, so production
// is not running the bad code even though nothing new was deployed).
func (s *Service) rollbackDispatch(ctx context.Context, repo domain.Repository, r *domain.Release, rollback *domain.ReleaseRollback) {
	restoredRef := rollback.RevertSHA
	if prev, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, r.CreatedAt); err == nil {
		restoredRef = prev.CommitSHA
	} else if !errors.Is(err, domain.ErrReleaseNotFound) {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: finding the previous good release for rollback failed")
	}

	rollback.Mechanism = domain.RollbackMechanismWorkflow
	rollback.RestoredRef = restoredRef

	_, ciUnavailable, err := s.createAndDispatch(ctx, repo, r.Profile.Workflow, restoredRef)
	if err != nil {
		rollback.Detail = err.Error()
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: redeploying the previous good release failed")
		if ciUnavailable {
			r.Rollback = rollback
			r.Status = domain.ReleaseFailed
			r.FailureReason = "the rollback deploy failed — production may still run the bad release: " + err.Error()
		}
	}
}

func (s *Service) writeRollbackProposal(ctx context.Context, r domain.Release, reason domain.RollbackReason, note string) {
	if s.tasks == nil || len(r.Tasks) == 0 {
		return
	}
	newest := r.Tasks[len(r.Tasks)-1]
	msg := fmt.Sprintf("Rollback PROPOSED, not executed — auto_rollback is off for this component.\n\n"+
		"Reason: %s. %s\n\n"+
		"What would happen: revert %s's merge commit(s) on the default branch, then %s.\n\n"+
		"A human has to confirm it (POST the rollback endpoint with the repository name as the confirmation phrase).",
		reason, firstNonEmptyStr(note, "no further detail given"), r.Version, dispatchDescription(r))
	if _, err := s.tasks.AddComment(ctx, r.RepositoryID, newest.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    msg,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", newest.ID.String()).Msg("release: writing the rollback proposal comment failed")
	}
}

func dispatchDescription(r domain.Release) string {
	if r.Mode == domain.DeliveryDispatch {
		return "redeploy the previous good release"
	}
	return "the revert push itself redeploys production"
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// revertSHAsNewestFirst orders the release's tasks' merge commits newest
// first, matching the order RevertOnDefaultBranch expects to revert in.
// ReleaseTaskRef carries no timestamp of its own, but Tasks is filled added_at
// ascending (port.ReleaseStore's doc contract), so reversing it is newest
// first.
func revertSHAsNewestFirst(tasks []domain.ReleaseTaskRef) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if sha := strings.TrimSpace(t.MergeCommitSHA); sha != "" {
			out = append(out, sha)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func revertCommitMessage(r domain.Release) string {
	keys := make([]string, 0, len(r.Tasks))
	for _, t := range r.Tasks {
		if t.Key != "" {
			keys = append(keys, t.Key)
		}
	}
	return fmt.Sprintf("Revert release %s: %s", r.Version, strings.Join(keys, ", "))
}

// manualStepsFor renders each task's own rollback runbook. ReleaseTaskRef
// does not carry the runbook fields, so the full task is re-read.
func (s *Service) manualStepsFor(ctx context.Context, r domain.Release) []string {
	if s.tasks == nil {
		return nil
	}
	var steps []string
	for _, t := range r.Tasks {
		task, err := s.tasks.GetTask(ctx, r.RepositoryID, t.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: loading a task for its rollback runbook failed")
			continue
		}
		if runbook := domain.TaskRollbackRunbook(task); runbook != "" {
			label := t.Key
			if label == "" {
				label = t.ID.String()
			}
			steps = append(steps, label+": "+runbook)
		}
	}
	return steps
}

// reopenTasks resets every task and sends it back to need_revision; parked
// cards are taken first so the move is always FROM done.
func (s *Service) reopenTasks(ctx context.Context, r domain.Release) {
	comment := rollbackReopenComment(r)
	for _, t := range r.Tasks {
		if s.parked != nil {
			if _, _, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: claiming a parked card before reopening it failed")
			}
		}
		if s.mergeState != nil {
			if err := s.mergeState.ResetMergeState(ctx, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: resetting merge state failed")
			}
		}
		if s.tasks == nil {
			continue
		}
		col := domain.TaskColumnNeedRevision
		if _, err := s.tasks.UpdateTask(ctx, r.RepositoryID, t.ID, domain.UpdateBoardTaskRequest{
			Column:       &col,
			SystemReason: domain.MoveReasonReleaseRolledBack,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: moving a rolled-back task to need_revision failed")
			continue
		}
		if _, err := s.tasks.AddComment(ctx, r.RepositoryID, t.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    comment,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: posting the rollback comment failed")
		}
	}
}

func rollbackReopenComment(r domain.Release) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Release %s was rolled back (%s).", r.Version, r.Rollback.Reason)
	if note := strings.TrimSpace(r.Rollback.Note); note != "" {
		sb.WriteString(" " + note)
	}
	if r.Checks.EarlyStop != "" {
		fmt.Fprintf(&sb, " Evidence: %s.", r.Checks.EarlyStop)
	}
	fmt.Fprintf(&sb, "\n\nYour change was reverted on the default branch as %s. "+
		"Re-apply your change on the task branch (git revert %s), fix it, and send it through review again.",
		domain.ShortSHA(r.Rollback.RevertSHA), domain.ShortSHA(r.Rollback.RevertSHA))
	return sb.String()
}

// sweepRollingBack watches the rollback's deploy (Rollback.RestoredRef is
// already a commit sha, never a tag).
func (s *Service) sweepRollingBack(ctx context.Context, r domain.Release) {
	if r.Rollback == nil {
		log.Warn().Str("release_id", r.ID.String()).Msg("release sweeper: rolling_back release has no rollback record")
		return
	}
	status, err := s.statusForRestoredRef(ctx, r)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: resolving the rollback deploy status failed")
		return
	}

	now := s.now()
	elapsed := now.Sub(r.Rollback.StartedAt)
	switch status.State {
	case domain.DeployWatchSuccess:
		s.finishRollback(ctx, r)
	case domain.DeployWatchFailure:
		s.failRollback(ctx, r, status.Detail)
	case domain.DeployWatchNoSignal:
		if elapsed <= rollbackNoSignalWait {
			return
		}
		if r.Rollback.Mechanism == domain.RollbackMechanismRevert {
			s.finishRollback(ctx, r)
			return
		}
		s.failRollback(ctx, r, "no deploy signal for the rollback within 15 minutes")
	default:
		if elapsed > rollbackTimeout {
			s.failRollback(ctx, r, "the rollback deploy did not settle within 30 minutes")
		}
	}
}

func (s *Service) statusForRestoredRef(ctx context.Context, r domain.Release) (domain.DeployWatchStatus, error) {
	if s.deployStatus == nil {
		return domain.DeployWatchStatus{State: domain.DeployWatchUnknown}, nil
	}
	return s.deployStatus.StatusForCommit(ctx, r.RepositoryID, r.Rollback.RestoredRef, r.Profile.Workflow)
}

func (s *Service) finishRollback(ctx context.Context, r domain.Release) {
	now := s.now()
	r.Status = domain.ReleaseRolledBack
	r.FinishedAt = &now
	updated, err := s.store.Update(ctx, r, domain.ReleaseRollingBack)
	if err != nil {
		s.logSweepUpdate(err, r.ID)
		return
	}
	s.reopenTasks(ctx, updated)
}

func (s *Service) failRollback(ctx context.Context, r domain.Release, detail string) {
	r.Status = domain.ReleaseFailed
	reason := "the rollback deploy failed — production may still run the bad release"
	if detail != "" {
		reason += ": " + detail
	}
	r.FailureReason = reason
	updated, err := s.store.Update(ctx, r, domain.ReleaseRollingBack)
	if err != nil {
		s.logSweepUpdate(err, r.ID)
		return
	}
	s.handBack(ctx, updated)
	s.ingestRollbackFailureIncident(ctx, updated)
}

func (s *Service) ingestRollbackFailureIncident(ctx context.Context, r domain.Release) {
	if s.incidents == nil {
		return
	}
	if _, err := s.incidents.Ingest(ctx, domain.IncidentInput{
		RepositoryID: r.RepositoryID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceDeploy,
		Severity:     domain.IncidentSeverityCritical,
		Title:        fmt.Sprintf("Release rollback failed: %s", r.Version),
		Detail:       r.FailureReason,
		Fingerprint:  domain.IncidentFingerprint("release-rollback-failed", domain.DeployEnvProd, r.ID.String()),
		Payload: map[string]any{
			"release_id": r.ID.String(),
			"version":    r.Version,
		},
	}); err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: ingesting a rollback-failure incident failed")
	}
}
