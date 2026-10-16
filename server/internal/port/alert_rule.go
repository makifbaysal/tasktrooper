package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AlertRuleStore persists alert rules and their state changes (migration
// 189). A missing rule is ErrNotFound.
type AlertRuleStore interface {
	// ListAlertRules answers by name, then id.
	ListAlertRules(ctx context.Context, filter domain.AlertRuleFilter) ([]domain.AlertRule, error)
	GetAlertRule(ctx context.Context, id uuid.UUID) (domain.AlertRule, error)
	// CreateAlertRule stores r's spec with its state; a zero ID is assigned.
	CreateAlertRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error)
	// UpdateAlertRule replaces the spec and the state of r.ID and moves
	// updated_at.
	UpdateAlertRule(ctx context.Context, r domain.AlertRule) (domain.AlertRule, error)
	DeleteAlertRule(ctx context.Context, id uuid.UUID) error
	// DueAlertRules answers the enabled rules last evaluated at or before before
	// (or never), the longest waiting first, at most limit.
	DueAlertRules(ctx context.Context, before time.Time, limit int) ([]domain.AlertRule, error)
	// RecordAlertEvaluation stores an evaluation on the rule and, when event
	// is not nil, that state change, in one transaction — only while the
	// rule's updated_at is still specVersion. ok is false when the rule was
	// edited or deleted since it was read; then nothing is written.
	RecordAlertEvaluation(ctx context.Context, id uuid.UUID, specVersion time.Time, eval domain.AlertEvaluation, event *domain.AlertEvent) (rule domain.AlertRule, ok bool, err error)
	// AppendAlertEvent records a state change made outside an evaluation (an
	// edit resolving a firing rule).
	AppendAlertEvent(ctx context.Context, event domain.AlertEvent) error
	// ListAlertEvents answers the newest first, at most limit.
	ListAlertEvents(ctx context.Context, ruleID uuid.UUID, limit int) ([]domain.AlertEvent, error)
	PruneAlertEvents(ctx context.Context, before time.Time) (int64, error)
}
