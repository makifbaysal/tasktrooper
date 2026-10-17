package notify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const DefaultSendTimeout = 5 * time.Second

// Service fans a notification out to every registered, enabled channel. A
// source (incidents, releases, alerts) talks only to Notify, so adding a
// channel never touches the code that decides when to notify.
type Service struct {
	mu       sync.RWMutex
	channels []port.NotificationChannel
	disabled map[domain.NotificationChannelKind]bool
	timeout  time.Duration
}

type Option func(*Service)

func WithSendTimeout(d time.Duration) Option {
	return func(s *Service) {
		if d > 0 {
			s.timeout = d
		}
	}
}

func NewService(opts ...Option) *Service {
	s := &Service{
		disabled: make(map[domain.NotificationChannelKind]bool),
		timeout:  DefaultSendTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a channel, replacing one already registered under the same kind.
func (s *Service) Register(ch port.NotificationChannel) {
	if ch == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.channels {
		if existing.Kind() == ch.Kind() {
			s.channels[i] = ch
			return
		}
	}
	s.channels = append(s.channels, ch)
}

// SetEnabled is the per-kind switch a preference store drives; every kind
// starts enabled.
func (s *Service) SetEnabled(kind domain.NotificationChannelKind, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		delete(s.disabled, kind)
		return
	}
	s.disabled[kind] = true
}

func (s *Service) Enabled(kind domain.NotificationChannelKind) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.disabled[kind]
}

// Notify never returns a channel's failure and waits at most the send timeout:
// a notification is a side effect of the caller's work, never a reason for it
// to fail or stall. The caller's cancellation is not inherited: an alert
// arriving on a request that is about to end must still go out.
func (s *Service) Notify(ctx context.Context, n domain.Notification) {
	s.deliver(ctx, n, s.enabledChannels())
}

func (s *Service) deliver(ctx context.Context, n domain.Notification, targets []port.NotificationChannel) {
	if len(targets) == 0 {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, ch := range targets {
		wg.Add(1)
		go func(ch port.NotificationChannel) {
			defer wg.Done()
			if err := send(sendCtx, ch, n); err != nil {
				log.Warn().Err(err).
					Str("channel", string(ch.Kind())).
					Str("source", string(n.Source)).
					Str("source_id", n.SourceID).
					Msg("notification not delivered")
			}
		}(ch)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-sendCtx.Done():
		log.Warn().Str("source", string(n.Source)).Str("source_id", n.SourceID).
			Dur("timeout", s.timeout).Msg("notification fan-out timed out")
	}
}

func (s *Service) enabledChannels() []port.NotificationChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]port.NotificationChannel, 0, len(s.channels))
	for _, ch := range s.channels {
		if !s.disabled[ch.Kind()] {
			out = append(out, ch)
		}
	}
	return out
}

func send(ctx context.Context, ch port.NotificationChannel, n domain.Notification) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel panicked: %v", r)
		}
	}()
	return ch.Send(ctx, n)
}

// Kinds are the channel kinds registered, enabled or not: what a source may
// name and have delivered once a person turns it on.
func (s *Service) Kinds() []domain.NotificationChannelKind {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.NotificationChannelKind, 0, len(s.channels))
	for _, ch := range s.channels {
		out = append(out, ch.Kind())
	}
	return out
}
