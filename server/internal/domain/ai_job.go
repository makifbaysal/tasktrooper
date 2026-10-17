package domain

import (
	"time"

	"github.com/google/uuid"
)

// AIJobKind is what one AI job asks the release engineer for.
type AIJobKind string

const (
	// AIJobDefineErrors reads recent error groups and proposes definitions.
	AIJobDefineErrors AIJobKind = "define_errors"
	// AIJobErrorCatalog reads the code's own error types and codes and
	// proposes `expected` definitions for them.
	AIJobErrorCatalog AIJobKind = "error_catalog"
	// AIJobMapSignals reads the data sources' inventories and proposes
	// bindings for the component's unbound golden signals.
	AIJobMapSignals AIJobKind = "map_signals"
)

type AIJobStatus string

const (
	AIJobRunning AIJobStatus = "running"
	AIJobDone    AIJobStatus = "done"
	AIJobFailed  AIJobStatus = "failed"
)

type AIJobItemKind string

const (
	AIJobItemErrorDefinition AIJobItemKind = "error_definition"
	AIJobItemSignalBinding   AIJobItemKind = "signal_binding"
	AIJobItemAlertRule       AIJobItemKind = "alert_rule"
)

// AIJobItem is one thing a job's tools stored, with the agent's reason.
type AIJobItem struct {
	Kind      AIJobItemKind `json:"kind"`
	ID        uuid.UUID     `json:"id"`
	Name      string        `json:"name"`
	Rationale string        `json:"rationale,omitempty"`
}

// AIJob is GET /v1/ai-jobs/:id (migration 191).
type AIJob struct {
	ID            uuid.UUID         `json:"id"`
	Kind          AIJobKind         `json:"kind"`
	RepositoryID  uuid.UUID         `json:"repository_id"`
	ComponentID   *uuid.UUID        `json:"component_id"`
	Environment   DeployEnvironment `json:"environment"`
	Status        AIJobStatus       `json:"status"`
	SessionID     *uuid.UUID        `json:"session_id"`
	ResultSummary string            `json:"result_summary"`
	Error         string            `json:"error"`
	Created       []AIJobItem       `json:"created"`
	CreatedAt     time.Time         `json:"created_at"`
	// StartedAt is when the job took a slot and its chat turn began (migration
	// 192); nil while it waits for one.
	StartedAt  *time.Time `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at"`
	// Owner is the process that created the job, so a restart fails only the
	// jobs a process before it left running.
	Owner string `json:"-"`
}

// AlertRuleCreator says who wrote an alert rule (migration 191).
type AlertRuleCreator string

const (
	AlertRuleByHuman AlertRuleCreator = "human"
	AlertRuleByAI    AlertRuleCreator = "ai"
)
