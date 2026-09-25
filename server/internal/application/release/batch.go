package release

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// openBatch adds the task to the component's draft release, creating it if
// none exists, via joinDraftRelease.
func (s *Service) openBatch(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask, component domain.Component, name string, profile domain.ComponentDelivery) domain.ReleaseOpening {
	draft, err := s.joinDraftRelease(ctx, repositoryID, component.ID, task, profile, true)
	if err != nil {
		log.Warn().Err(err).Str("component_id", component.ID.String()).Msg("release: opening the draft release failed")
		return domain.ReleaseOpening{Mode: domain.DeliveryBatch, Next: fmt.Sprintf("could not open a release: %s", err)}
	}

	id := draft.ID
	return domain.ReleaseOpening{
		Mode:      domain.DeliveryBatch,
		ReleaseID: &id,
		Status:    domain.ReleaseDraft,
		Next:      fmt.Sprintf("Queued in the next release of %s. A human cuts it on the Deploy tab; nothing to do now.", name),
	}
}

// joinDraftRelease finds (or, on a race, creates) the component's draft
// release and adds task to it with AddTasksToDraft, which only succeeds
// while the release is still a draft. Two merges can race to create the
// draft: idx_releases_one_draft makes the loser's Create fail, so the loser
// re-reads the draft the winner just created and joins it. A false result
// from AddTasksToDraft means the draft was cut or superseded between the
// find and the add (including a racing winner's draft that got cut just as
// fast); allowRetry re-finds/creates once more and tries again — a second
// loss is reported rather than looping forever.
func (s *Service) joinDraftRelease(ctx context.Context, repositoryID, componentID uuid.UUID, task domain.BoardTask, profile domain.ComponentDelivery, allowRetry bool) (domain.Release, error) {
	draft, err := s.findDraftRelease(ctx, repositoryID, componentID)
	switch {
	case err == nil:
		ok, addErr := s.store.AddTasksToDraft(ctx, draft.ID, []uuid.UUID{task.ID})
		if addErr != nil {
			return domain.Release{}, addErr
		}
		if ok {
			return draft, nil
		}
		if !allowRetry {
			return domain.Release{}, fmt.Errorf("the draft release was cut before this task could be queued")
		}
		return s.joinDraftRelease(ctx, repositoryID, componentID, task, profile, false)
	case errors.Is(err, domain.ErrReleaseNotFound):
		now := s.now()
		r := domain.Release{
			RepositoryID: repositoryID,
			ComponentID:  &componentID,
			Version:      "",
			Mode:         domain.DeliveryBatch,
			Executor:     profile.Executor,
			Status:       domain.ReleaseDraft,
			Profile:      profile,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		created, cerr := s.store.Create(ctx, r, []uuid.UUID{task.ID})
		if cerr == nil {
			return created, nil
		}
		if !allowRetry {
			return domain.Release{}, cerr
		}
		return s.joinDraftRelease(ctx, repositoryID, componentID, task, profile, false)
	default:
		return domain.Release{}, err
	}
}

// deployBatch dispatches a cut batch release's deploy to whichever executor
// its frozen profile names; the three implementations live in deploy.go
// (github_actions, alongside dispatch's own tagging), local.go and store.go.
func (s *Service) deployBatch(ctx context.Context, r domain.Release, actor domain.ReleaseActor) (domain.Release, error) {
	_ = actor
	switch r.Executor {
	case domain.ExecutorGitHubActions:
		return s.deployBatchGitHubActions(ctx, r)
	case domain.ExecutorLocal:
		return s.deployBatchLocal(ctx, r)
	case domain.ExecutorStore:
		return s.deployBatchStore(ctx, r)
	default:
		return domain.Release{}, fmt.Errorf("%w: unknown batch executor %q", domain.ErrReleaseNoDeploy, r.Executor)
	}
}

func (s *Service) findDraftRelease(ctx context.Context, repositoryID, componentID uuid.UUID) (domain.Release, error) {
	if s.store == nil {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &repositoryID,
		ComponentID:  &componentID,
		Statuses:     []domain.ReleaseStatus{domain.ReleaseDraft},
		Limit:        1,
	})
	if err != nil {
		return domain.Release{}, err
	}
	if len(releases) == 0 {
		return domain.Release{}, domain.ErrReleaseNotFound
	}
	return releases[0], nil
}

// wakeNewestTask wakes the release engineer on the newest task of a release
// that is still in done — a cut batch release has never had an agent run
// watching it, so there is nothing to un-park, unlike handBack's use of the
// same fallback. A task a human already moved elsewhere (need_revision, a
// re-merge under way, …) is skipped: waking it there would surface a release
// card on a column nothing about it expects.
func (s *Service) wakeNewestTask(ctx context.Context, r domain.Release, status domain.ReleaseStatus) {
	if s.waker == nil || s.tasks == nil {
		return
	}
	for i := len(r.Tasks) - 1; i >= 0; i-- {
		ref := r.Tasks[i]
		if ref.Column != domain.TaskColumnDone {
			continue
		}
		task, err := s.tasks.GetTask(ctx, r.RepositoryID, ref.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", ref.ID.String()).Msg("release: loading the newest task to wake after a cut failed")
			return
		}
		if err := s.waker.Wake(ctx, r.RepositoryID, task, status); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Str("release_id", r.ID.String()).
				Msg("release: waking the release engineer for a cut release failed")
		}
		return
	}
	log.Warn().Str("release_id", r.ID.String()).Msg("release: no task in done to wake after a cut")
}

// finishBatchRollback is Rollback's batch path: the revert already landed (the
// caller does that before reaching here), and unlike dispatch/on_merge modes
// nothing is redeployed — a published desktop build or a store build cannot be
// unpublished by a revert — so the release goes straight to rolled_back
// instead of rolling_back.
func (s *Service) finishBatchRollback(ctx context.Context, r domain.Release, expect domain.ReleaseStatus, rollback *domain.ReleaseRollback) (domain.Release, error) {
	rollback.Mechanism = domain.RollbackMechanismRevert
	rollback.RestoredRef = rollback.RevertSHA
	rollback.Detail = "nothing was redeployed — a published desktop build or a store build cannot be unpublished by a revert"
	rollback.ManualSteps = append(batchRollbackManualSteps(r), rollback.ManualSteps...)

	r.Rollback = rollback
	r.Status = domain.ReleaseRolledBack
	now := s.now()
	r.FinishedAt = &now
	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	s.reopenTasks(ctx, updated)
	return updated, nil
}

// batchRollbackManualSteps names what the revert itself cannot undo: a
// github_actions/local batch already published artifacts at the tag, a store
// batch already has (or is building) a store submission.
func batchRollbackManualSteps(r domain.Release) []string {
	switch r.Executor {
	case domain.ExecutorGitHubActions, domain.ExecutorLocal:
		tag := r.Tag
		if tag == "" {
			tag = r.Version
		}
		return []string{fmt.Sprintf(
			"Unpublish or mark as broken the artifacts published for %s (the GitHub Release, any package/cask/update feed that points at it) — the revert only fixes the default branch.",
			tag)}
	case domain.ExecutorStore:
		var steps []string
		for _, b := range r.StoreBuilds {
			if b.Build == "" {
				continue
			}
			steps = append(steps, fmt.Sprintf(
				"Halt or stop the %s rollout of build %s in the store (Operations → Apps) — the revert does not touch the store.",
				b.Platform, b.Build))
		}
		return steps
	}
	return nil
}
