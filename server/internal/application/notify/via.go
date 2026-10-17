package notify

import (
	"context"
	"slices"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// NotifyVia is Notify on the enabled channels of the given kinds only: a
// source that lets a person pick its channels (an alert rule) names them,
// and naming none reaches no channel at all.
func (s *Service) NotifyVia(ctx context.Context, n domain.Notification, kinds []domain.NotificationChannelKind) {
	var targets []port.NotificationChannel
	for _, ch := range s.enabledChannels() {
		if slices.Contains(kinds, ch.Kind()) {
			targets = append(targets, ch)
		}
	}
	s.deliver(ctx, n, targets)
}
