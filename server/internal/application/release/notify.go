package release

import (
	"context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	approveRollbackLabel  = "notifications.actions.approve_rollback"
	notificationBodyRunes = 200
)

// notifyRelease tells a human about a release at most once per kind per
// release inside the cooldown: an agent re-proposing the same rollback, or a
// retried rollback, does not notify again.
func (s *Service) notifyRelease(ctx context.Context, kind domain.NotificationKind, r domain.Release, body string) {
	if s.notifier == nil {
		return
	}
	if !s.notifyCooldown.Allow(string(kind)+"\x00"+r.ID.String(), s.now()) {
		return
	}
	n := domain.Notification{
		Kind:     kind,
		Title:    releaseNotificationTitle(kind, r),
		Body:     truncateRunes(strings.TrimSpace(body), notificationBodyRunes),
		Severity: domain.IncidentSeverityHigh,
		Link:     domain.ReleaseLink(r.ID),
		Source:   domain.NotificationSourceRelease,
		SourceID: r.ID.String(),
	}
	if kind == domain.NotificationRollbackSuggested {
		n.Actions = []domain.NotificationAction{{ID: domain.NotificationActionApproveRollback, Label: approveRollbackLabel}}
	}
	s.notifier.Notify(ctx, n)
}

func releaseNotificationTitle(kind domain.NotificationKind, r domain.Release) string {
	if kind == domain.NotificationRollbackSuggested {
		return "Rollback suggested · " + r.Version
	}
	return "Rolled back · " + r.Version
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
