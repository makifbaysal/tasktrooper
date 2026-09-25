package release

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// handBack wakes the release engineer once a release no longer needs the
// sweeper: the first parked card of the release is claimed and woken, every
// other parked card of the same release is claimed silently, and if none was
// parked at all (the agent run that would have parked is gone) the newest
// task is woken instead — it is still sitting in done.
func (s *Service) handBack(ctx context.Context, r domain.Release) {
	if len(r.Tasks) == 0 {
		return
	}
	woke := false
	if s.parked != nil {
		for _, t := range r.Tasks {
			task, ok, err := s.parked.TakeBlockedResourceTask(ctx, domain.ResourceReleaseWatch, t.ID)
			if err != nil {
				log.Warn().Err(err).Str("task_id", t.ID.String()).Msg("release: claiming a parked card failed")
				continue
			}
			if !ok {
				continue
			}
			if !woke {
				s.wake(ctx, r, task)
				woke = true
			}
		}
	}
	if woke {
		return
	}

	newest := r.Tasks[len(r.Tasks)-1]
	if s.tasks == nil {
		return
	}
	task, err := s.tasks.GetTask(ctx, r.RepositoryID, newest.ID)
	if err != nil {
		log.Warn().Err(err).Str("task_id", newest.ID.String()).Msg("release: loading the newest task to hand back failed")
		return
	}
	s.wake(ctx, r, task)
}

func (s *Service) wake(ctx context.Context, r domain.Release, task domain.BoardTask) {
	if s.waker == nil {
		return
	}
	if err := s.waker.Wake(ctx, r.RepositoryID, task, r.Status); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("release_id", r.ID.String()).
			Msg("release: waking the release engineer failed")
	}
}
