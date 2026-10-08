package domain

import (
	"strings"

	"github.com/google/uuid"
)

// The order note is the generated half of a task's pre-deploy runbook: the
// sentence that says this task ships after another one, written from the
// relations rather than from an agent's memory of them. It lives INSIDE
// board_tasks.before_deploy rather than beside it, fenced by these two markers,
// because that field is what the release path reads, the pre-deploy checklist
// posts and the SPA renders — and fencing makes the field safe to regenerate:
// the generator replaces only what is between the markers and never touches a
// character the agent wrote outside them. The markers are HTML comments so they
// vanish in every markdown renderer, and they are literal — no regex — so text
// that merely resembles them cannot be mistaken for a fence.
const (
	OrderNoteOpen  = "<!-- tt:order -->"
	OrderNoteClose = "<!-- /tt:order -->"
)

// OrderNoteEmpty reports whether a task declares no ordering at all —
// deployAfter and workAfter are both empty, so OrderNote's generated block
// renders as "". deployAfter names the tasks that must be live in production
// first (deploy_depends_on); workAfter names the tasks that must be finished
// before this one may be worked on (the `blocks` rows pointing at it). Both
// are rendered because both answer the same question — which task goes
// first — at two moments. Rendering the two lists into the generated block's
// sentences happens at the application consumer (briefs.repository.order_note
// in catalog/system), since domain must not import application.
func OrderNoteEmpty(deployAfter, workAfter []string) bool {
	return len(deployAfter) == 0 && len(workAfter) == 0
}

// StripOrderNote removes a previously generated block, leaving everything a
// human or an agent wrote around it. An unterminated opening marker takes the
// rest of the text with it — the safe reading of a truncated field, since the
// alternative would make the next generation append a second block.
func StripOrderNote(text string) string {
	for {
		start := strings.Index(text, OrderNoteOpen)
		if start < 0 {
			return strings.TrimSpace(text)
		}
		rest := text[start+len(OrderNoteOpen):]
		end := strings.Index(rest, OrderNoteClose)
		if end < 0 {
			text = text[:start]
			continue
		}
		text = text[:start] + rest[end+len(OrderNoteClose):]
	}
}

// ApplyOrderNote returns the runbook text with the generated block refreshed:
// any previous block removed, the new one placed FIRST and every other line kept
// verbatim. First on purpose — "this ships after T-12" is a precondition for
// the whole checklist below it, and a reader who stops after the first screen
// must not be the one who misses it.
func ApplyOrderNote(existing, note string) string {
	body := StripOrderNote(existing)
	switch {
	case note == "" && body == "":
		return ""
	case note == "":
		return body
	case body == "":
		return note
	default:
		return note + "\n\n" + body
	}
}

// AnalysisReference is one analiz task an implementation task was opened out
// of, together with the documents that analysis produced. It is a domain type
// rather than a repository-package one so the board runner can put it in front
// of a run without importing the application service that assembles it — the
// same reason RunJob carries a BoardTask rather than a service handle.
type AnalysisReference struct {
	TaskID    uuid.UUID      `json:"task_id"`
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	Documents []TaskDocument `json:"documents"`
	// TaskType, Column and RepositoryID describe the referenced task, so a
	// design reference can be told apart from an analysis and opened.
	TaskType     TaskType   `json:"task_type,omitempty"`
	Column       TaskColumn `json:"column,omitempty"`
	RepositoryID uuid.UUID  `json:"repository_id,omitempty"`
}

// IsDesign is a reference to a design task: its documents are the approved
// screens, the hand-off spec or the design system report.
func (r AnalysisReference) IsDesign() bool { return r.TaskType == TaskTypeDesign }
