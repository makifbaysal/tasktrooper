package session

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	WakeSweeperInterval = 5 * time.Second
	WakeTimeout         = 2 * time.Hour
	WakeMessagePrefix   = "**Background update:**"
)

type WakeState struct {
	Done   bool
	Status string
	Detail string
}

type WakeChecker func(ctx context.Context, refID uuid.UUID) (WakeState, error)

type wakeItem struct {
	Label    string
	Status   string
	Detail   string
	TimedOut bool
}

type wakeUpdateInput struct{ Items []wakeItem }

var wakeUpdateKey = prompt.Define("guard.session_wake_update", wakeUpdateInput{Items: []wakeItem{{Label: "audience_build \"x\"", Status: "ready"}}})

type wakeState struct {
	store    port.SessionWakeStore
	checkers map[string]WakeChecker
	mu       sync.Mutex
	firing   map[uuid.UUID]bool
}

func (s *Service) SetWakes(store port.SessionWakeStore, checkers map[string]WakeChecker) {
	s.wakes = &wakeState{store: store, checkers: checkers, firing: map[uuid.UUID]bool{}}
}

// WakeSessionOn makes the calling chat turn's session run again once the
// watched thing ended. Outside a chat turn it does nothing.
func (s *Service) WakeSessionOn(ctx context.Context, kind string, refID uuid.UUID, label string) {
	sessionID := registry.SessionIDFromContext(ctx)
	if s == nil || s.wakes == nil || sessionID == uuid.Nil {
		return
	}
	err := s.wakes.store.Add(context.WithoutCancel(ctx), domain.SessionWake{
		SessionID: sessionID, Kind: kind, RefID: refID, Label: label, ExpiresAt: time.Now().Add(WakeTimeout),
	})
	if err != nil {
		log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("session wake: could not register")
	}
}

func (s *Service) ListWakes(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionWake, error) {
	if s.wakes == nil {
		return nil, nil
	}
	return s.wakes.store.ListBySession(ctx, sessionID)
}

// StopWaiting drops the session's pending wakes (all of them, or one) so the
// agent is not rerun for them.
func (s *Service) StopWaiting(ctx context.Context, sessionID, wakeID uuid.UUID) (int, error) {
	if s.wakes == nil {
		return 0, nil
	}
	if wakeID == uuid.Nil {
		return s.wakes.store.DeleteBySession(ctx, sessionID)
	}
	list, err := s.wakes.store.ListBySession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	for _, w := range list {
		if w.ID == wakeID {
			ok, err := s.wakes.store.Delete(ctx, wakeID)
			if ok {
				return 1, err
			}
			return 0, err
		}
	}
	return 0, nil
}

type WakeSweeper struct {
	svc *Service
	now func() time.Time
}

func NewWakeSweeper(svc *Service) *WakeSweeper { return &WakeSweeper{svc: svc, now: time.Now} }

func (w *WakeSweeper) Start(ctx context.Context, interval time.Duration) {
	if w == nil || w.svc == nil || w.svc.wakes == nil {
		return
	}
	if interval <= 0 {
		interval = WakeSweeperInterval
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.sweep(ctx)
			}
		}
	}()
}

type endedWake struct {
	wake domain.SessionWake
	item wakeItem
}

func (w *WakeSweeper) sweep(ctx context.Context) {
	ws := w.svc.wakes
	open, err := ws.store.ListOpen(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("session wake sweeper: listing wakes failed")
		return
	}
	now := w.now()
	bySession := map[uuid.UUID][]endedWake{}
	for _, wk := range open {
		check := ws.checkers[wk.Kind]
		if check == nil {
			continue
		}
		st, err := check(ctx, wk.RefID)
		if err != nil {
			st = WakeState{Status: "unknown", Detail: err.Error()}
		}
		timedOut := !st.Done && !now.Before(wk.ExpiresAt)
		if !st.Done && !timedOut {
			continue
		}
		bySession[wk.SessionID] = append(bySession[wk.SessionID], endedWake{wk, wakeItem{Label: wk.Label, Status: st.Status, Detail: st.Detail, TimedOut: timedOut}})
	}
	for sessionID, ended := range bySession {
		w.fire(ctx, sessionID, ended)
	}
}

func (w *WakeSweeper) fire(ctx context.Context, sessionID uuid.UUID, ended []endedWake) {
	s := w.svc
	ws := s.wakes
	ws.mu.Lock()
	busy := ws.firing[sessionID]
	if !busy {
		s.runsMu.Lock()
		busy = len(s.sessionRuns[sessionID]) > 0
		s.runsMu.Unlock()
	}
	if busy {
		ws.mu.Unlock()
		return
	}
	ws.firing[sessionID] = true
	ws.mu.Unlock()

	var items []wakeItem
	for _, e := range ended {
		if ok, err := ws.store.Delete(ctx, e.wake.ID); err != nil || !ok {
			continue
		}
		items = append(items, e.item)
	}
	if len(items) == 0 {
		ws.mu.Lock()
		delete(ws.firing, sessionID)
		ws.mu.Unlock()
		return
	}
	go func() {
		defer func() {
			ws.mu.Lock()
			delete(ws.firing, sessionID)
			ws.mu.Unlock()
		}()
		runCtx := context.WithoutCancel(ctx)
		content := wakeUpdateKey.Render(wakeUpdateInput{Items: items})
		if _, err := s.store.AppendMessage(runCtx, sessionID, domain.RoleUser, content, nil, nil); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("session wake: appending the update failed")
			return
		}
		req := domain.SessionMessageRequest{Role: domain.RoleUser, Content: content, NoQuotaPark: true}
		if err := s.resumeParkedTurn(runCtx, domain.PendingSessionTurn{SessionID: sessionID, Request: req}); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("session wake: the follow-up turn failed")
		}
	}()
}
