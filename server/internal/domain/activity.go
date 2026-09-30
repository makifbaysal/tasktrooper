package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrRunCancelled is a chat turn a human stopped from the UI. It travels back
// up so the transport can end cleanly instead of reporting a failure; the run's
// row is already persisted by then.
var ErrRunCancelled = errors.New("agent run cancelled by user")

// The statuses a session_runs row can hold, as constants rather than literals
// because reads COMPARE a status while a typo in a write would not be silent.
const (
	SessionRunStatusRunning   = "running"
	SessionRunStatusCompleted = "completed"
	SessionRunStatusFailed    = "failed"
	SessionRunStatusCancelled = "cancelled"
)

type SessionRun struct {
	ID        uuid.UUID  `json:"id"`
	SessionID *uuid.UUID `json:"session_id,omitempty"`
	// AgentID and Title come from the owning session, not session_runs itself
	// (see ActivityStore.ListActiveRuns) — the desktop shell needs them to
	// build a chat-turn notification and its /agents/:agentId/chat/:sessionId
	// route without a second round trip per run.
	AgentID     *uuid.UUID `json:"agent_id,omitempty"`
	Title       string     `json:"title,omitempty"`
	RequestID   string     `json:"request_id"`
	Status      string     `json:"status"`
	Model       string     `json:"model,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type SessionStep struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	StepType  string          `json:"step_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type APIKeyRecord struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	ToolPolicy ToolPolicy `json:"tool_policy"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name       string     `json:"name"`
	ToolPolicy ToolPolicy `json:"tool_policy"`
}

type CreateAPIKeyResponse struct {
	APIKeyRecord
	Key string `json:"key"`
}
