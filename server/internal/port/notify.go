package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// NotificationChannel delivers a notification over one medium. Send should
// honour ctx's deadline; the fan-out stops waiting for it once that passes.
type NotificationChannel interface {
	Kind() domain.NotificationChannelKind
	Send(ctx context.Context, n domain.Notification) error
}
