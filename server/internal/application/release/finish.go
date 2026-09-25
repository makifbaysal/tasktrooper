package release

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Finish records the release engineer's (or a human's) verdict that a
// release is good and moves every one of its tasks to released.
// awaiting_verdict is open to either actor; failed only to a human
// ("ship it anyway" overrides the machine's own refusal).
func (s *Service) Finish(ctx context.Context, releaseID uuid.UUID, actor domain.ReleaseActor, note string) (domain.Release, error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	switch {
	case r.Status == domain.ReleaseAwaitingVerdict:
	case r.Status == domain.ReleaseFailed && actor == domain.ReleaseActorHuman:
	default:
		return domain.Release{}, fmt.Errorf(
			"%w: finish_release only applies to a release awaiting a verdict, or a failed one a human confirms anyway (this one is %s, actor %s)",
			domain.ErrReleaseWrongStatus, r.Status, actor)
	}

	expect := r.Status
	now := s.now()
	r.Status = domain.ReleaseReleased
	r.Verdict = note
	r.FinishedAt = &now
	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		return domain.Release{}, err
	}
	s.releaseTasks(ctx, updated)
	return updated, nil
}

func (s *Service) releaseTasks(ctx context.Context, r domain.Release) {
	if s.tasks == nil {
		return
	}
	for _, t := range r.Tasks {
		if s.parked != nil {
			if _, _, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID); err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: claiming a parked card before releasing it failed")
			}
		}
		col := domain.TaskColumnReleased
		if _, err := s.tasks.UpdateTask(ctx, r.RepositoryID, t.ID, domain.UpdateBoardTaskRequest{
			Column:       &col,
			SystemReason: domain.MoveReasonReleaseVerified,
		}); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: moving a finished release's task to released failed")
		}
	}
}
