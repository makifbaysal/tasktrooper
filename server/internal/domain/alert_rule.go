package domain

import (
	"time"

	"github.com/google/uuid"
)

// AlertOp is what an alert rule's condition tests its value for.
type AlertOp string

const (
	AlertOpAbove          AlertOp = "gt"
	AlertOpBelow          AlertOp = "lt"
	AlertOpChangePctAbove AlertOp = "change_pct_gt"
	AlertOpChangePctBelow AlertOp = "change_pct_lt"
	// AlertOpRising holds while the value is above the threshold and higher
	// than one window earlier.
	AlertOpRising AlertOp = "rising"
	// AlertOpNoData holds while the rule reads nothing.
	AlertOpNoData AlertOp = "no_data"
)

func ValidAlertOp(op AlertOp) bool {
	switch op {
	case AlertOpAbove, AlertOpBelow, AlertOpChangePctAbove, AlertOpChangePctBelow, AlertOpRising, AlertOpNoData:
		return true
	}
	return false
}

// LowIsWorse: the ops a falling value trips, whose worst sample is the lowest.
func (op AlertOp) LowIsWorse() bool {
	return op == AlertOpBelow || op == AlertOpChangePctBelow
}

type AlertCondition struct {
	Op        AlertOp `json:"op"`
	Threshold float64 `json:"threshold"`
	// ForSeconds is how long the condition must hold before the rule fires.
	ForSeconds int64 `json:"for_seconds"`
	// WindowSeconds is what one value summarizes, and how far back the
	// change and rising ops compare.
	WindowSeconds int64 `json:"window_seconds"`
}

func (c AlertCondition) For() time.Duration    { return time.Duration(c.ForSeconds) * time.Second }
func (c AlertCondition) Window() time.Duration { return time.Duration(c.WindowSeconds) * time.Second }

type AlertState string

const (
	AlertStateOK      AlertState = "ok"
	AlertStatePending AlertState = "pending"
	AlertStateFiring  AlertState = "firing"
	AlertStateNoData  AlertState = "no_data"
)

// AlertRuleSpec is what a person sets on an alert rule: a catalog Signal
// read through the component environment's confirmed binding, or a raw
// Query on DataSourceID, never both.
type AlertRuleSpec struct {
	Name         string                    `json:"name"`
	ComponentID  uuid.UUID                 `json:"component_id"`
	Environment  DeployEnvironment         `json:"environment"`
	Signal       string                    `json:"signal"`
	DataSourceID *uuid.UUID                `json:"data_source_id"`
	Query        string                    `json:"query"`
	Condition    AlertCondition            `json:"condition"`
	Severity     IncidentSeverity          `json:"severity"`
	Channels     []NotificationChannelKind `json:"channels"`
	OpenIncident bool                      `json:"open_incident"`
	Enabled      bool                      `json:"enabled"`
}

// AlertRule is a spec plus where its evaluation stands (migration 189).
// UpdatedAt moves only when the spec changes, never on an evaluation.
type AlertRule struct {
	ID uuid.UUID `json:"id"`
	AlertRuleSpec
	State           AlertState `json:"state"`
	StateSince      time.Time  `json:"state_since"`
	LastValue       *float64   `json:"last_value"`
	LastEvaluatedAt *time.Time `json:"last_evaluated_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type AlertRuleFilter struct {
	ComponentID *uuid.UUID
	Environment DeployEnvironment
}

// AlertEvaluation is one evaluation's outcome as kept on the rule.
type AlertEvaluation struct {
	State      AlertState
	StateSince time.Time
	Value      *float64
	At         time.Time
}

// AlertEvent is one state change of a rule; IncidentID is the incident its
// firing opened or its recovery resolved.
type AlertEvent struct {
	ID         uuid.UUID  `json:"-"`
	RuleID     uuid.UUID  `json:"-"`
	At         time.Time  `json:"at"`
	FromState  AlertState `json:"from_state"`
	ToState    AlertState `json:"to_state"`
	Value      *float64   `json:"value"`
	IncidentID *uuid.UUID `json:"incident_id"`
}

// AlertFire is one firing a backtest replayed; End is nil while it was
// still firing at the end of the range.
type AlertFire struct {
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end"`
	Peak  *float64   `json:"peak"`
}

type AlertSeries struct {
	Name   string        `json:"name"`
	Points []SeriesPoint `json:"points"`
}

// AlertBacktest is POST /v1/alert-rules/backtest.
type AlertBacktest struct {
	Fires       []AlertFire   `json:"fires"`
	Series      []AlertSeries `json:"series"`
	StepSeconds int64         `json:"step_seconds"`
}

// IncidentSourceAlertRule is an incident an alert rule opened.
const IncidentSourceAlertRule = "alert_rule"

// AlertRuleFingerprint is the one identity every firing of a rule shares, so
// a rule that keeps firing folds into its open incident.
func AlertRuleFingerprint(ruleID uuid.UUID) string { return "alert:" + ruleID.String() }

func AlertRuleLink(componentID, ruleID uuid.UUID) string {
	return "/operations/entity/" + componentID.String() + "/alerts?rule=" + ruleID.String()
}
