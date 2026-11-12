package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type memWakes struct {
	mu    sync.Mutex
	items []domain.SessionWake
}

func (m *memWakes) Add(_ context.Context, w domain.SessionWake) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.ID = uuid.New()
	m.items = append(m.items, w)
	return nil
}
func (m *memWakes) ListOpen(context.Context) ([]domain.SessionWake, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.SessionWake(nil), m.items...), nil
}
func (m *memWakes) ListBySession(_ context.Context, id uuid.UUID) ([]domain.SessionWake, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.SessionWake
	for _, w := range m.items {
		if w.SessionID == id {
			out = append(out, w)
		}
	}
	return out, nil
}
func (m *memWakes) Delete(_ context.Context, id uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, w := range m.items {
		if w.ID == id {
			m.items = append(m.items[:i], m.items[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (m *memWakes) DeleteBySession(_ context.Context, id uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	kept := m.items[:0]
	for _, w := range m.items {
		if w.SessionID == id {
			n++
			continue
		}
		kept = append(kept, w)
	}
	m.items = kept
	return n, nil
}
func (m *memWakes) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}

type wakeSessionStore struct {
	port.SessionStore
	appended chan string
}

func (s *wakeSessionStore) AppendMessage(_ context.Context, _ uuid.UUID, _ domain.Role, content string, _, _ []byte) (domain.SessionMessage, error) {
	s.appended <- content
	return domain.SessionMessage{}, nil
}

func (s *wakeSessionStore) Get(context.Context, uuid.UUID) (domain.Session, error) {
	return domain.Session{}, errors.New("stop here")
}

func newWakeService(t *testing.T, state WakeState) (*Service, *memWakes, *wakeSessionStore) {
	t.Helper()
	store := &wakeSessionStore{appended: make(chan string, 4)}
	svc := &Service{store: store}
	wakes := &memWakes{}
	svc.SetWakes(wakes, map[string]WakeChecker{
		domain.WakeKindAudienceBuild: func(context.Context, uuid.UUID) (WakeState, error) { return state, nil },
	})
	return svc, wakes, store
}

func registerWake(svc *Service, sessionID uuid.UUID) {
	ctx := registry.ContextWithSessionID(context.Background(), sessionID)
	svc.WakeSessionOn(ctx, domain.WakeKindAudienceBuild, uuid.New(), `audience_build "Acme"`)
}

func TestWakeSweepRerunsTheChatWhenTheWatchedWorkEnded(t *testing.T) {
	svc, wakes, store := newWakeService(t, WakeState{Done: true, Status: "ready"})
	registerWake(svc, uuid.New())

	NewWakeSweeper(svc).sweep(context.Background())

	select {
	case content := <-store.appended:
		assert.Contains(t, content, WakeMessagePrefix)
		assert.Contains(t, content, `audience_build "Acme"`)
		assert.Contains(t, content, "ready")
	case <-time.After(2 * time.Second):
		t.Fatal("no background update was appended")
	}
	assert.Equal(t, 0, wakes.count())
}

func TestWakeSweepLeavesRunningWorkAlone(t *testing.T) {
	svc, wakes, store := newWakeService(t, WakeState{Status: "building"})
	registerWake(svc, uuid.New())

	NewWakeSweeper(svc).sweep(context.Background())

	assert.Equal(t, 1, wakes.count())
	assert.Empty(t, store.appended)
}

func TestWakeSweepGivesUpWhenTheWaitRanOut(t *testing.T) {
	svc, wakes, store := newWakeService(t, WakeState{Status: "building"})
	registerWake(svc, uuid.New())
	sweeper := NewWakeSweeper(svc)
	sweeper.now = func() time.Time { return time.Now().Add(WakeTimeout + time.Minute) }

	sweeper.sweep(context.Background())

	content := <-store.appended
	assert.Contains(t, content, "still not finished")
	assert.Equal(t, 0, wakes.count())
}

func TestWakeSweepWaitsForABusyChat(t *testing.T) {
	svc, wakes, store := newWakeService(t, WakeState{Done: true, Status: "ready"})
	sessionID := uuid.New()
	registerWake(svc, sessionID)
	_, deregister := svc.registerRun(sessionID, uuid.New(), func() {})
	defer deregister()

	NewWakeSweeper(svc).sweep(context.Background())

	assert.Equal(t, 1, wakes.count(), "a turn in flight answers first; the wake fires on a later pass")
	assert.Empty(t, store.appended)
}

func TestStopWaitingDropsTheWakesSoTheChatIsNotRerun(t *testing.T) {
	svc, wakes, store := newWakeService(t, WakeState{Done: true, Status: "ready"})
	sessionID := uuid.New()
	registerWake(svc, sessionID)
	registerWake(svc, uuid.New())

	n, err := svc.StopWaiting(context.Background(), sessionID, uuid.Nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, wakes.count(), "only this chat's waits are dropped")

	NewWakeSweeper(svc).sweep(context.Background())
	<-store.appended
}

func TestWakeSessionOnIgnoresACallOutsideAChat(t *testing.T) {
	svc, wakes, _ := newWakeService(t, WakeState{})
	svc.WakeSessionOn(context.Background(), domain.WakeKindAudienceBuild, uuid.New(), "x")
	assert.Equal(t, 0, wakes.count())
}
