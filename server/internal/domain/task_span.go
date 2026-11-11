package domain

import (
	"time"

	"github.com/google/uuid"
)

// TaskColumnSpan is one uninterrupted stay of a task in one board column. A
// task that bounces back through need_revision produces a second span for the
// same column with a higher VisitNo rather than overwriting the first, so the
// history of a rework loop stays readable.
type TaskColumnSpan struct {
	ID              uuid.UUID  `json:"id"`
	TaskID          uuid.UUID  `json:"task_id"`
	RepositoryID    uuid.UUID  `json:"repository_id"`
	BoardColumn     string     `json:"board_column"`
	AgentID         *uuid.UUID `json:"agent_id,omitempty"`
	EnteredAt       time.Time  `json:"entered_at"`
	LeftAt          *time.Time `json:"left_at,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	VisitNo         int        `json:"visit_no"`
	ReviewVerdict   string     `json:"review_verdict,omitempty"`
	// PatchID is `git patch-id --stable` of the diff a code_review approval
	// closed this span on; empty for every span that isn't an approved
	// code_review visit. The diff-skip stage compares it against the task's
	// current patch id instead of trusting the span's column alone.
	PatchID string `json:"patch_id,omitempty"`
}

const (
	ReviewVerdictApprove = "approve"
	ReviewVerdictReject  = "reject"
)

// SpanCountsForSpeed reports whether time spent in a column may be charged to
// an agent's speed KPI. Excluded: blocked (waiting on a human), human_uat and
// analiz_review (human gates), backlog/todo (nobody's work), need_revision (a
// queue, not work), and the terminal columns.
func SpanCountsForSpeed(col TaskColumn) bool {
	switch col {
	case TaskColumnInProgress, TaskColumnCodeReview, TaskColumnReadyForQA,
		TaskColumnInQA:
		return true
	default:
		return false
	}
}
