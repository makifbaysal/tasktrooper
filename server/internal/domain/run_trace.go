package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	TraceCategoryRead     = "read"
	TraceCategoryChange   = "change"
	TraceCategoryExec     = "exec"
	TraceCategoryExternal = "external"
	TraceCategoryBoard    = "board"
	TraceCategoryUndo     = "undo"
)

var TraceCategories = []string{
	TraceCategoryRead, TraceCategoryChange, TraceCategoryExec,
	TraceCategoryExternal, TraceCategoryBoard, TraceCategoryUndo,
}

func ValidTraceCategory(c string) bool {
	for _, known := range TraceCategories {
		if c == known {
			return true
		}
	}
	return false
}

const (
	TraceStatusOK         = "ok"
	TraceStatusError      = "error"
	TraceStatusUnfinished = "unfinished"
)

const (
	TraceSourceLoop = "loop"
	TraceSourceCLI  = "cli"
	TraceSourceMCP  = "mcp"
	TraceSourceUndo = "undo"
)

const (
	MaxTraceEventsPerRun = 5000
	TraceRetentionDays   = 90
)

// RunTraceEvent is one tool call of a board run, written when the call
// finished. Detail carries the structured summary of its arguments and
// outcome; it never carries file contents, request bodies or credentials.
type RunTraceEvent struct {
	RunID      uuid.UUID       `json:"run_id"`
	TaskID     uuid.UUID       `json:"task_id"`
	AgentID    uuid.UUID       `json:"agent_id"`
	Seq        int             `json:"seq"`
	At         time.Time       `json:"at"`
	DurationMS int64           `json:"duration_ms"`
	Tool       string          `json:"tool"`
	NativeTool string          `json:"native_tool,omitempty"`
	Source     string          `json:"source"`
	Category   string          `json:"category"`
	Target     string          `json:"target,omitempty"`
	Status     string          `json:"status"`
	Error      string          `json:"error,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`
}

type TraceFileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	From   string `json:"from,omitempty"`
}

// RunTraceVerification is what git says the run changed, read once the run was
// over and compared with the change events the trace recorded.
type RunTraceVerification struct {
	ComputedAt time.Time         `json:"computed_at"`
	BaseSHA    string            `json:"base_sha"`
	HeadSHA    string            `json:"head_sha,omitempty"`
	Available  bool              `json:"available"`
	Reason     string            `json:"reason,omitempty"`
	Files      []TraceFileChange `json:"files"`
	// Unrecorded are files git shows changed that no recorded change event
	// accounts for: the shell or a script wrote them.
	Unrecorded []string `json:"unrecorded"`
	// RecordedOnly are paths a change event names that git shows unchanged
	// (edited back, or never landed).
	RecordedOnly []string `json:"recorded_only"`
	TrackedFiles int      `json:"tracked_files"`
	Untouched    int      `json:"untouched"`
}

// RunTrace is the per-run header row: the commit the run started from and the
// verified outcome.
type RunTrace struct {
	RunID         uuid.UUID             `json:"run_id"`
	TaskID        uuid.UUID             `json:"task_id"`
	AgentID       uuid.UUID             `json:"agent_id"`
	BaseSHA       string                `json:"base_sha,omitempty"`
	HeadSHA       string                `json:"head_sha,omitempty"`
	Branch        string                `json:"branch,omitempty"`
	WorkspacePath string                `json:"workspace_path,omitempty"`
	ColumnBefore  string                `json:"column_before,omitempty"`
	Remote        bool                  `json:"remote,omitempty"`
	Events        int                   `json:"events"`
	Dropped       int                   `json:"dropped"`
	Verification  *RunTraceVerification `json:"verification,omitempty"`
	StartedAt     time.Time             `json:"started_at"`
	FinishedAt    *time.Time            `json:"finished_at,omitempty"`
}

type RunTraceCounts struct {
	Read     int `json:"read"`
	Change   int `json:"change"`
	Exec     int `json:"exec"`
	External int `json:"external"`
	Board    int `json:"board"`
	Undo     int `json:"undo"`
	Failed   int `json:"failed"`
	Total    int `json:"total"`
	// ReadFiles / ChangedFiles are distinct paths, not call counts.
	ReadFiles    int `json:"read_files"`
	ChangedFiles int `json:"changed_files"`
}

// RunTraceEffect is something a run did that no git reset takes back.
type RunTraceEffect struct {
	Seq    int    `json:"seq"`
	Tool   string `json:"tool"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}

type RunTraceSummary struct {
	Run          RunTrace              `json:"run"`
	Counts       RunTraceCounts        `json:"counts"`
	Verification *RunTraceVerification `json:"verification,omitempty"`
	NonUndoable  []RunTraceEffect      `json:"non_undoable"`
}

var ErrRunTraceNotFound = errors.New("run trace not found")
