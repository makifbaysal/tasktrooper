package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPipelineNotFound signals that a task has no pipeline runs yet; callers
// (e.g. get_pipeline_status) treat this as an informational state, not a
// failure.
var ErrPipelineNotFound = errors.New("no pipeline for this task")

// ErrPipelineActive signals that a manual (re-)trigger or retry was rejected
// because the task's latest pipeline is still pending or running — a second run
// now would execute concurrently in the same task workspace. The
// ready_for_qa move trigger is exempt: moving a task's column into
// ready_for_qa re-triggers deliberately and supersedes any pending pipeline.
var ErrPipelineActive = errors.New("pipeline is already running")

type PipelineTrigger string

const (
	PipelineTriggerReadyForQA PipelineTrigger = "ready_for_qa"
	PipelineTriggerManual     PipelineTrigger = "manual"
	PipelineTriggerRetry      PipelineTrigger = "retry"
	// Deploy triggers record a stage/prod GitHub Actions deploy as a pipeline
	// so its status and job logs surface in the same UI/tooling as the QA gate.
	PipelineTriggerStageDeploy   PipelineTrigger = "stage_deploy"
	PipelineTriggerPreProdDeploy PipelineTrigger = "preprod_deploy"
	PipelineTriggerProdDeploy    PipelineTrigger = "prod_deploy"
)

type PipelineStatus string

const (
	PipelineStatusPending PipelineStatus = "pending"
	PipelineStatusRunning PipelineStatus = "running"
	PipelineStatusSuccess PipelineStatus = "success"
	PipelineStatusFailed  PipelineStatus = "failed"
	// PipelineStatusSkipped: nothing was executed — the repository has no
	// job/workflow mapped for this trigger. It passes the gate exactly like
	// success (an unconfigured repo must still flow), but it is NOT evidence
	// that anything built or tested, and the UI must not paint it green.
	PipelineStatusSkipped PipelineStatus = "skipped"
)

// PipelineStatusOpensGate reports whether a terminal pipeline status lets the
// task continue. Skipped counts: no checks were configured, so there is nothing
// to fail — the difference from success is what it proves, not what it permits.
func PipelineStatusOpensGate(s PipelineStatus) bool {
	return s == PipelineStatusSuccess || s == PipelineStatusSkipped
}

type PipelineJobStatus string

const (
	PipelineJobStatusPending PipelineJobStatus = "pending"
	PipelineJobStatusRunning PipelineJobStatus = "running"
	PipelineJobStatusSuccess PipelineJobStatus = "success"
	PipelineJobStatusFailed  PipelineJobStatus = "failed"
	PipelineJobStatusSkipped PipelineJobStatus = "skipped"
)

// PipelineProvider records where a pipeline actually executed, so the UI can
// say "ran on GitHub Actions" instead of leaving the user guessing.
const (
	// PipelineProviderGitHubActions: the run was executed by GitHub Actions.
	PipelineProviderGitHubActions = "github_actions"
	// PipelineProviderNone: nothing executed (no workflow mapped, no
	// workspace, GitHub not connected).
	PipelineProviderNone = "none"
)

// Pipeline gate reasons: why the code-review gate opened without a green
// build. Empty on every ordinary pipeline. They exist because the alternative —
// opening the gate silently — is indistinguishable, from the board, from CI
// having passed: the reviewer would be dispatched onto a diff nobody built, and
// the only record of that would be its absence.
const (
	// PipelineGateReasonTimeout: nothing reported within the gate window; the
	// pipeline may still be running somewhere, the board has stopped waiting.
	PipelineGateReasonTimeout = "timeout"
	// PipelineGateReasonCIUnavailable: GitHub said the run cannot happen —
	// Actions billing/quota exhausted (402/403), or no run exists for the
	// task's head commit long after the branch was pushed.
	PipelineGateReasonCIUnavailable = "ci_unavailable"
	// PipelineGateReasonNoCI: the repository has no validate/build/test job
	// mapped, so there was never anything to wait for.
	PipelineGateReasonNoCI = "no_ci_configured"
)

// PipelineGateReasonOpen reports whether a gate reason means the reviewer was
// dispatched WITHOUT a build behind it — used by the UI to decide whether to
// explain itself, and by the board to decide whether to say so on the card.
func PipelineGateReasonOpen(reason string) bool {
	switch reason {
	case PipelineGateReasonTimeout, PipelineGateReasonCIUnavailable,
		PipelineGateReasonNoCI:
		return true
	}
	return false
}

// TaskPipelineDigest is the per-task pipeline summary a board list carries:
// enough to paint the card, without loading every job of every pipeline.
type TaskPipelineDigest struct {
	Status     string `json:"status"`
	GateReason string `json:"gate_reason,omitempty"`
}

type TaskPipeline struct {
	ID           uuid.UUID       `json:"id"`
	TaskID       uuid.UUID       `json:"task_id"`
	RepositoryID uuid.UUID       `json:"repository_id"`
	Trigger      PipelineTrigger `json:"trigger"`
	Status       PipelineStatus  `json:"status"`
	Provider     string          `json:"provider,omitempty"`
	Note         string          `json:"note,omitempty"`
	// HeadSHA is the commit the pipeline is about, recorded once when the
	// pipeline resolves the task branch's git info — the only coordinate the
	// reconciling poll needs afterwards, since the task workspace may be gone
	// and the process that started the pipeline may be dead.
	HeadSHA string `json:"head_sha,omitempty"`
	// GateReason is why the code-review gate opened without a green build — one
	// of the PipelineGateReason* codes, or "" for an ordinary pipeline.
	GateReason string            `json:"gate_reason,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	StartedAt  *time.Time        `json:"started_at,omitempty"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Jobs       []TaskPipelineJob `json:"jobs,omitempty"`
	// CoveragePct is the test coverage parsed from the pipeline's test job
	// log(s), the mean when more than one reports; nil when no test job printed
	// a parsable total.
	CoveragePct *float64 `json:"coverage_pct,omitempty"`
}

type TaskPipelineJob struct {
	ID         uuid.UUID         `json:"id"`
	PipelineID uuid.UUID         `json:"pipeline_id"`
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	Status     PipelineJobStatus `json:"status"`
	ExitCode   *int              `json:"exit_code,omitempty"`
	Output     string            `json:"output,omitempty"`
	DurationMS int64             `json:"duration_ms"`
	Position   int               `json:"position"`
	// RunURL points at the provider's page for this job (an Actions job URL),
	// so the UI can link straight to the real run instead of only mirroring its
	// logs.
	RunURL string `json:"run_url,omitempty"`
	// CoveragePct is the test coverage parsed from this job's log (test-category
	// jobs only); nil when the job isn't a test job or the log had no parsable
	// total.
	CoveragePct *float64 `json:"coverage_pct,omitempty"`
}
