package smokegen

import (
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Status is where a generation run sits; a job never leaves "running" except
// into exactly one of the other three, and only once.
type Status string

const (
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Job is a smoke-check generation run: read-only evidence of what the
// release-engineer agent proposed and how it tested, never a saved delivery
// profile. Held in memory only (see Service) and returned to the HTTP layer
// as-is.
type Job struct {
	JobID       uuid.UUID  `json:"job_id"`
	ComponentID uuid.UUID  `json:"component_id"`
	Status      Status     `json:"status"`
	AgentName   string     `json:"agent_name"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	// Checks/Results are never null on the wire — an empty proposal is still
	// an array, so the UI never has to special-case "no field" vs "no rows".
	Checks  []domain.SmokeCheck  `json:"checks"`
	Results []domain.SmokeResult `json:"results"`
	BaseURL string               `json:"base_url"`
	// Dropped counts checks the agent proposed that were invalid, duplicated
	// (against existing or each other) or cut by the cap — evidence the
	// generator did something even when it under-delivers.
	Dropped int    `json:"dropped"`
	Error   string `json:"error"`
}
