package release

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// watchNotingStore notes every write that leaves a release Watched(), so
// whichever path starts a watch (a deploy, a local or store batch, a rollback,
// a provider rollback) brings the sweeper back to its active cadence without
// each of them having to remember to.
type watchNotingStore struct {
	port.ReleaseStore
	note func()
}

func (w watchNotingStore) Create(ctx context.Context, r domain.Release, taskIDs []uuid.UUID) (domain.Release, error) {
	created, err := w.ReleaseStore.Create(ctx, r, taskIDs)
	if err == nil && created.Status.Watched() {
		w.note()
	}
	return created, err
}

func (w watchNotingStore) Update(ctx context.Context, r domain.Release, expect domain.ReleaseStatus) (domain.Release, error) {
	updated, err := w.ReleaseStore.Update(ctx, r, expect)
	if err == nil && updated.Status.Watched() {
		w.note()
	}
	return updated, err
}

func (s *Service) noteWatching() {
	s.cadenceMu.Lock()
	s.watchNotes++
	wasIdle := !s.watching
	s.watching = true
	s.cadenceMu.Unlock()
	if wasIdle {
		select {
		case s.watchKick <- struct{}{}:
		default:
		}
	}
}

func (s *Service) watchNoteCount() uint64 {
	s.cadenceMu.Lock()
	defer s.cadenceMu.Unlock()
	return s.watchNotes
}

// settleCadence goes idle only when the sweep found nothing to watch AND no
// watch started while it ran: a release that entered a watched status after
// the sweep listed must not wait out the idle interval.
func (s *Service) settleCadence(notesBefore uint64, watched bool) {
	s.cadenceMu.Lock()
	defer s.cadenceMu.Unlock()
	if watched {
		s.watching = true
		return
	}
	if s.watchNotes == notesBefore {
		s.watching = false
	}
}

func (s *Service) watchActive() bool {
	s.cadenceMu.Lock()
	defer s.cadenceMu.Unlock()
	return s.watching
}

func (s *Service) nextSweepIn(active time.Duration) time.Duration {
	if s.watchActive() || active >= IdleSweepInterval {
		return active
	}
	return IdleSweepInterval
}
