package release

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MergeGate refuses a task's merge for any of three reasons, checked in
// order: the component's delivery profile is not confirmed (its own workflow
// may still deploy this merge unwatched, so refusing beats guessing); it
// declares a deploy_depends_on task that has not reached released yet; or, on
// an on_merge component, it still has pending before-deploy steps — the
// merge itself is what would deploy them, and a human has to perform those
// steps first. It resolves the component the same way OpenForMerge would; an
// UNRESOLVED component (none recorded on the task or the repository at all)
// is not refused here — the merge goes through and the task simply waits in
// done the way OpenForMerge already handles that case.
//
// Every refusal is also recorded on the task as a merge hold, and a pass
// clears one, so the card always says what the merge is waiting for.
func (s *Service) MergeGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	hold, err := s.mergeGate(ctx, repositoryID, task)
	s.recordMergeHold(ctx, repositoryID, task, hold)
	return err
}

func (s *Service) mergeGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) (domain.ResourceBlock, error) {
	component, name, ok := s.resolveComponent(ctx, repositoryID, task)
	if !ok {
		return domain.ResourceBlock{}, nil
	}
	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed {
		// The repository's own workflow may still deploy this merge (a push
		// trigger nobody told the delivery profile about) — refuse rather
		// than merge blind. OpenPending wakes this task once a human
		// confirms the profile on the Deploy tab.
		s.commentOnce(ctx, repositoryID, task.ID, prompt.Text(deliveryUnconfirmedCommentKey))
		return domain.ResourceBlock{Resource: domain.ResourceDeliveryProfile, Detail: name},
			fmt.Errorf("%w: %s", domain.ErrDeliveryUnconfirmed,
				deliveryUnconfirmedKey.Render(releaseNameInput{Name: name}))
	}
	if profile.Mode != domain.DeliveryOnMerge {
		return domain.ResourceBlock{}, nil
	}
	if pending := s.pendingDeployDependencies(ctx, []uuid.UUID{task.ID}); len(pending) > 0 {
		s.commentDeployDependencies(ctx, repositoryID, task.ID, pending)
		return domain.ResourceBlock{Resource: domain.ResourceDeployOrder, Detail: strings.Join(pending, ", ")},
			fmt.Errorf("%s deploys on merge, so the merge waits: %w", name, deployDependencyError(pending))
	}
	if hold, err := s.envGate(ctx, repositoryID, component, name, task.ID); err != nil {
		return hold, err
	}
	if !task.BeforeDeployPending() {
		return domain.ResourceBlock{}, nil
	}
	steps := task.BeforeDeploySteps()
	s.commentOnce(ctx, repositoryID, task.ID,
		beforeDeployPendingMergeCommentKey.Render(releaseNameStepsInput{Name: name, Steps: steps}))
	return domain.ResourceBlock{Resource: domain.ResourceBeforeDeploy, Detail: steps},
		fmt.Errorf("%w: %s", domain.ErrBeforeDeployPending,
			beforeDeployPendingMergeKey.Render(releaseNameStepsInput{Name: name, Steps: steps}))
}

func (s *Service) recordMergeHold(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, hold domain.ResourceBlock) {
	if s.holds == nil {
		return
	}
	if hold.Resource == "" {
		s.releaseMergeHold(ctx, task, domain.MergeHoldResources...)
		return
	}
	if task.BlockedResource == hold.Resource && task.BlockedQuestion == hold.Detail {
		return
	}
	if err := s.holds.HoldMerge(ctx, repositoryID, task.ID, hold.Resource, hold.Detail); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("resource", hold.Resource).
			Msg("release: recording why the merge waits failed")
	}
}

// releaseMergeHold clears the task's hold if it is one of resources — called
// the moment what it waited for is resolved, so the card stops claiming to wait
// while the woken agent is still on its way to the merge.
func (s *Service) releaseMergeHold(ctx context.Context, task domain.BoardTask, resources ...string) {
	if s.holds == nil || !slices.Contains(resources, task.BlockedResource) {
		return
	}
	if err := s.holds.ReleaseMergeHold(ctx, task.ID, resources...); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("resource", task.BlockedResource).
			Msg("release: clearing a merge hold failed")
	}
}

// WakeTask wakes the release engineer on a task sitting in `done` — used by
// the before-deploy confirm endpoint so a merge/deploy that was only waiting
// on a human resumes immediately instead of on the next poll. It looks up
// the task's release (if one was already opened, e.g. a dispatch release
// whose Deploy call was refused) to report an accurate release_status; a
// task still waiting to be merged has none yet, and that is not an error.
func (s *Service) WakeTask(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	if s.waker == nil {
		return nil
	}
	status := domain.ReleaseStatus("")
	if s.store != nil {
		if r, err := s.store.ForTask(ctx, task.ID); err == nil {
			status = r.Status
		} else if !errors.Is(err, domain.ErrReleaseNotFound) {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: resolving a task's release before waking it failed")
		}
	}
	return s.waker.Wake(ctx, repositoryID, task, status)
}

// beforeDeployGate refuses a dispatch release's Deploy while any of its
// tasks has pending before-deploy steps, commenting once (on the newest
// task) with every pending step so a human sees the whole list in one place.
func (s *Service) beforeDeployGate(ctx context.Context, r domain.Release) error {
	pending := pendingBeforeDeployTasks(r.Tasks)
	if len(pending) == 0 {
		return nil
	}
	s.commentPendingBeforeDeploy(ctx, r, pending)
	return fmt.Errorf("%w: %s", domain.ErrBeforeDeployPending, pendingBeforeDeployMessage(pending))
}

func pendingBeforeDeployTasks(tasks []domain.ReleaseTaskRef) []domain.ReleaseTaskRef {
	var out []domain.ReleaseTaskRef
	for _, t := range tasks {
		if t.BeforeDeployPending() {
			out = append(out, t)
		}
	}
	return out
}

func pendingBeforeDeployMessage(pending []domain.ReleaseTaskRef) string {
	var sb strings.Builder
	sb.WriteString(prompt.Text(beforeDeployPendingIntroKey))
	for _, t := range pending {
		fmt.Fprintf(&sb, "\n- %s: %s", taskLabel(t), domain.StripOrderNote(t.BeforeDeploy))
	}
	return sb.String()
}

func (s *Service) commentPendingBeforeDeploy(ctx context.Context, r domain.Release, pending []domain.ReleaseTaskRef) {
	if s.tasks == nil || len(r.Tasks) == 0 {
		return
	}
	newest := r.Tasks[len(r.Tasks)-1]
	content := beforeDeployPendingDeployCommentKey.Render(releaseMessageInput{Message: pendingBeforeDeployMessage(pending)})
	if _, err := s.tasks.AddComment(ctx, r.RepositoryID, newest.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    content,
	}); err != nil {
		log.Warn().Err(err).Str("task_id", newest.ID.String()).Msg("release: posting the pending before-deploy comment failed")
	}
}

// stampBeforeDeployConfirmations confirms every task's before-deploy steps
// the way a batch Cut does: cutting the release IS the human's confirmation,
// so every task that carries steps is stamped, confirmed or not yet.
func (s *Service) stampBeforeDeployConfirmations(ctx context.Context, r domain.Release) {
	if s.beforeDeploy == nil {
		return
	}
	for _, t := range r.Tasks {
		if strings.TrimSpace(t.BeforeDeploy) == "" {
			continue
		}
		if err := s.beforeDeploy.ConfirmBeforeDeploy(ctx, r.RepositoryID, t.ID); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: confirming a before-deploy step at cut failed")
		}
	}
}
