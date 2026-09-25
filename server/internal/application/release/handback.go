package release

import (
	"context"
	"errors"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// handBack wakes the release engineer once a release no longer needs the
// sweeper: the first parked card of the release is claimed and woken, every
// other parked card of the same release is claimed silently, and if none was
// parked at all (the agent run that would have parked is gone) the newest
// task still in done or released is woken instead — a task a human
// already moved elsewhere (need_revision, blocked) is left alone; that move
// is the human's decision, not something a stale release verdict should
// override. Every call stamps the persisted hand-back bookkeeping first (N5),
// whether or not a card was actually parked to claim — the watchdog's
// cooldown/cap is about "how many times has this been handed back", not
// "how many times did a park exist to claim".
func (s *Service) handBack(ctx context.Context, r domain.Release) {
	if len(r.Tasks) == 0 {
		return
	}
	r = s.stampHandBack(ctx, r)
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

	if s.tasks == nil {
		return
	}
	for i := len(r.Tasks) - 1; i >= 0; i-- {
		ref := r.Tasks[i]
		task, err := s.tasks.GetTask(ctx, r.RepositoryID, ref.ID)
		if err != nil {
			log.Warn().Err(err).Str("task_id", ref.ID.String()).Msg("release: loading a task to hand back failed")
			continue
		}
		if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
			continue
		}
		s.wake(ctx, r, task)
		return
	}
}

// stampHandBack persists that a hand-back to the agent was attempted —
// HandBackCount/LastHandBackAt on the row (N5), not process memory, so the
// watchdog's cooldown and cap survive a desktop relaunch and are shared by
// every sweeper instance. A lost race (something else moved the release
// meanwhile) is not retried: the release will be picked up fresh on the next
// sweep tick, and this caller's own view of it is already stale.
func (s *Service) stampHandBack(ctx context.Context, r domain.Release) domain.Release {
	expect := r.Status
	r.HandBackCount++
	now := s.now()
	r.LastHandBackAt = &now
	updated, err := s.store.Update(ctx, r, expect)
	if err != nil {
		if !errors.Is(err, domain.ErrReleaseWrongStatus) {
			log.Warn().Err(err).Str("release_id", r.ID.String()).Msg("release: stamping a hand-back failed")
		}
		return r
	}
	return updated
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
