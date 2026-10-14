package domain

import (
	"time"

	"github.com/google/uuid"
)

// FindingCategory groups code-health rules the way a person triages them.
type FindingCategory string

const (
	FindingLogging       FindingCategory = "logging"
	FindingErrorHandling FindingCategory = "error_handling"
	FindingConfiguration FindingCategory = "configuration"
	FindingObservability FindingCategory = "observability"
	FindingSecurity      FindingCategory = "security"
	FindingReliability   FindingCategory = "reliability"
	FindingQuality       FindingCategory = "quality"
)

func AllFindingCategories() []FindingCategory {
	return []FindingCategory{
		FindingSecurity, FindingConfiguration, FindingLogging, FindingErrorHandling,
		FindingObservability, FindingReliability, FindingQuality,
	}
}

type FindingSeverity string

const (
	FindingSeverityHigh   FindingSeverity = "high"
	FindingSeverityMedium FindingSeverity = "medium"
	FindingSeverityLow    FindingSeverity = "low"
)

// Rank orders severities high-first; an unknown one sorts last.
func (s FindingSeverity) Rank() int {
	switch s {
	case FindingSeverityHigh:
		return 0
	case FindingSeverityMedium:
		return 1
	case FindingSeverityLow:
		return 2
	default:
		return 3
	}
}

// TaskPriority is the board priority a task opened for this finding starts at.
func (s FindingSeverity) TaskPriority() TaskPriority {
	switch s {
	case FindingSeverityHigh:
		return TaskPriorityHigh
	case FindingSeverityLow:
		return TaskPriorityLow
	default:
		return TaskPriorityMedium
	}
}

type FindingStatus string

const (
	FindingOpen FindingStatus = "open"
	// FindingAccepted is a person keeping the problem on purpose (risk
	// accepted); FindingFalsePositive is a person saying the scan is wrong.
	// A later scan that finds the same problem again never reopens either.
	FindingAccepted      FindingStatus = "accepted"
	FindingFalsePositive FindingStatus = "false_positive"
	// FindingResolved is set by a scan that no longer finds the problem, never
	// by a person: a fix is only believed once the code says so.
	FindingResolved FindingStatus = "resolved"
)

// Closed by a person: a rescan updates the counts but keeps the verdict.
func (s FindingStatus) Suppressed() bool {
	return s == FindingAccepted || s == FindingFalsePositive
}

// ValidPatchStatus is what a person may set; resolved belongs to the scan.
func (s FindingStatus) ValidPatchStatus() bool {
	return s == FindingOpen || s == FindingAccepted || s == FindingFalsePositive
}

// MaxFindingEvidence caps the locations one finding keeps; Occurrences
// still counts every one.
const MaxFindingEvidence = 8

// DetectedFinding is every occurrence of one rule inside one component, as a
// scan read it. ComponentPath "" is a repository-wide finding no component
// owns (a root .env in a monorepo).
type DetectedFinding struct {
	ComponentPath string           `json:"component_path"`
	Rule          string           `json:"rule"`
	Category      FindingCategory  `json:"category"`
	Severity      FindingSeverity  `json:"severity"`
	Occurrences   int              `json:"occurrences"`
	Evidence      []SourceEvidence `json:"evidence,omitempty"`
}

// ComponentFinding is a DetectedFinding kept across scans, keyed by
// (component, rule): a person's verdict survives every rescan, and a task
// opened for it stays linked until the scan stops finding the problem.
type ComponentFinding struct {
	ID           uuid.UUID        `json:"id"`
	RepositoryID uuid.UUID        `json:"repository_id"`
	ComponentID  *uuid.UUID       `json:"component_id,omitempty"`
	Rule         string           `json:"rule"`
	Category     FindingCategory  `json:"category"`
	Severity     FindingSeverity  `json:"severity"`
	Occurrences  int              `json:"occurrences"`
	Evidence     []SourceEvidence `json:"evidence"`
	Status       FindingStatus    `json:"status"`
	// StatusReason is the person's word on why they accepted the finding or
	// called it a false positive.
	StatusReason string     `json:"status_reason,omitempty"`
	TaskID       *uuid.UUID `json:"task_id,omitempty"`
	// Task is read-side only: the linked board task as it stands now.
	Task       *FindingTask `json:"task,omitempty"`
	LastScanID *uuid.UUID   `json:"last_scan_id,omitempty"`
	ResolvedAt *time.Time   `json:"resolved_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

type FindingTask struct {
	ID     uuid.UUID  `json:"id"`
	Key    string     `json:"key"`
	Title  string     `json:"title"`
	Column TaskColumn `json:"column"`
}

// FindingPatch is PATCH /v1/findings/:id: reopen, accept or call a false
// positive, with an optional reason.
type FindingPatch struct {
	Status *FindingStatus `json:"status,omitempty"`
	Reason *string        `json:"reason,omitempty"`
}

// FindingTaskRequest is POST /v1/repositories/:id/findings/task: one board
// task covering every listed finding.
type FindingTaskRequest struct {
	FindingIDs []uuid.UUID  `json:"finding_ids"`
	Title      string       `json:"title"`
	TaskType   TaskType     `json:"task_type,omitempty"`
	Priority   TaskPriority `json:"priority,omitempty"`
	// Notes is the person's own words, added to the task above the findings.
	Notes string `json:"notes,omitempty"`
	// CreatedBy is set by the transport, never read from the body.
	CreatedBy string `json:"-"`
}

type FindingTaskResult struct {
	Task     BoardTask          `json:"task"`
	Findings []ComponentFinding `json:"findings"`
}
