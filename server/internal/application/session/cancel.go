package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const persistTimeout = 10 * time.Second

func persistCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
}

type runHandle struct {
	sessionID uuid.UUID

	runID   uuid.UUID
	cancel  context.CancelFunc
	stopped atomic.Bool
}

func (h *runHandle) stop() bool {
	if !h.stopped.CompareAndSwap(false, true) {
		return false
	}
	h.cancel()
	return true
}

func (h *runHandle) userStopped() bool {
	return h != nil && h.stopped.Load()
}

func (s *Service) registerRun(sessionID, runID uuid.UUID, cancel context.CancelFunc) (*runHandle, func()) {

	key := runID
	if key == uuid.Nil {
		key = uuid.New()
	}
	handle := &runHandle{sessionID: sessionID, runID: runID, cancel: cancel}

	s.runsMu.Lock()
	if s.runs == nil {
		s.runs = make(map[uuid.UUID]*runHandle)
	}
	if s.sessionRuns == nil {
		s.sessionRuns = make(map[uuid.UUID]map[uuid.UUID]struct{})
	}
	s.runs[key] = handle
	if s.sessionRuns[sessionID] == nil {
		s.sessionRuns[sessionID] = make(map[uuid.UUID]struct{})
	}
	s.sessionRuns[sessionID][key] = struct{}{}
	s.runsMu.Unlock()

	stopWatch := s.watchRemoteCancel(handle)

	return handle, func() {
		stopWatch()
		s.runsMu.Lock()
		delete(s.runs, key)
		if keys := s.sessionRuns[sessionID]; keys != nil {
			delete(keys, key)
			if len(keys) == 0 {
				delete(s.sessionRuns, sessionID)
			}
		}
		s.runsMu.Unlock()
	}
}

const remoteCancelPoll = 5 * time.Second

func (s *Service) watchRemoteCancel(handle *runHandle) func() {
	if s.activityStore == nil || handle == nil || handle.runID == uuid.Nil {
		return func() {}
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		every := s.cancelPoll
		if every <= 0 {
			every = remoteCancelPoll
		}
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), remoteCancelPoll)
				status, err := s.activityStore.RunStatus(ctx, handle.runID)
				cancel()
				if err != nil {

					log.Warn().Err(err).Str("run_id", handle.runID.String()).
						Msg("chat turn cancel watch failed")
					continue
				}
				if status == domain.SessionRunStatusCancelled {
					log.Info().Str("run_id", handle.runID.String()).
						Msg("chat turn cancelled elsewhere in the fleet, stopping it here")
					handle.stop()
					return
				}
			}
		}
	}()
	return stop
}

func (s *Service) liveRuns(sessionID uuid.UUID) []*runHandle {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	keys := s.sessionRuns[sessionID]
	handles := make([]*runHandle, 0, len(keys))
	for key := range keys {
		if handle := s.runs[key]; handle != nil {
			handles = append(handles, handle)
		}
	}
	return handles
}

func (s *Service) CancelSession(ctx context.Context, sessionID uuid.UUID, reason string) (bool, error) {
	if _, err := s.store.Get(ctx, sessionID); err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrSessionNotFound, err)
	}
	handles := s.liveRuns(sessionID)
	if len(handles) == 0 {

		return s.cancelSessionRows(ctx, sessionID)
	}
	live := false
	for _, handle := range handles {
		stopped, err := s.stopRun(ctx, handle, reason)
		if err != nil {
			return live, err
		}
		live = live || stopped
	}
	return live, nil
}

func (s *Service) CancelSessionRun(ctx context.Context, sessionID, runID uuid.UUID, reason string) (bool, error) {
	if _, err := s.store.Get(ctx, sessionID); err != nil {
		return false, fmt.Errorf("%w: %v", domain.ErrSessionNotFound, err)
	}
	s.runsMu.Lock()
	handle := s.runs[runID]
	s.runsMu.Unlock()
	if handle == nil || handle.sessionID != sessionID {

		return s.cancelSessionRow(ctx, sessionID, runID)
	}
	return s.stopRun(ctx, handle, reason)
}

func (s *Service) cancelSessionRows(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	if s.activityStore == nil {
		return false, nil
	}
	runs, err := s.activityStore.ListRunsBySession(ctx, sessionID, activeRunLookback)
	if err != nil {
		return false, err
	}
	live := false
	for _, run := range runs {
		if run.Status != domain.SessionRunStatusRunning {
			continue
		}
		flipped, err := s.activityStore.CancelRun(ctx, run.ID)
		if err != nil {
			return live, err
		}
		live = live || flipped
	}
	return live, nil
}

func (s *Service) cancelSessionRow(ctx context.Context, sessionID, runID uuid.UUID) (bool, error) {
	if s.activityStore == nil {
		return false, nil
	}
	runs, err := s.activityStore.ListRunsBySession(ctx, sessionID, activeRunLookback)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.ID != runID {
			continue
		}
		if run.Status != domain.SessionRunStatusRunning {
			return false, nil
		}
		return s.activityStore.CancelRun(ctx, runID)
	}
	return false, nil
}

const activeRunLookback = 20

func (s *Service) stopRun(ctx context.Context, handle *runHandle, reason string) (bool, error) {
	flipped := false
	if s.activityStore != nil && handle.runID != uuid.Nil {
		var err error
		flipped, err = s.activityStore.CancelRun(ctx, handle.runID)
		if err != nil {
			return false, err
		}
	}
	stopped := handle.stop()
	if stopped {
		log.Info().
			Str("session_id", handle.sessionID.String()).
			Str("run_id", handle.runID.String()).
			Str("reason", strings.TrimSpace(reason)).
			Msg("chat turn stopped by user")
	}
	return stopped || flipped, nil
}

func (s *Service) persistCancelledTurn(ctx context.Context, sessionID uuid.UUID, partial string) {
	partial = strings.TrimSpace(partial)
	if partial == "" {
		return
	}
	s.persistAssistantResponse(ctx, sessionID, domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: partial},
	})
}
