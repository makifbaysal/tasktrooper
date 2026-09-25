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
		// fall through to the newer-open-release check below
	case domain.ReleaseReleased:
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
	default:
		return false, fmt.Sprintf("this release is %s", r.Status)
	}
	// A failed/awaiting_verdict release is neither Open() nor Terminal(), so
	// OpenForMerge's take-over-an-open-release logic never supersedes it —
	// a later merge can open its own release right beside it. Rolling this
	// one back now would fight that newer release for the branch/tag.
	if version, ok := s.newerOpenRelease(ctx, r); ok {
		return false, fmt.Sprintf("the component has a newer open release (%s) — resolve that one first", version)
	}
	return true, ""
}

// newerOpenRelease answers whether the component has an Open() release
// created after r that is not r itself.
func (s *Service) newerOpenRelease(ctx context.Context, r domain.Release) (string, bool) {
	if r.ComponentID == nil {
		return "", false
	}
	componentID := *r.ComponentID
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &r.RepositoryID,
		ComponentID:  &componentID,
		Statuses: []domain.ReleaseStatus{
			domain.ReleasePending, domain.ReleaseDeploying, domain.ReleaseVerifying, domain.ReleaseAwaitingVerdict,
		},
	})
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: checking for a newer open release before rollback failed")
		return "", false
	}
	for _, other := range releases {
		if other.ID != r.ID && other.CreatedAt.After(r.CreatedAt) {
			return other.Version, true
		}
	}
	return "", false
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

	// A retry: the previous attempt already landed a revert (its sha
	// survived onto r.Rollback even if that attempt then failed a later
	// step), so redoing it would try to revert commits no longer on top of
	// the default branch. Only the mechanism-specific redeploy/promote is
	// retried.
	retry := r.Rollback != nil && strings.TrimSpace(r.Rollback.RevertSHA) != ""
	// A release that never deployed put nothing on the provider or the
	// default branch's HEAD build to roll back beyond the revert itself —
	// computed from expect (the status as read) because the claim below
	// moves r.Status to rolling_back before this is used again. Never true on
	// a retry: the earlier attempt already resolved this once, and expect at
	// that point is the earlier attempt's OWN outcome (e.g. failed because
	// ITS redeploy failed), not the original pre-rollback state.
	neverDeployed := !retry && r.DeployedAt == nil && expect == domain.ReleaseFailed

	rollback := &domain.ReleaseRollback{
		Reason:    reason,
		Note:      note,
		Actor:     string(actor),
		StartedAt: s.now(),
	}
	var priorProvider providerRollbackAttempt
	if retry {
		rollback.RevertSHA = r.Rollback.RevertSHA
		rollback.ManualSteps = r.Rollback.ManualSteps
		if r.Rollback.Mechanism == domain.RollbackMechanismProvider {
			priorProvider = providerRollbackAttempt{success: true, targetID: r.Rollback.ProviderDeploymentID, detail: r.Rollback.Detail}
		}
	} else {
		rollback.ManualSteps = s.manualStepsFor(ctx, r)
	}

	// Claim BEFORE any side effect: a Finish, a supersede or a second
	// concurrent Rollback racing this one now loses its own conditional
	// Update instead of the two mutating production together. Should the
	// process die between this claim and the outcome Update below, the
	// release is left rolling_back with Rollback.RestoredRef still empty —
	// the sweeper's grace window (sweepRollingBack) is what notices and
	// fails it instead of leaving it stuck forever.
	r.Rollback = rollback
	r.Status = domain.ReleaseRollingBack
	claimed, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	r = claimed
	rollback = r.Rollback

	// Provider rollback runs BEFORE the revert (on_merge only): it is
	// seconds, the revert (and any dispatch redeploy) is minutes, and
	// production should stop serving the bad release as fast as possible.
	// Dispatch redeploys the previous release's own workflow run, which would
	// immediately undo a pinned provider rollback and then leave automatic
	// production assignment off for every later deploy; batch releases have
	// no comparable bound-environment notion. A retry reuses whatever the
	// earlier attempt already recorded instead of repeating it.
	var provider providerRollbackAttempt
	switch {
	case retry:
		provider = priorProvider
	case r.Mode == domain.DeliveryOnMerge && !neverDeployed:
		provider = s.attemptProviderRollback(ctx, r)
	}

	// A successful provider rollback already moved production — persisted
	// right away, before the revert risks the process dying with that fact
	// nowhere but memory, which would otherwise strand "production is
	// already safe" nowhere durable. A retry's provider outcome was already
	// persisted by the attempt it came from.
	if !retry && provider.success {
		rollback.Mechanism = domain.RollbackMechanismProvider
		rollback.ProviderDeploymentID = provider.targetID
		if provider.detail != "" {
			rollback.Detail = provider.detail
		}
		progressAt := s.now()
		rollback.ProgressAt = &progressAt
		r.Rollback = rollback
		persisted, perr := s.store.Update(ctx, r, domain.ReleaseRollingBack)
		if perr != nil {
			return domain.Release{}, perr
		}
		r = persisted
		rollback = r.Rollback
	}

	var revertSHA string
	if retry {
		revertSHA = rollback.RevertSHA
	} else {
		shas := revertSHAsNewestFirst(r.Tasks)
		if len(shas) == 0 {
			shas = []string{r.CommitSHA}
		}
		sha, revertErr := s.reverter.RevertOnDefaultBranch(ctx, repo.RootPath, shas, revertCommitMessage(r))
		if revertErr != nil {
			s.recordRevertFailure(ctx, r, rollback, provider, revertErr)
			return domain.Release{}, fmt.Errorf("reverting the release's commits on the default branch: %w", revertErr)
		}
		revertSHA = sha
		rollback.RevertSHA = revertSHA
		// Persisted immediately, before any redeploy/promote step below, so a
		// crash after the push lands does not make a retry re-revert commits
		// no longer on top of the default branch.
		progressAt := s.now()
		rollback.ProgressAt = &progressAt
		r.Rollback = rollback
		persisted, perr := s.store.Update(ctx, r, domain.ReleaseRollingBack)
		if perr != nil {
			return domain.Release{}, perr
		}
		r = persisted
		rollback = r.Rollback
	}
	if provider.detail != "" {
		rollback.Detail = provider.detail
	}

	if r.Mode == domain.DeliveryBatch {
		return s.finishBatchRollback(ctx, r, domain.ReleaseRollingBack, rollback)
	}

	if neverDeployed {
		rollback.Mechanism = domain.RollbackMechanismRevert
		rollback.RestoredRef = revertSHA
		rollback.Detail = "the release never deployed; only the revert was needed"
		r.Rollback = rollback
		r.Status = domain.ReleaseRolledBack
		now := s.now()
		r.FinishedAt = &now
		updated, uerr := s.store.Update(ctx, r, domain.ReleaseRollingBack)
		if uerr != nil {
			return domain.Release{}, uerr
		}
		s.reopenTasks(ctx, updated)
		return updated, nil
	}

	switch r.Mode {
	case domain.DeliveryDispatch:
		if provider.success {
			rollback.Mechanism = domain.RollbackMechanismProvider
			rollback.ProviderDeploymentID = provider.targetID
			rollback.RestoredRef = revertSHA
		} else {
			s.rollbackDispatch(ctx, repo, &r, rollback)
		}
	default:
		rollback.Mechanism = domain.RollbackMechanismRevert
		rollback.RestoredRef = revertSHA
		if provider.success {
			rollback.Mechanism = domain.RollbackMechanismProvider
			rollback.ProviderDeploymentID = provider.targetID
		}
	}

	// r.Status only changes above (rollbackDispatch's ANY-error path sets it
	// to failed); it is never reset to rolling_back here, or a failed
	// redeploy would look like an in-flight rollback the sweeper waits on
	// forever instead of a release a human must look at.
	r.Rollback = rollback
	updated, err := s.store.Update(ctx, r, domain.ReleaseRollingBack)
	if err != nil {
		return domain.Release{}, err
	}
	if updated.Status == domain.ReleaseFailed {
		s.handBack(ctx, updated)
	}
	return updated, nil
}

// recordRevertFailure persists why the revert did not land. When a provider
// rollback ran first and succeeded, production already stopped serving the
// bad release even though the default branch was not touched — the outcome
// says so explicitly (a stock "production is unchanged" would be false) and
// keeps the provider mechanism on the record, so a human knows the branch
// itself still needs a revert/merge; auto-assignment stays off until this
// release reaches released again.
func (s *Service) recordRevertFailure(ctx context.Context, r domain.Release, rollback *domain.ReleaseRollback, provider providerRollbackAttempt, revertErr error) {
	r.Status = domain.ReleaseFailed
	if provider.success {
		rollback.Mechanism = domain.RollbackMechanismProvider
		rollback.ProviderDeploymentID = provider.targetID
		rollback.Detail = provider.detail
		r.Rollback = rollback
		r.FailureReason = fmt.Sprintf(
			"production WAS rolled back to the provider's earlier deployment %s, but the default branch was NOT reverted (%s) — a human must revert or merge the fix; auto-assignment stays off until a promote.",
			provider.targetID, revertErr.Error())
	} else {
		r.FailureReason = "nothing was reverted; production is unchanged: " + revertErr.Error()
	}
	updated, uerr := s.store.Update(ctx, r, domain.ReleaseRollingBack)
	if uerr != nil {
		log.Warn().Err(uerr).Str("release_id", r.ID.String()).Msg("release: recording a failed revert failed")
		return
	}
	s.handBack(ctx, updated)
}

// rollbackDispatch resolves and redeploys the previous good release for a
// dispatch-mode component. A redeploy that fails for ANY reason fails the
// release: the revert already landed, so production is not running the
// bad code, but nothing new was deployed either — a human has to look at it
// rather than the sweeper waiting on a redeploy that was never dispatched.
func (s *Service) rollbackDispatch(ctx context.Context, repo domain.Repository, r *domain.Release, rollback *domain.ReleaseRollback) {
	restoredRef := rollback.RevertSHA
	if prev, err := s.store.LastReleased(ctx, r.RepositoryID, r.ComponentID, r.CreatedAt); err == nil {
		restoredRef = prev.CommitSHA
	} else if !errors.Is(err, domain.ErrReleaseNotFound) {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: finding the previous good release for rollback failed")
	}

	rollback.Mechanism = domain.RollbackMechanismWorkflow
	rollback.RestoredRef = restoredRef

	if _, _, err := s.createAndDispatch(ctx, repo, r.Profile.Workflow, restoredRef); err != nil {
		if rollback.Detail != "" {
			rollback.Detail += "; " + err.Error()
		} else {
			rollback.Detail = err.Error()
		}
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: redeploying the previous good release failed")
		r.Status = domain.ReleaseFailed
		r.FailureReason = "the rollback deploy failed — production may still run the bad release: " + err.Error()
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

// reopenTasks resets every task and sends the ones still sitting in
// done/released back to need_revision; parked cards are taken first so that
// move is always FROM done. A task a human already moved somewhere else
// (back into review, blocked, …) is left there — only commented on — since
// the rollback should explain itself without fighting a move nobody but that
// human asked for.
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
		if s.shouldMoveReopenedTask(ctx, r.RepositoryID, t.ID) {
			col := domain.TaskColumnNeedRevision
			if _, err := s.tasks.UpdateTask(ctx, r.RepositoryID, t.ID, domain.UpdateBoardTaskRequest{
				Column:       &col,
				SystemReason: domain.MoveReasonReleaseRolledBack,
			}); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: moving a rolled-back task to need_revision failed")
				continue
			}
		}
		if _, err := s.tasks.AddComment(ctx, r.RepositoryID, t.ID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    comment,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: posting the rollback comment failed")
		}
	}
}

// shouldMoveReopenedTask re-reads the task's live column rather than trusting
// r.Tasks (a snapshot from whenever the release was last read, and
// reopenTasks always runs a beat after that — the parked-card claim and
// merge-state reset above both precede it): cheap insurance against moving a
// task the human already moved out of done/released in that gap. A
// read failure moves it anyway — the old, safe default — rather than
// silently stranding a task nobody will look at again.
func (s *Service) shouldMoveReopenedTask(ctx context.Context, repositoryID, taskID uuid.UUID) bool {
	task, err := s.tasks.GetTask(ctx, repositoryID, taskID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", taskID.String()).Msg("release: reading a task's live column before reopening it failed")
		return true
	}
	return task.Column == domain.TaskColumnDone || task.Column == domain.TaskColumnReleased
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

// rollbackClaimGrace is how long a rolling_back release may sit with
// Rollback.RestoredRef still empty before the sweeper gives up on it, judged
// from ProgressAt (falling back to StartedAt when no side effect has landed
// yet) — a provider leg that already reported progress must not be judged
// abandoned by how long ago the claim itself started. Rollback claims
// rolling_back BEFORE any side effect runs, so RestoredRef is legitimately
// empty for the moment the revert/dispatch takes; past this window with no
// progress at all it means the process died in between and nothing will ever
// fill it.
const rollbackClaimGrace = 15 * time.Minute

// rollbackProgressBasis is when the rollback last provably did something:
// ProgressAt once a side effect has landed, else StartedAt.
func rollbackProgressBasis(rb *domain.ReleaseRollback) time.Time {
	if rb.ProgressAt != nil {
		return *rb.ProgressAt
	}
	return rb.StartedAt
}

// sweepRollingBack watches the rollback's deploy (Rollback.RestoredRef is
// already a commit sha, never a tag).
func (s *Service) sweepRollingBack(ctx context.Context, r domain.Release) {
	if r.Rollback == nil {
		log.Warn().Str("release_id", r.ID.String()).Msg("release sweeper: rolling_back release has no rollback record")
		return
	}
	if strings.TrimSpace(r.Rollback.RestoredRef) == "" {
		if s.now().Sub(rollbackProgressBasis(r.Rollback)) < rollbackClaimGrace {
			return
		}
		s.failRollback(ctx, r, "the rollback did not complete — the server may have restarted mid-way")
		return
	}
	if r.Rollback.Mechanism == domain.RollbackMechanismProvider {
		s.sweepRollingBackProvider(ctx, r)
		return
	}
	status, err := s.statusForRestoredRef(ctx, r)
	if err != nil {
		log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release sweeper: resolving the rollback deploy status failed")
		// A status-lookup error must not stall a rolling_back release
		// forever — the 30-minute timeout still applies, judged the same way
		// a resolved-but-unsettled status would be.
		if s.now().Sub(rollbackProgressBasis(r.Rollback)) > rollbackTimeout {
			s.failRollback(ctx, r, "the rollback deploy status could not be checked: "+err.Error())
		}
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

// statusForRestoredRef watches the rollback's own redeploy. The workflow
// mechanism redeploys the previous release's OWN sha/tag, which an earlier
// run (that release's original deploy, or a prior failed rollback attempt)
// may already have built green — StatusForCommitSince, keyed by since =
// Rollback.StartedAt, is what keeps that stale run from being read as this
// rollback's. The revert mechanism watches a brand-new commit no earlier run
// has ever touched, so it uses the plain StatusForCommit instead — Since is
// narrowed to Actions runs only, and the plain call's commit-status/
// deployment fallbacks are what make a push-to-deploy provider (no Actions
// run at all) observable here.
func (s *Service) statusForRestoredRef(ctx context.Context, r domain.Release) (domain.DeployWatchStatus, error) {
	if s.deployStatus == nil {
		return domain.DeployWatchStatus{State: domain.DeployWatchUnknown}, nil
	}
	if r.Rollback.Mechanism == domain.RollbackMechanismWorkflow {
		return s.deployStatus.StatusForCommitSince(ctx, r.RepositoryID, r.Rollback.RestoredRef, r.Profile.Workflow, r.Rollback.StartedAt)
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
