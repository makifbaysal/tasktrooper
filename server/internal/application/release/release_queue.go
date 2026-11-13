package release

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Deploys of one component go out one at a time: while a release of it is
// deploying, soaking or waiting for its verdict, the next task's merge waits
// on a release_in_flight hold instead of superseding it. When the release is
// released or rolled back, the oldest held task is woken and merges; the rest
// wait for the release that one opens. A problem in production is then
// always one change's, never a pile of superseded ones.
//
// The woken task keeps its hold until its merge passes the gate, and the gate
// lets only the oldest held task of a component through, so a second wake (the
// sweeper's safety net) can never let two merges race past an empty slot.
//
// Batch components are left alone (a batch ships many tasks on purpose), as
// are components with no delivery (nothing deploys).

// queueReWakeInterval bounds how often the same queued task is woken again
// while the agent that was woken for it has not merged yet.
const queueReWakeInterval = 10 * time.Minute

var inFlightStatuses = []domain.ReleaseStatus{
	domain.ReleasePending, domain.ReleaseDeploying, domain.ReleaseVerifying,
	domain.ReleaseAwaitingVerdict, domain.ReleaseRollingBack, domain.ReleaseFailed,
}

func queuesMerges(mode domain.DeliveryMode) bool {
	return mode == domain.DeliveryOnMerge || mode == domain.DeliveryDispatch
}

func (s *Service) releaseInFlightGate(ctx context.Context, repositoryID uuid.UUID, component domain.Component, profile domain.ComponentDelivery, name string, task domain.BoardTask) (domain.ResourceBlock, error) {
	if !queuesMerges(profile.Mode) {
		return domain.ResourceBlock{}, nil
	}
	var label, status string
	if r, ok := s.releaseInFlight(ctx, repositoryID, component.ID); ok && !slices.Contains(r.TaskIDs(), task.ID) {
		label, status = r.Version, string(r.Status)
		if len(r.Tasks) > 0 {
			label = taskLabel(r.Tasks[len(r.Tasks)-1]) + " (" + r.Version + ")"
		}
	} else if ahead, ok := s.queuedAhead(ctx, component.ID, task); ok {
		label, status = taskLabel(domain.ReleaseTaskRef{ID: ahead.ID, Key: ahead.Key, Title: ahead.Title}), "waiting to merge ahead of it"
	} else {
		return domain.ResourceBlock{}, nil
	}
	in := releaseInFlightInput{Name: name, Release: label, Status: status}
	s.commentOnce(ctx, repositoryID, task.ID, releaseInFlightCommentKey.Render(in))
	return domain.ResourceBlock{Resource: domain.ResourceReleaseInFlight, Detail: label},
		fmt.Errorf("%w: %s", domain.ErrReleaseInFlight, releaseInFlightKey.Render(in))
}

// releaseInFlight finds the component's release that has not reached a
// verdict yet. A store that cannot be read counts as nothing in flight: the
// merge then behaves as it did before the queue existed (it supersedes), which
// is safe, where refusing every merge on a read error would not be.
func (s *Service) releaseInFlight(ctx context.Context, repositoryID, componentID uuid.UUID) (domain.Release, bool) {
	if s.store == nil {
		return domain.Release{}, false
	}
	releases, err := s.store.List(ctx, domain.ReleaseListFilter{
		RepositoryID: &repositoryID,
		ComponentID:  &componentID,
		Statuses:     inFlightStatuses,
		Limit:        1,
	})
	if err != nil {
		log.Warn().Err(err).Str("component_id", componentID.String()).Msg("release: checking for a release in flight failed")
		return domain.Release{}, false
	}
	if len(releases) == 0 {
		return domain.Release{}, false
	}
	return releases[0], true
}

// queuedAhead returns the oldest other task of the component still held on
// release_in_flight in done, when it was queued before task.
func (s *Service) queuedAhead(ctx context.Context, componentID uuid.UUID, task domain.BoardTask) (domain.BoardTask, bool) {
	for _, held := range s.releaseQueue(ctx) {
		if held.ID == task.ID {
			return domain.BoardTask{}, false
		}
		// The queue is sorted oldest first and stops at task itself, so every
		// held task seen here was queued before it.
		if c, _, ok := s.resolveComponent(ctx, held.RepositoryID, held); ok && c.ID == componentID {
			return held, true
		}
	}
	return domain.BoardTask{}, false
}

// releaseQueue lists the tasks held on release_in_flight that are still in
// done, oldest hold first. A held task that left done (a human moved it) has
// its hold cleared here — it no longer waits to merge.
func (s *Service) releaseQueue(ctx context.Context) []domain.BoardTask {
	if s.parked == nil {
		return nil
	}
	held, err := s.parked.ListBlockedByResource(ctx, domain.ResourceReleaseInFlight, sweepBatch)
	if err != nil {
		log.Warn().Err(err).Msg("release: listing tasks queued behind a release failed")
		return nil
	}
	out := held[:0]
	for _, t := range held {
		if t.Column != domain.TaskColumnDone {
			s.releaseMergeHold(ctx, t, domain.ResourceReleaseInFlight)
			continue
		}
		out = append(out, t)
	}
	slices.SortStableFunc(out, func(a, b domain.BoardTask) int {
		return blockedAt(a).Compare(blockedAt(b))
	})
	return out
}

// wakeReleaseQueue wakes the head of each component's queue once nothing of
// that component is in flight. It runs right after a release is released or
// rolled back, and on every sweep as the safety net: a woken task that then
// stops on another hold no longer holds release_in_flight, so the next sweep
// wakes the task after it instead of the queue stalling behind it.
func (s *Service) wakeReleaseQueue(ctx context.Context) {
	if s.waker == nil {
		return
	}
	handled := map[uuid.UUID]bool{}
	for _, task := range s.releaseQueue(ctx) {
		component, _, ok := s.resolveComponent(ctx, task.RepositoryID, task)
		if !ok {
			s.releaseMergeHold(ctx, task, domain.ResourceReleaseInFlight)
			continue
		}
		if handled[component.ID] {
			continue
		}
		handled[component.ID] = true
		if _, inFlight := s.releaseInFlight(ctx, task.RepositoryID, component.ID); inFlight {
			continue
		}
		if !s.claimQueueWake(task.ID) {
			continue
		}
		if err := s.WakeTask(ctx, task.RepositoryID, task); err != nil {
			log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release: waking the next queued merge failed")
		}
	}
}

func (s *Service) claimQueueWake(taskID uuid.UUID) bool {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	now := s.now()
	if last, ok := s.queueWokenAt[taskID]; ok && now.Sub(last) < queueReWakeInterval {
		return false
	}
	if s.queueWokenAt == nil {
		s.queueWokenAt = map[uuid.UUID]time.Time{}
	}
	for id, at := range s.queueWokenAt {
		if now.Sub(at) >= queueReWakeInterval {
			delete(s.queueWokenAt, id)
		}
	}
	s.queueWokenAt[taskID] = now
	return true
}

func blockedAt(t domain.BoardTask) time.Time {
	if t.BlockedAt != nil {
		return *t.BlockedAt
	}
	return t.CreatedAt
}
