package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrTaskAlreadyClaimed is a claim on a task another agent already holds. The
// claim update only takes unassigned rows, so "no rows" alone could not tell
// this apart from a missing task.
var ErrTaskAlreadyClaimed = errors.New("task is already assigned to another agent")

// ErrTaskOutsideRepository refuses a task-scoped tool call naming a task in a
// repository other than the run's; the binding is a containment boundary, so a
// run talked into naming a planted task must not act on its repository.
var ErrTaskOutsideRepository = errors.New("board task belongs to a different repository than this run")

type TaskType string

const (
	TaskTypeTask      TaskType = "task"
	TaskTypeAnaliz    TaskType = "analiz"
	TaskTypeBug       TaskType = "bug"
	TaskTypeTechnical TaskType = "technical"
)

func ValidTaskType(t TaskType) bool {
	switch t {
	case TaskTypeTask, TaskTypeAnaliz, TaskTypeBug, TaskTypeTechnical:
		return true
	default:
		return false
	}
}

// TaskKeyPrefix is the letter a task's key starts with: T-1 for work, B-1 for
// a bug, A-1 for an analysis. Each prefix counts on its own, so the three
// sequences do not interleave.
func TaskKeyPrefix(t TaskType) string {
	switch t {
	case TaskTypeBug:
		return "B"
	case TaskTypeAnaliz:
		return "A"
	case TaskTypeTechnical:
		return "TC"
	default:
		return "T"
	}
}

// TaskTypeForKeyPrefix is TaskKeyPrefix backwards. Unknown prefixes report
// false rather than guessing "task".
func TaskTypeForKeyPrefix(prefix string) (TaskType, bool) {
	switch strings.ToUpper(strings.TrimSpace(prefix)) {
	case "T":
		return TaskTypeTask, true
	case "B":
		return TaskTypeBug, true
	case "A":
		return TaskTypeAnaliz, true
	case "TC":
		return TaskTypeTechnical, true
	default:
		return "", false
	}
}

func FormatTaskKey(t TaskType, number int) string {
	return fmt.Sprintf("%s-%d", TaskKeyPrefix(t), number)
}

type TaskPriority string

const (
	TaskPriorityLow      TaskPriority = "low"
	TaskPriorityMedium   TaskPriority = "medium"
	TaskPriorityHigh     TaskPriority = "high"
	TaskPriorityCritical TaskPriority = "critical"
)

func ValidTaskPriority(p TaskPriority) bool {
	switch p {
	case TaskPriorityLow, TaskPriorityMedium, TaskPriorityHigh, TaskPriorityCritical:
		return true
	default:
		return false
	}
}

type TaskRelationType string

const (
	// TaskRelationBlocks is a WORK-order statement: the SOURCE must be finished
	// before the TARGET may be worked. Its arrow points opposite to the other
	// relations — the direction migration 022 wrote — so the tools accept
	// `blocked_by` (the blockers), never an agent-facing "blocks".
	TaskRelationBlocks TaskRelationType = "blocks"
	// TaskRelationDeployDependsOn is a shipping order, not a planning one: the
	// TARGET must be live before the SOURCE may ship.
	TaskRelationDeployDependsOn TaskRelationType = "deploy_depends_on"
	// TaskRelationDerivedFrom is provenance: the SOURCE was opened out of the
	// TARGET's investigation, so the target's documents are the source's
	// specification. Direction matches deploy_depends_on: source is the
	// implementation task, target the analiz task it came from.
	TaskRelationDerivedFrom TaskRelationType = "derived_from"
	// TaskRelationDiscoveredFrom is weaker provenance than derived_from: "found
	// while working on", not "specified by". It is written automatically when
	// create_board_task runs inside a task run; only derived_from feeds a spec
	// into the new run's context.
	TaskRelationDiscoveredFrom TaskRelationType = "discovered_from"
)

func ValidTaskRelationType(t TaskRelationType) bool {
	switch t {
	case TaskRelationBlocks, TaskRelationDeployDependsOn, TaskRelationDerivedFrom, TaskRelationDiscoveredFrom:
		return true
	default:
		return false
	}
}

// ErrCriterionNotFound is returned when a criterion id no longer resolves to
// a row — usually the ids were reassigned by a full acceptance-criteria
// replace since the agent last listed them.
var ErrCriterionNotFound = errors.New("acceptance criterion not found")

type AcceptanceCriterion struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	Text      string    `json:"text"`
	Position  int       `json:"position"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
	// Canceled is the third answer, never a synonym for Completed: deliberately
	// dropped, with CancelReason saying by what argument.
	Canceled     bool   `json:"canceled"`
	CancelReason string `json:"cancel_reason,omitempty"`
	// Checks are QA's and PM's independent verdicts; a criterion is only
	// accepted when every role has approved it.
	Checks []CriterionCheck `json:"checks,omitempty"`
}

// Settled reports whether this criterion still owes an answer; an open one is
// what holds the hand-off.
func (c AcceptanceCriterion) Settled() bool { return c.Completed || c.Canceled }

// CriterionReviewRole names who is verifying a criterion; derived from the
// column the task is in when the verdict is recorded, never from the agent's
// self-description.
type CriterionReviewRole string

const (
	CriterionReviewRoleQA CriterionReviewRole = "qa"
	CriterionReviewRolePM CriterionReviewRole = "pm"
)

// CriterionCheck is one role's verdict on one acceptance criterion; a rejection
// must say why in Note.
type CriterionCheck struct {
	ID          uuid.UUID           `json:"id"`
	CriterionID uuid.UUID           `json:"criterion_id"`
	Role        CriterionReviewRole `json:"role"`
	AgentID     *uuid.UUID          `json:"agent_id,omitempty"`
	Approved    bool                `json:"approved"`
	Note        string              `json:"note,omitempty"`
	CheckedAt   time.Time           `json:"checked_at"`
	// VerifiedSHA is the task branch's HEAD at the moment of this verdict.
	VerifiedSHA string `json:"verified_sha,omitempty"`
}

// ReleasedBoardWindow is how long a released task stays on the board before
// moving to the released archive — nothing is deleted, only hidden.
const ReleasedBoardWindow = 7 * 24 * time.Hour

type AcceptanceCriterionInput struct {
	Text      string `json:"text"`
	Position  int    `json:"position,omitempty"`
	Completed bool   `json:"completed,omitempty"`
}

type TaskRelation struct {
	ID           uuid.UUID        `json:"id"`
	SourceTaskID uuid.UUID        `json:"source_task_id"`
	TargetTaskID uuid.UUID        `json:"target_task_id"`
	RelationType TaskRelationType `json:"relation_type"`
	TargetKey    string           `json:"target_key,omitempty"`
	// TargetTitle / SourceKey / SourceTitle are joined in for display; the
	// source fields are filled only by TARGET-end reads (ListBlockedBy).
	TargetTitle string    `json:"target_title,omitempty"`
	SourceKey   string    `json:"source_key,omitempty"`
	SourceTitle string    `json:"source_title,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func RelationLabel(key, title string, id uuid.UUID) string {
	key = strings.TrimSpace(key)
	title = strings.TrimSpace(title)
	switch {
	case key != "" && title != "":
		return key + " (" + title + ")"
	case key != "":
		return key
	case title != "":
		return title
	default:
		return id.String()
	}
}

type TaskRelationInput struct {
	TargetTaskID uuid.UUID        `json:"target_task_id,omitempty"`
	TargetKey    string           `json:"target_key,omitempty"`
	RelationType TaskRelationType `json:"relation_type"`
}

type TaskDocument struct {
	ID            uuid.UUID `json:"id"`
	TaskID        uuid.UUID `json:"task_id"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	Position      int       `json:"position"`
	CreatedByType string    `json:"created_by_type"`
	CreatedByID   string    `json:"created_by_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateTaskDocumentRequest struct {
	Title         string `json:"title"`
	Content       string `json:"content,omitempty"`
	Position      int    `json:"position,omitempty"`
	CreatedByType string `json:"created_by_type,omitempty"`
	CreatedByID   string `json:"created_by_id,omitempty"`
}

type UpdateTaskDocumentRequest struct {
	Title    *string `json:"title,omitempty"`
	Content  *string `json:"content,omitempty"`
	Position *int    `json:"position,omitempty"`
}

type BoardTask struct {
	ID                   uuid.UUID  `json:"id"`
	RepositoryID         uuid.UUID  `json:"repository_id"`
	Key                  string     `json:"key"`
	TaskNumber           int        `json:"task_number"`
	Title                string     `json:"title"`
	TaskType             TaskType   `json:"task_type"`
	Description          string     `json:"description"`
	TechnicalDescription string     `json:"technical_description"`
	InitiativeProjectID  *uuid.UUID `json:"initiative_project_id,omitempty"`
	// ComponentID narrows the task to one component of a monorepo: its brief,
	// its required checks. nil = the whole repository.
	ComponentID        *uuid.UUID            `json:"component_id,omitempty"`
	Column             TaskColumn            `json:"column"`
	Position           int                   `json:"position"`
	Priority           TaskPriority          `json:"priority"`
	CreatedBy          string                `json:"created_by"`
	AssigneeAgentID    *uuid.UUID            `json:"assignee_agent_id,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	AcceptanceCriteria []AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	// TestCases is the full round the task was given, including cases rejected
	// as not valid. Single-task read only.
	TestCases []TaskTestCase `json:"test_cases,omitempty"`
	// Relations are the relations this task is the SOURCE of.
	Relations []TaskRelation `json:"relations,omitempty"`
	// BlockedBy is where this task is the TARGET of a blocks relation — what
	// must finish before it may be worked. Kept separate from Relations because
	// it is read from the other end. Single-task detail path only.
	BlockedBy []TaskRelation `json:"blocked_by,omitempty"`
	Documents []TaskDocument `json:"documents,omitempty"`
	// Attachments are binary files linked to the task; single-task detail path
	// only, never in bulk lists.
	Attachments          []AttachmentMeta `json:"attachments,omitempty"`
	LatestPipelineStatus string           `json:"latest_pipeline_status,omitempty"`
	// LatestPipelineGateReason is why the code-review gate opened without a
	// green build (a PipelineGateReason* code), or "" for an ordinary pipeline.
	LatestPipelineGateReason string `json:"latest_pipeline_gate_reason,omitempty"`
	// Set while an agent waits on a human answer; answering clears the block.
	BlockedQuestion  string     `json:"blocked_question,omitempty"`
	BlockedSessionID *uuid.UUID `json:"blocked_session_id,omitempty"`
	BlockedAt        *time.Time `json:"blocked_at,omitempty"`
	// ClarificationSessionID is the chat all this task's questions share; it
	// survives the answer so follow-ups continue the same thread.
	ClarificationSessionID *uuid.UUID `json:"clarification_session_id,omitempty"`
	// BlockedOriginColumn is where the task was when it blocked; answering
	// returns it there.
	BlockedOriginColumn TaskColumn `json:"blocked_origin_column,omitempty"`
	// BlockedResource names the shared resource this task is queued for, parked
	// by contention rather than a question. Always serialized, "" when not
	// parked — the board badge is rendered off this field.
	BlockedResource string `json:"blocked_resource"`
	// BlockedResumeAt is when a parked task is expected to come back (the run's
	// recorded quota_resume_at). Null for every block without a predictable
	// time, which is all except the Claude Code usage limit.
	BlockedResumeAt *time.Time `json:"blocked_resume_at"`
	// HasMigration is detected from the task's changed files, not declared by
	// the agent.
	HasMigration    bool       `json:"has_migration"`
	StageVerifiedAt *time.Time `json:"stage_verified_at,omitempty"`
	// VerifiedSHA is the commit the branch pointed at when the task reached
	// done; the release gate refuses to ship anything else, empty blocks it.
	VerifiedSHA string `json:"verified_sha,omitempty"`
	// ColumnEnteredAt is when the task entered its current column; nil for
	// tasks that predate the span ledger.
	ColumnEnteredAt *time.Time `json:"column_entered_at,omitempty"`
	// BeforeDeploy / AfterDeploy / RollbackPlan are the release runbook, each
	// surfaced at the moment it applies. Pointers: nil means "not written".
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
	// BeforeDeployConfirmedAt is when a human confirmed the BeforeDeploy steps
	// were done; nothing ships a task whose steps are written but unconfirmed.
	// Editing BeforeDeploy clears it.
	BeforeDeployConfirmedAt *time.Time `json:"before_deploy_confirmed_at,omitempty"`
	// PRURL / PRNumber are the pull request this task's branch is reviewed in,
	// recorded when one is opened, not re-derived on demand. The number may be
	// 0 while the URL is set: an unrecognised html_url is still stored.
	PRURL    string `json:"pr_url,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
	// MergeCommitSHA is the squash commit this task's PR produced on the
	// default branch, written by merge_task_pull_request and nothing else. Once
	// set, done stops waking the QA agent; a follow-up deploy should key off
	// this SHA, not the repository's moving default branch.
	MergeCommitSHA string `json:"merge_commit_sha,omitempty"`
}

type CreateBoardTaskRequest struct {
	Title                string                     `json:"title"`
	TaskType             TaskType                   `json:"task_type,omitempty"`
	Description          string                     `json:"description,omitempty"`
	TechnicalDescription string                     `json:"technical_description,omitempty"`
	InitiativeProjectID  *uuid.UUID                 `json:"initiative_project_id,omitempty"`
	ComponentID          *uuid.UUID                 `json:"component_id,omitempty"`
	Column               TaskColumn                 `json:"column,omitempty"`
	Priority             TaskPriority               `json:"priority,omitempty"`
	CreatedBy            string                     `json:"created_by,omitempty"`
	AssigneeAgentID      *uuid.UUID                 `json:"assignee_agent_id,omitempty"`
	AcceptanceCriteria   []AcceptanceCriterionInput `json:"acceptance_criteria,omitempty"`
	// Relations are written with the NEW task as their source.
	Relations []TaskRelationInput `json:"relations,omitempty"`
	// BlockedBy is written with the new task as the TARGET (the direction
	// TaskRelationBlocks is stored in), so it cannot ride in Relations.
	BlockedBy []TaskRelationInput         `json:"blocked_by,omitempty"`
	Documents []CreateTaskDocumentRequest `json:"documents,omitempty"`
	// Deploy runbook; nil leaves the column NULL.
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
}

type UpdateBoardTaskRequest struct {
	Title                *string    `json:"title,omitempty"`
	TaskType             *TaskType  `json:"task_type,omitempty"`
	Description          *string    `json:"description,omitempty"`
	TechnicalDescription *string    `json:"technical_description,omitempty"`
	InitiativeProjectID  *uuid.UUID `json:"initiative_project_id,omitempty"`
	// ComponentID: omitted leaves it, null clears it, a value sets it.
	ComponentID Nullable[uuid.UUID] `json:"component_id,omitempty"`
	Column      *TaskColumn         `json:"column,omitempty"`
	Position    *int                `json:"position,omitempty"`
	Priority    *TaskPriority       `json:"priority,omitempty"`
	// AssigneeAgentID reads three spellings: omitted (unchanged), explicit
	// null (unassign), a value (assign; "" also unassigns). Nullable rather
	// than a plain pointer so omitted and null stay distinct.
	AssigneeAgentID Nullable[uuid.UUID] `json:"assignee_agent_id,omitempty"`
	// Deploy runbook: nil leaves the stored value alone, a pointer to ""
	// clears it.
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
	// DeployDependsOn replaces the deploy_depends_on relations wholesale when
	// non-nil; an explicit [] clears them.
	DeployDependsOn *[]TaskRelationInput `json:"deploy_depends_on,omitempty"`
	// BlockedBy ADDS work-order blockers; it does not replace them (unlike
	// DeployDependsOn), so one planner cannot silently drop another's.
	BlockedBy []TaskRelationInput `json:"blocked_by,omitempty"`
	// Actor decides review-gate behaviour; never accepted from the client —
	// the transport sets it.
	Actor TaskActor `json:"-"`
	// ActorAgentID identifies which agent moved the task when Actor is
	// TaskActorAgent; the dispatcher uses it to avoid re-dispatching the agent
	// whose own tool call produced the event. Set by the tool layer.
	ActorAgentID *uuid.UUID `json:"-"`
	// SystemReason is one of the domain.MoveReason* constants explaining a
	// TaskActorSystem move; it rides into the board event payload.
	SystemReason string `json:"-"`
}

type TaskActor string

const (
	// TaskActorSystem is the zero value.
	TaskActorSystem TaskActor = ""
	TaskActorAgent  TaskActor = "agent"
	TaskActorHuman  TaskActor = "human"
)

// BeforeDeployPending reports steps a human still has to perform before this
// task may ship.
func (t BoardTask) BeforeDeployPending() bool {
	return t.BeforeDeploy != nil && strings.TrimSpace(*t.BeforeDeploy) != "" && t.BeforeDeployConfirmedAt == nil
}
