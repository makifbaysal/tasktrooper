package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrorCategory is what kind of failure an error definition names.
type ErrorCategory string

const (
	ErrorCategoryBug            ErrorCategory = "bug"
	ErrorCategoryExpected       ErrorCategory = "expected"
	ErrorCategoryDependency     ErrorCategory = "dependency"
	ErrorCategoryInfrastructure ErrorCategory = "infrastructure"
	ErrorCategoryUserError      ErrorCategory = "user_error"
)

func ValidErrorCategory(c ErrorCategory) bool {
	switch c {
	case ErrorCategoryBug, ErrorCategoryExpected, ErrorCategoryDependency, ErrorCategoryInfrastructure, ErrorCategoryUserError:
		return true
	}
	return false
}

// ErrorAction is what a matching error group sets off.
type ErrorAction string

const (
	ErrorActionNone ErrorAction = "none"
	// ErrorActionIgnore hides matching groups from the Errors inbox.
	ErrorActionIgnore ErrorAction = "ignore"
	// ErrorActionAlert opens (and resolves) an incident while the matching
	// groups' count in the threshold window is at or above the threshold.
	ErrorActionAlert ErrorAction = "alert"
	// ErrorActionIncident opens an incident whenever the threshold is
	// crossed and never resolves it on its own.
	ErrorActionIncident ErrorAction = "incident"
	// ErrorActionTask opens one board task per matching error group.
	ErrorActionTask ErrorAction = "task"
)

func ValidErrorAction(a ErrorAction) bool {
	switch a {
	case ErrorActionNone, ErrorActionIgnore, ErrorActionAlert, ErrorActionIncident, ErrorActionTask:
		return true
	}
	return false
}

// Thresholded reports the actions an alert_threshold applies to.
func (a ErrorAction) Thresholded() bool { return a == ErrorActionAlert || a == ErrorActionIncident }

// Swept reports the actions the background sweep carries out.
func (a ErrorAction) Swept() bool { return a.Thresholded() || a == ErrorActionTask }

type ErrorDefinitionSource string

const (
	ErrorDefinitionHuman ErrorDefinitionSource = "human"
	ErrorDefinitionAI    ErrorDefinitionSource = "ai"
	// ErrorDefinitionCode is an AI suggestion derived from an error type or
	// code declared in the repository's own code.
	ErrorDefinitionCode ErrorDefinitionSource = "code"
)

type ErrorDefinitionStatus string

const (
	ErrorDefinitionActive    ErrorDefinitionStatus = "active"
	ErrorDefinitionSuggested ErrorDefinitionStatus = "suggested"
	ErrorDefinitionDismissed ErrorDefinitionStatus = "dismissed"
)

func ValidErrorDefinitionStatus(s ErrorDefinitionStatus) bool {
	return s == ErrorDefinitionActive || s == ErrorDefinitionSuggested || s == ErrorDefinitionDismissed
}

// ErrorMatch is what an error group must show for a definition to apply:
// every field that is set must hold. Status 0 and an empty map are unset.
type ErrorMatch struct {
	Fingerprint   string            `json:"fingerprint,omitempty"`
	MessageRegex  string            `json:"message_regex,omitempty"`
	ExceptionType string            `json:"exception_type,omitempty"`
	Endpoint      string            `json:"endpoint,omitempty"`
	Status        int               `json:"status,omitempty"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

func (m ErrorMatch) Empty() bool {
	return m.Fingerprint == "" && m.MessageRegex == "" && m.ExceptionType == "" && m.Endpoint == "" &&
		m.Status == 0 && len(m.Attributes) == 0
}

// Specificity orders definitions that match the same group: a fingerprint
// names exactly one group, every other field narrows by one.
func (m ErrorMatch) Specificity() int {
	n := len(m.Attributes)
	if m.Fingerprint != "" {
		n += 10
	}
	for _, set := range []bool{m.MessageRegex != "", m.ExceptionType != "", m.Endpoint != "", m.Status != 0} {
		if set {
			n++
		}
	}
	return n
}

type ErrorAlertThreshold struct {
	Count         int   `json:"count"`
	WindowSeconds int64 `json:"window_seconds"`
}

func (t ErrorAlertThreshold) Window() time.Duration {
	return time.Duration(t.WindowSeconds) * time.Second
}

// ErrorDefinition says what one kind of runtime error means (migration 191).
// ComponentID nil covers every component of the repository. FiringSince is
// set while an alert or incident definition is over its threshold.
type ErrorDefinition struct {
	ID             uuid.UUID             `json:"id"`
	RepositoryID   uuid.UUID             `json:"repository_id"`
	ComponentID    *uuid.UUID            `json:"component_id"`
	Name           string                `json:"name"`
	Match          ErrorMatch            `json:"match"`
	Category       ErrorCategory         `json:"category"`
	Severity       IncidentSeverity      `json:"severity"`
	Action         ErrorAction           `json:"action"`
	AlertThreshold *ErrorAlertThreshold  `json:"alert_threshold"`
	Source         ErrorDefinitionSource `json:"source"`
	Status         ErrorDefinitionStatus `json:"status"`
	Rationale      string                `json:"rationale"`
	Evidence       []string              `json:"evidence"`
	TaskID         *uuid.UUID            `json:"task_id"`
	FiringSince    *time.Time            `json:"firing_since"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// Ref is the definition as an Errors inbox group carries it.
func (d ErrorDefinition) Ref() *ErrorDefinitionRef {
	return &ErrorDefinitionRef{ID: d.ID, Name: d.Name, Category: d.Category, Severity: d.Severity, Action: d.Action}
}

type ErrorDefinitionRef struct {
	ID       uuid.UUID        `json:"id"`
	Name     string           `json:"name"`
	Category ErrorCategory    `json:"category"`
	Severity IncidentSeverity `json:"severity"`
	Action   ErrorAction      `json:"action"`
}

// ErrorDefinitionFilter: nil fields match everything; ComponentID matches
// the definitions of that component and the repository-wide ones.
type ErrorDefinitionFilter struct {
	RepositoryIDs []uuid.UUID
	ComponentID   *uuid.UUID
	Statuses      []ErrorDefinitionStatus
	Actions       []ErrorAction
}

// ErrorGroupLink records the task action's one task for one error group.
type ErrorGroupLink struct {
	DefinitionID   uuid.UUID  `json:"definition_id"`
	Fingerprint    string     `json:"fingerprint"`
	ComponentID    uuid.UUID  `json:"component_id"`
	EnvironmentID  *uuid.UUID `json:"environment_id"`
	DataSourceID   *uuid.UUID `json:"data_source_id"`
	TaskID         *uuid.UUID `json:"task_id"`
	FirstMatchedAt time.Time  `json:"first_matched_at"`
}

// IncidentSourceErrorDefinition is an incident an error definition's alert
// or incident action opened.
const IncidentSourceErrorDefinition = "error_definition"

const errorDefinitionFingerprintPrefix = "errdef:"

// ErrorDefinitionFingerprint is the one identity every incident of a
// definition shares, so a definition that keeps crossing folds into its open
// incident.
func ErrorDefinitionFingerprint(id uuid.UUID) string {
	return errorDefinitionFingerprintPrefix + id.String()
}

// ErrorDefinitionOfFingerprint reads the definition id back out of an
// incident fingerprint ErrorDefinitionFingerprint wrote.
func ErrorDefinitionOfFingerprint(fingerprint string) (uuid.UUID, bool) {
	raw, ok := strings.CutPrefix(fingerprint, errorDefinitionFingerprintPrefix)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}
