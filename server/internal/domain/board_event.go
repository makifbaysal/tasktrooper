package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrTaskAgentRunNotFound reports a run row that no longer exists. Deleting a
// task cascades to its runs, so an in-flight run whose task a human deletes
// hits this on its final update — an expected outcome, not a failure.
var ErrTaskAgentRunNotFound = errors.New("task agent run not found")

type BoardEventType string

const (
	BoardEventTaskCreated   BoardEventType = "task.created"
	BoardEventTaskMoved     BoardEventType = "task.moved"
	BoardEventTaskCommented BoardEventType = "task.commented"
	BoardEventTaskAssigned  BoardEventType = "task.assigned"
	// BoardEventTaskRunCancelled and BoardEventTaskRerunRequested are written
	// straight to the event store, never through the Dispatcher: dispatching
	// them would fan a fresh agent out onto the very task a human just stopped,
	// and would re-run every agent on the column instead of the one asked for.
	BoardEventTaskRunCancelled   BoardEventType = "task.run_cancelled"
	BoardEventTaskRerunRequested BoardEventType = "task.rerun_requested"
)

// Board event payload keys and system-move reasons. A move made by the
// control plane itself (pipeline hand-off, verification bounce, reconciler)
// carries no actor_agent_id, which the UI used to render as "by User" — the
// human saw moves they never made. These two keys say "the system did it, and
// this is why"; the UI maps MoveReason* to a sentence.
const (
	// EventPayloadActor is "agent" | "human" | "system".
	EventPayloadActor = "actor"
	// EventPayloadReason explains a system move (one of the MoveReason* below).
	EventPayloadReason = "system_reason"
	// EventPayloadResumedResource names the parked resource a sweeper just
	// released, and it is the ONLY thing that can wake a task in a terminal
	// column for the deploy watch.
	//
	// `done` and `released` dispatch nobody: that suspension is what keeps a
	// finished task finished, and it is the guard that stopped agents being
	// woken onto their own completed work. The deploy watch has to reach into
	// exactly one of those columns and no further, so the carve-out is keyed on
	// a payload only the sweeper writes (board.deployWatchWake) rather than on
	// the task's state — which any comment, update or second move could
	// otherwise reproduce.
	EventPayloadResumedResource = "resumed_resource"
	// EventPayloadPipelineGate names why the code-review gate let this
	// dispatch through WITHOUT a green build behind it — one of the
	// PipelineGateReason* codes. Absent on the ordinary path, where the
	// pipeline really did report.
	//
	// It goes on the event rather than only on the pipeline row because the
	// gate can be skipped when there is no pipeline row at all
	// (require_pipeline_for_review off), and because a dispatch is the thing
	// being explained: the board history has to be able to say "an architect
	// was put on this diff, and this is what stood behind it".
	EventPayloadPipelineGate = "pipeline_gate"

	EventActorAgent  = "agent"
	EventActorHuman  = "human"
	EventActorSystem = "system"

	// MoveReasonPipelinePassed: the QA-gate pipeline succeeded and the task was
	// handed to its column's reviewing agent. Not a column change.
	MoveReasonPipelinePassed = "pipeline_passed"
	// MoveReasonPipelineFailed: the pipeline failed, task bounced to need_revision.
	MoveReasonPipelineFailed = "pipeline_failed"
	// MoveReasonPipelineGateOpened: no pipeline result ever arrived (or GitHub
	// said none could), and the code-review gate was opened anyway so the card
	// could move. Distinct from MoveReasonPipelinePassed on purpose: the two
	// dispatch the same agent, and only one of them means the build was green.
	MoveReasonPipelineGateOpened = "pipeline_gate_opened"
	// MoveReasonVerificationFailed: post-run build/vet checks still failed after
	// the fix attempts, task bounced back to in_progress.
	MoveReasonVerificationFailed = "verification_failed"
	// MoveReasonDeployReleased: a prod/preprod deploy pipeline opened the gate,
	// releasing the task — either a real deploy succeeded, or no workflow is
	// mapped for the repo and the pipeline was skipped. Which one happened is
	// on the pipeline's status/provider, not on this reason code.
	MoveReasonDeployReleased = "deploy_released"
	// MoveReasonMergeReleasedNoDeployTarget: the repository has zero
	// deploy_target rows in any environment, so merging the task's pull
	// request already was the whole release — no deploy pipeline runs.
	// Distinct from MoveReasonDeployReleased, which means a real
	// prod/preprod deploy pipeline succeeded; this one means no deploy
	// exists to run in the first place.
	MoveReasonMergeReleasedNoDeployTarget = "merge_released_no_deploy_target"
	// MoveReasonReconciled: the reconciler revived a task whose run died.
	MoveReasonReconciled = "reconciled"
	// MoveReasonQuestionAnswered: a human answered the clarification a parked
	// task was waiting on, and the task went back into the dispatch queue.
	MoveReasonQuestionAnswered = "question_answered"
	// MoveReasonQuotaRenewed: a task paused on an exhausted budget resumed when
	// the billing period rolled over.
	MoveReasonQuotaRenewed = "quota_renewed"
	// MoveReasonResourceFree: a task parked because a shared resource — the
	// test device — was held by another run, and the sweeper found it free.
	//
	// The value reads narrower than the reason is: it releases the deploy watch
	// and the work order too. It stays "device_free" because rows carrying it
	// are already in board_events on live boards, and renaming it would silently
	// change what those rows say — a cosmetic gain paid for with history.
	MoveReasonResourceFree = "device_free"
	// MoveReasonQuotaExhausted: the local Claude Code subscription hit its usage
	// limit mid-run and the task was parked in `blocked` until the window
	// reopens. It is the PARK half of MoveReasonQuotaRenewed — without it the
	// card moved to blocked with no board event at all, so the timeline showed
	// the task working right up to the moment a sweeper resumed it hours later.
	MoveReasonQuotaExhausted = "quota_exhausted"
	// MoveReasonResourceBlocked: a run stopped because a shared resource — a
	// phone, a running deploy — was held by someone else, and the card was parked
	// until it frees. The PARK half of MoveReasonResourceFree, and a separate
	// constant because reusing the resume reason would have printed "device
	// free" on the move INTO blocked, which is the opposite of what happened.
	MoveReasonResourceBlocked = "resource_blocked"
	// MoveReasonPipelineLoopParked: the SAME commit failed CI again after the
	// board had already sent the task back for it once, so the card was parked
	// in `blocked` instead of being bounced a second time. It is a LOOP brake,
	// not a build verdict: the pipeline result is identical to the one already
	// on the card, and re-reporting it would only restart the cycle.
	MoveReasonPipelineLoopParked = "pipeline_loop_parked"
	// MoveReasonReviewLoopParked: the task entered need_revision for the Nth
	// time (see board.maxReviewLoopEntries) with no human having touched it in
	// between, so the developer was not dispatched again and the card was parked
	// for a person to decide. The agent-only half of the board has demonstrably
	// run out of ways to finish this task on its own.
	MoveReasonReviewLoopParked = "review_loop_parked"
	// MoveReasonRunnerOffline: the assignee's Mac is not connected, so the run
	// that would have happened on it was parked instead. Its own reason rather
	// than MoveReasonResourceBlocked because this is the one park a person can
	// clear in five seconds by opening a laptop, and the timeline is where they
	// find out that is all it needs.
	MoveReasonRunnerOffline = "runner_offline"
	// MoveReasonRunnerAttached: that Mac came back and the sweeper released the
	// card. The RESUME half of MoveReasonRunnerOffline.
	MoveReasonRunnerAttached = "runner_attached"
)

type BoardEvent struct {
	ID           uuid.UUID       `json:"id"`
	RepositoryID uuid.UUID       `json:"repository_id"`
	TaskID       uuid.UUID       `json:"task_id"`
	EventType    BoardEventType  `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	// ActorUserID is the Firebase UID of the human who caused this event,
	// resolved from the signed X-Internal-Actor header. Nil for anything an
	// agent or the system produced, and for any human action that arrived
	// without a gateway in front of it (self-hosted/desktop).
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
	// AuthorName is resolved at read time from the agent roster; it is not
	// stored, so a renamed agent shows its current name on old comments.
	AuthorName string `json:"author_name,omitempty"`
	// ActorUserID is the Firebase UID of the human who wrote this comment,
	// resolved from the signed X-Internal-Actor header. Nil for agent/system
	// comments and for a human comment that arrived without a gateway in
	// front of it (self-hosted/desktop).
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
	// ToolCalls/ToolErrors/ErrorPattern are what the run's tools actually did.
	// They outlive the loop on purpose: the next run reads the pattern so it
	// does not walk into the same wall, and the tool_error_rate KPI sums the
	// counters per agent over a period.
	ToolCalls    int    `json:"tool_calls"`
	ToolErrors   int    `json:"tool_errors"`
	ErrorPattern string `json:"error_pattern,omitempty"`
	// Token usage summed over every LLM chat call this run made — loop turns,
	// history summarization, orchestrator subtasks. Same contract as Usage:
	// PromptTokens is the TOTAL prompt size; CacheReadTokens/CacheWriteTokens
	// are subsets of it, not additions. Embedding calls are excluded, so these
	// read as the run's conversation spend.
	LLMCalls         int   `json:"llm_calls"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	// CLISessionID and QuotaResumeAt belong to runs executed by a local agent
	// CLI rather than by the in-process loop (provider claude_code). They are
	// on the row rather than in memory because the whole point of a parked run
	// is that it survives a restart: the sweeper that resumes it reads these,
	// and a pod that died between the park and the reset would otherwise leave
	// the task blocked forever.
	//
	// CLISessionID is the executor's own session, replayed with `--resume` so
	// the continuation keeps what the first attempt already learned.
	// QuotaResumeAt is when the usage limit is expected to lift; NULL on every
	// run that was never parked, which is nearly all of them.
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
	// TaskAgentRunStatusCancelled is a human stopping the run from the UI. It is
	// terminal and deliberately distinct from failed: the reconciler leaves it
	// alone, and the next run does not inherit it as an error pattern — nothing
	// went wrong, someone changed their mind.
	TaskAgentRunStatusCancelled = "cancelled"
)

// TaskAgentRunIsTerminal reports whether the run has stopped for good.
func TaskAgentRunIsTerminal(status string) bool {
	switch status {
	case TaskAgentRunStatusCompleted, TaskAgentRunStatusFailed, TaskAgentRunStatusCancelled:
		return true
	}
	return false
}

var (
	// ErrRunNotLive is a cancel request for a run that already stopped. The
	// caller races the run itself, so this is an expected outcome, not a bug.
	ErrRunNotLive = errors.New("agent run is not pending or running")
	// ErrRunNotTerminal is a re-run request for a run that is still going.
	ErrRunNotTerminal = errors.New("agent run has not finished yet")
	// ErrTaskBlockedForRerun is a re-run request on a blocked task. Recovery for
	// a blocked task is moving it to a column, which the listening agent picks
	// up on its own; re-running in place would leave the block dangling.
	ErrTaskBlockedForRerun = errors.New("task is blocked: move it to a column instead")
)
