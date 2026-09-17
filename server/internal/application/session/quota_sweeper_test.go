package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// stubSessionQuotaTaker stands in for the store's one atomic
// claim-and-unpark statement — SessionQuotaSweeper's twin of
// board.stubQuotaTaker.
type stubSessionQuotaTaker struct {
	pending []domain.PendingSessionTurn
	calls   int
	asked   []time.Time
	err     error
}

func (s *stubSessionQuotaTaker) TakePendingSessionTurn(_ context.Context, now time.Time) (domain.PendingSessionTurn, bool, error) {
	s.calls++
	s.asked = append(s.asked, now)
	if s.err != nil {
		return domain.PendingSessionTurn{}, false, s.err
	}
	if len(s.pending) == 0 {
		return domain.PendingSessionTurn{}, false, nil
	}
	next := s.pending[0]
	s.pending = s.pending[1:]
	return next, true, nil
}

// erroringSessionStore fails Get, which is as far as resumeParkedTurn needs
// to go for these tests: they are about the sweep LOOP's claim/cap/pass
// behaviour, not about a resumed run's outcome (covered separately by
// TestParkTurnOnQuota* and by SendMessage's own, already-passing tests).
type erroringSessionStore struct {
	port.SessionStore
}

func (erroringSessionStore) Get(context.Context, uuid.UUID) (domain.Session, error) {
	return domain.Session{}, errors.New("session store unavailable")
}

func pendingTurn() domain.PendingSessionTurn {
	return domain.PendingSessionTurn{SessionID: uuid.New(), Request: domain.SessionMessageRequest{Role: domain.RoleUser, Content: "go on"}}
}

func TestSessionQuotaSweepDoesNothingWhenNothingIsDue(t *testing.T) {
	taker := &stubSessionQuotaTaker{}
	s := NewSessionQuotaSweeper(taker, &Service{store: erroringSessionStore{}})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls, "the store is asked once per pass")
}

func TestSessionQuotaSweepDrainsEveryDueTurnInOnePass(t *testing.T) {
	taker := &stubSessionQuotaTaker{pending: []domain.PendingSessionTurn{pendingTurn(), pendingTurn(), pendingTurn()}}
	s := NewSessionQuotaSweeper(taker, &Service{store: erroringSessionStore{}})

	s.sweep(context.Background())

	assert.Empty(t, taker.pending, "no due turn may be left waiting a whole interval behind another")
	assert.Equal(t, 4, taker.calls, "three claims plus the empty one that ends the pass")
}

func TestSessionQuotaSweepStopsAtTheBatchCap(t *testing.T) {
	pending := make([]domain.PendingSessionTurn, quotaSweepBatchCap+5)
	for i := range pending {
		pending[i] = pendingTurn()
	}
	taker := &stubSessionQuotaTaker{pending: pending}
	s := NewSessionQuotaSweeper(taker, &Service{store: erroringSessionStore{}})

	s.sweep(context.Background())

	assert.Len(t, taker.pending, 5, "the cap leaves the remainder for the next pass, not the whole backlog")
	assert.Equal(t, quotaSweepBatchCap, taker.calls)
}

func TestSessionQuotaSweepStopsThePassOnAClaimError(t *testing.T) {
	taker := &stubSessionQuotaTaker{err: errors.New("db down")}
	s := NewSessionQuotaSweeper(taker, &Service{store: erroringSessionStore{}})

	s.sweep(context.Background())

	assert.Equal(t, 1, taker.calls, "a broken claim ends the pass rather than spinning on the same error")
}
