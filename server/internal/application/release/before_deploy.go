package release

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MergeGate refuses a merge whose task still has pending before-deploy steps
// on an on_merge component — the merge itself is what would deploy them, and
// a human has to perform those steps first. It resolves the component the
// same way OpenForMerge would; an unresolved or unconfirmed component is not
// refused here — the merge goes through and the task simply waits in done
// the way OpenForMerge already handles that case.
func (s *Service) MergeGate(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) error {
	component, name, ok := s.resolveComponent(ctx, repositoryID, task)
	if !ok {
		return nil
	}
	profile, confirmed := domain.DeliveryConfirmed(component.Delivery)
	if !confirmed || profile.Mode != domain.DeliveryOnMerge {
		return nil
	}
	if pending := s.pendingDeployDependencies(ctx, []uuid.UUID{task.ID}); len(pending) > 0 {
		s.commentDeployDependencies(ctx, repositoryID, task.ID, pending)
		return fmt.Errorf("%s deploys on merge, so the merge waits: %w", name, deployDependencyError(pending))
	}
	if !task.BeforeDeployPending() {
		return nil
	}
	steps := strings.TrimSpace(*task.BeforeDeploy)
	s.commentOnce(ctx, repositoryID, task.ID, "Waiting to merge: "+name+" deploys on merge, and this task has before-deploy steps "+
		"a human must perform first. Do them, then press \"Confirm before-deploy steps\" on the task:\n\n"+steps)
	return fmt.Errorf("%w: %s deploys on merge, and this task's before-deploy steps are not confirmed — "+
		"a human must perform them and press \"Confirm before-deploy steps\" on the task before it can merge. "+
		"Nothing was merged. Do not retry — you will be woken when a human confirms:\n\n%s",
		domain.ErrBeforeDeployPending, name, steps)
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
	sb.WriteString("a human must perform these before-deploy steps and press \"Confirm before-deploy steps\" " +
		"on each task before this release can deploy")
	for _, t := range pending {
		fmt.Fprintf(&sb, "\n- %s: %s", taskLabel(t), strings.TrimSpace(t.BeforeDeploy))
	}
	return sb.String()
}

func (s *Service) commentPendingBeforeDeploy(ctx context.Context, r domain.Release, pending []domain.ReleaseTaskRef) {
	if s.tasks == nil || len(r.Tasks) == 0 {
		return
	}
	newest := r.Tasks[len(r.Tasks)-1]
	content := "Deploy refused: " + pendingBeforeDeployMessage(pending) +
		"\n\nNothing was deployed. Do not retry — you will be woken when a human confirms."
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
