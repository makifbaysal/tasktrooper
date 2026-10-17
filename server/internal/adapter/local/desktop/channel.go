package desktop

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Channel is the macOS banner as a notification channel. Off macOS the
// notifier does nothing, so registering it everywhere is harmless.
type Channel struct {
	notifier *Notifier
}

func NewChannel(notifier *Notifier) *Channel {
	return &Channel{notifier: notifier}
}

func (c *Channel) Kind() domain.NotificationChannelKind {
	return domain.NotificationChannelDesktop
}

func (c *Channel) Send(_ context.Context, n domain.Notification) error {
	c.notifier.Notify(n.Title, n.Body)
	return nil
}
