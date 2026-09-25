package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrTaskAgentRunNotFound is a run row that no longer exists — expected when
// an in-flight run's task is deleted (deleting a task cascades to its runs).
var ErrTaskAgentRunNotFound = errors.New("task agent run not found")

type BoardEventType string

const (
	BoardEventTaskCreated   BoardEventType = "task.created"
	BoardEventTaskMoved     BoardEventType = "task.moved"
	BoardEventTaskCommented BoardEventType = "task.commented"
	BoardEventTaskAssigned  BoardEventType = "task.assigned"
	// Written straight to the event store, never through the Dispatcher:
	// dispatching them would re-run every agent on the column.
	BoardEventTaskRunCancelled   BoardEventType = "task.run_cancelled"
	BoardEventTaskRerunRequested BoardEventType = "task.rerun_requested"
)

// Board event payload keys and system-move reasons. A control-plane move
// carries no actor_agent_id; these keys say "the system did it, and why".
const (
	// EventPayloadActor is "agent" | "human" | "system".
	EventPayloadActor  = "actor"
	EventPayloadReason = "system_reason"
	// The ONLY thing that can wake a task in a terminal column for the deploy
	// watch: `done` and `released` dispatch nobody, so the carve-out is keyed
	// on a payload only the sweeper writes rather than on task state, which any
	// comment or move could reproduce.
	EventPayloadResumedResource = "resumed_resource"
	// Names why the code-review gate dispatched WITHOUT a green build; absent
	// on the ordinary path. Goes on the event rather than only on the pipeline
	// row because the gate can be skipped with no pipeline row at all.
	EventPayloadPipelineGate = "pipeline_gate"

	EventActorAgent  = "agent"
	EventActorHuman  = "human"
	EventActorSystem = "system"

	// The QA-gate pipeline succeeded and the task was handed to its column's
	// reviewing agent. Not a column change.
	MoveReasonPipelinePassed = "pipeline_passed"
	MoveReasonPipelineFailed = "pipeline_failed"
	// No pipeline result ever arrived and the gate was opened anyway; distinct
	// from passed because only one of them means the build was green.
	MoveReasonPipelineGateOpened = "pipeline_gate_opened"
	// Post-run build/vet checks still failed; task bounced to in_progress.
	MoveReasonVerificationFailed = "verification_failed"
	// Either a real deploy succeeded, or no workflow was mapped and the
	// pipeline was skipped — which is on the pipeline row, not this code.
	MoveReasonDeployReleased = "deploy_released"
	// The repository has no deploy_target rows, so merging the pull request
	// already was the whole release — no deploy pipeline runs.
	MoveReasonMergeReleasedNoDeployTarget = "merge_released_no_deploy_target"
	// The component's delivery mode is none: the merge was the release.
	MoveReasonMergeReleasedNoDelivery = "merge_released_no_delivery"
	// The task's release was verified in production and finished.
	MoveReasonReleaseVerified = "release_verified"
	// The task's release was rolled back; its change is reverted on the
	// default branch and the task goes back for rework.
	MoveReasonReleaseRolledBack = "release_rolled_back"
	MoveReasonReconciled        = "reconciled"
	MoveReasonQuestionAnswered  = "question_answered"
	MoveReasonQuotaRenewed      = "quota_renewed"
	// The resume half of quota_renewed's park; it also releases the deploy
	// watch and work order. Stays "device_free" because rows already carry it
	// and renaming would rewrite history.
	MoveReasonResourceFree = "device_free"
	// The PARK half of quota_renewed; without it the card reached blocked with
	// no board event at all.
	MoveReasonQuotaExhausted = "quota_exhausted"
	// The PARK half of resume; a separate constant so the move INTO blocked
	// would not print "device free".
	MoveReasonResourceBlocked = "resource_blocked"
	// The SAME commit failed CI again after the board already bounced it once,
	// so the card parked instead of cycling a second time. A loop brake, not a
	// build verdict.
	MoveReasonPipelineLoopParked = "pipeline_loop_parked"
	// need_revision for the Nth time with no human in between; the developer
	// was not dispatched again and the card was parked for a person.
	MoveReasonReviewLoopParked = "review_loop_parked"
	// The reconciler retried an unsettled-criteria failure up to its limit with
	// no human in between; its own reason because no need_revision entries are
	// involved — just a run-status streak.
	MoveReasonCriteriaLoopParked = "criteria_loop_parked"
	// The assignee's Mac is not connected; the one park a person can clear by
	// opening a laptop.
	MoveReasonRunnerOffline = "runner_offline"
	// The RESUME half of runner_offline.
	MoveReasonRunnerAttached = "runner_attached"
)

type BoardEvent struct {
	ID           uuid.UUID       `json:"id"`
	RepositoryID uuid.UUID       `json:"repository_id"`
	TaskID       uuid.UUID       `json:"task_id"`
	EventType    BoardEventType  `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	// The human who caused this event, when one did; nil for agent/system
	// events and for actions that arrived without a gateway.
	ActorUserID *string   `json:"actor_user_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type TaskComment struct {
	ID         uuid.UUID `json:"id"`
	TaskID     uuid.UUID `json:"task_id"`
	AuthorType string    `json:"author_type"`
	AuthorID   string    `json:"author_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	// Resolved at read time from the agent roster, so a renamed agent shows
	// its current name on old comments.
	AuthorName  string  `json:"author_name,omitempty"`
	ActorUserID *string `json:"actor_user_id,omitempty"`
}

type CreateTaskCommentRequest struct {
	Content    string `json:"content"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorID   string `json:"author_id,omitempty"`
}

type TaskAgentRun struct {
	ID            uuid.UUID  `json:"id"`
	TaskID        uuid.UUID  `json:"task_id"`
	AgentID       uuid.UUID  `json:"agent_id"`
	BoardEventID  uuid.UUID  `json:"board_event_id"`
	SessionRunID  *uuid.UUID `json:"session_run_id,omitempty"`
	Status        string     `json:"status"`
	Summary       string     `json:"summary"`
	WorkspacePath string     `json:"workspace_path,omitempty"`
	// What the run's tools actually did; they outlive it so the next run does
	// not walk into the same wall and the tool_error_rate KPI sums them.
	ToolCalls    int    `json:"tool_calls"`
	ToolErrors   int    `json:"tool_errors"`
	ErrorPattern string `json:"error_pattern,omitempty"`
	// Same contract as Usage: CacheRead/CacheWrite tokens are subsets of
	// PromptTokens, not additions; embedding calls are excluded.
	LLMCalls         int   `json:"llm_calls"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	// Belong to runs executed by a local agent CLI; on the row rather than in
	// memory because a parked run must survive a restart. Replayed with
	// `--resume`; QuotaResumeAt is NULL on every run never parked.
	CLISessionID  string     `json:"cli_session_id,omitempty"`
	QuotaResumeAt *time.Time `json:"quota_resume_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ActivityItem struct {
	ID           uuid.UUID       `json:"id"`
	Kind         string          `json:"kind"`
	RepositoryID uuid.UUID       `json:"repository_id,omitempty"`
	TaskID       uuid.UUID       `json:"task_id,omitempty"`
	AgentID      *uuid.UUID      `json:"agent_id,omitempty"`
	Status       string          `json:"status,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	EventType    string          `json:"event_type,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

const (
	TaskAgentRunStatusPending   = "pending"
	TaskAgentRunStatusRunning   = "running"
	TaskAgentRunStatusCompleted = "completed"
	TaskAgentRunStatusFailed    = "failed"
	// Terminal and deliberately distinct from failed: the reconciler leaves it
	// alone and the next run does not inherit it as an error pattern.
	TaskAgentRunStatusCancelled = "cancelled"
)

func TaskAgentRunIsTerminal(status string) bool {
	switch status {
	case TaskAgentRunStatusCompleted, TaskAgentRunStatusFailed, TaskAgentRunStatusCancelled:
		return true
	}
	return false
}

var (
	// A cancel request for a run that already stopped — the caller races the
	// run, so this is expected.
	ErrRunNotLive = errors.New("agent run is not pending or running")
	// A re-run request for a run that is still going.
	ErrRunNotTerminal = errors.New("agent run has not finished yet")
	// A re-run request on a blocked task: recovery is moving it to a column,
	// which the listening agent picks up; rerunning would leave the block
	// dangling.
	ErrTaskBlockedForRerun = errors.New("task is blocked: move it to a column instead")
)
