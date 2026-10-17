package notify_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

func TestNotifyViaReachesOnlyTheNamedEnabledKinds(t *testing.T) {
	n := domain.Notification{Kind: domain.NotificationAlertFiring, Title: "PROD · Errors", Source: domain.NotificationSourceAlert}
	channel := func(kind domain.NotificationChannelKind) *mocks.NotificationChannel {
		ch := mocks.NewNotificationChannel(t)
		ch.On("Kind").Return(kind).Maybe()
		return ch
	}
	desktop := channel(domain.NotificationChannelDesktop)
	slack := channel(domain.NotificationChannelSlack)
	email := channel(domain.NotificationChannelEmail)
	slack.On("Send", mock.Anything, n).Return(nil).Once()
	svc := notify.NewService()
	svc.Register(desktop)
	svc.Register(slack)
	svc.Register(email)
	svc.SetEnabled(domain.NotificationChannelEmail, false)

	svc.NotifyVia(context.Background(), n, []domain.NotificationChannelKind{domain.NotificationChannelSlack, domain.NotificationChannelEmail})
	svc.NotifyVia(context.Background(), n, nil)
}
