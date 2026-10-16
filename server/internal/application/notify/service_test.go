package notify_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type NotifySuite struct {
	suite.Suite
	ctx context.Context
	svc *notify.Service
	n   domain.Notification
}

func TestNotifySuite(t *testing.T) { suite.Run(t, new(NotifySuite)) }

func (s *NotifySuite) SetupTest() {
	s.ctx = context.Background()
	s.svc = notify.NewService(notify.WithSendTimeout(200 * time.Millisecond))
	s.n = domain.Notification{
		Title:    "PROD · api down",
		Body:     "roll back the last deploy",
		Severity: domain.IncidentSeverityCritical,
		Link:     "/operations/issues?id=abc",
		Source:   domain.NotificationSourceIncident,
		SourceID: "abc",
	}
}

func (s *NotifySuite) channel(kind domain.NotificationChannelKind) *mocks.NotificationChannel {
	ch := mocks.NewNotificationChannel(s.T())
	ch.On("Kind").Return(kind).Maybe()
	return ch
}

func (s *NotifySuite) TestFansOutToEveryRegisteredChannel() {
	desktop := s.channel(domain.NotificationChannelDesktop)
	slack := s.channel(domain.NotificationChannelSlack)
	desktop.On("Send", mock.Anything, s.n).Return(nil).Once()
	slack.On("Send", mock.Anything, s.n).Return(nil).Once()
	s.svc.Register(desktop)
	s.svc.Register(slack)

	s.svc.Notify(s.ctx, s.n)
}

func (s *NotifySuite) TestADisabledKindIsSkippedUntilReEnabled() {
	desktop := s.channel(domain.NotificationChannelDesktop)
	s.svc.Register(desktop)

	s.svc.SetEnabled(domain.NotificationChannelDesktop, false)
	s.False(s.svc.Enabled(domain.NotificationChannelDesktop))
	s.svc.Notify(s.ctx, s.n)
	desktop.AssertNotCalled(s.T(), "Send", mock.Anything, mock.Anything)

	s.svc.SetEnabled(domain.NotificationChannelDesktop, true)
	desktop.On("Send", mock.Anything, s.n).Return(nil).Once()
	s.svc.Notify(s.ctx, s.n)
}

func (s *NotifySuite) TestAFailingOrPanickingChannelDoesNotStopTheOthers() {
	failing := s.channel(domain.NotificationChannelWebhook)
	panicking := s.channel(domain.NotificationChannelEmail)
	healthy := s.channel(domain.NotificationChannelDesktop)
	failing.On("Send", mock.Anything, s.n).Return(errors.New("connection refused")).Once()
	panicking.On("Send", mock.Anything, s.n).Run(func(mock.Arguments) { panic("boom") }).Return(nil).Once()
	healthy.On("Send", mock.Anything, s.n).Return(nil).Once()
	s.svc.Register(failing)
	s.svc.Register(panicking)
	s.svc.Register(healthy)

	s.NotPanics(func() { s.svc.Notify(s.ctx, s.n) })
}

func (s *NotifySuite) TestASlowChannelDoesNotHoldTheCallerPastTheTimeout() {
	release := make(chan struct{})
	finished := make(chan struct{})
	slow := s.channel(domain.NotificationChannelIOSPush)
	slow.On("Send", mock.Anything, s.n).Run(func(mock.Arguments) {
		<-release
		close(finished)
	}).Return(nil).Once()
	s.svc.Register(slow)

	start := time.Now()
	s.svc.Notify(s.ctx, s.n)
	s.Less(time.Since(start), 2*time.Second)

	close(release)
	<-finished
}

func (s *NotifySuite) TestTheCallersCancellationIsNotInherited() {
	ctx, cancel := context.WithCancel(s.ctx)
	cancel()
	var sendErr error
	var mu sync.Mutex
	desktop := s.channel(domain.NotificationChannelDesktop)
	desktop.On("Send", mock.Anything, s.n).Run(func(args mock.Arguments) {
		mu.Lock()
		defer mu.Unlock()
		sendErr = args.Get(0).(context.Context).Err()
	}).Return(nil).Once()
	s.svc.Register(desktop)

	s.svc.Notify(ctx, s.n)

	mu.Lock()
	defer mu.Unlock()
	s.NoError(sendErr)
}

func (s *NotifySuite) TestRegisteringAKindAgainReplacesTheChannel() {
	first := s.channel(domain.NotificationChannelDesktop)
	second := s.channel(domain.NotificationChannelDesktop)
	second.On("Send", mock.Anything, s.n).Return(nil).Once()
	s.svc.Register(first)
	s.svc.Register(second)

	s.svc.Notify(s.ctx, s.n)
	first.AssertNotCalled(s.T(), "Send", mock.Anything, mock.Anything)
}

func (s *NotifySuite) TestNoChannelsIsANoOp() {
	s.NotPanics(func() { s.svc.Notify(s.ctx, s.n) })
}
