package domain

import (
	"time"

	"github.com/google/uuid"
)

// LocalPreviewStatus is where a "run it locally" process is in its life.
type LocalPreviewStatus string

const (
	LocalPreviewStarting LocalPreviewStatus = "starting"
	LocalPreviewRunning  LocalPreviewStatus = "running"
	LocalPreviewStopped  LocalPreviewStatus = "stopped"
	LocalPreviewFailed   LocalPreviewStatus = "failed"
)

// LocalPreview is one repository's "run it locally" process — the human_uat
// reviewer's own manual pass at the task's branch, running on this machine.
// See application/localpreview and PM's automated pm-uat-review skill (which
// exercises stage, never local) for the other half of a task's UAT.
type LocalPreview struct {
	RepositoryID uuid.UUID          `json:"repository_id"`
	TaskID       uuid.UUID          `json:"task_id"`
	Branch       string             `json:"branch"`
	Command      string             `json:"command"`
	Status       LocalPreviewStatus `json:"status"`
	// URL is the http://localhost:<port> line detected in the process's own
	// output, "" until one has been seen.
	URL string `json:"url,omitempty"`
	// Detail explains a Failed status — the command's own complaint, or why a
	// preview could not even be started (no RunCommand configured, checkout
	// failed, ...).
	Detail    string    `json:"detail,omitempty"`
	StartedAt time.Time `json:"started_at"`
	// LogTail is the process's most recent output lines, oldest first.
	LogTail []string `json:"log_tail,omitempty"`
}
