package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrTaskAlreadyClaimed is a claim on a task another agent already holds. The
// claim update only takes rows that are unassigned or assigned to the caller,
// so "no rows" alone could not tell this apart from a missing task — and the
// agent that read "no rows in result set" had nothing to act on but retry.
var ErrTaskAlreadyClaimed = errors.New("task is already assigned to another agent")

// ErrTaskOutsideRepository is a task-scoped tool call naming a task that lives
// in a repository other than the one the run is bound to. The call is refused:
// the binding is a containment boundary — merges, releases and deletes all
// resolve their repository the same way — so a run talked into naming a
// planted task must not act on that task's repository.
//
// It is a typed error because the refusal used to surface as a bare "board
// task not found: <id>", which told the agent nothing and had it retry.
var ErrTaskOutsideRepository = errors.New("board task belongs to a different repository than this run")

type TaskType string

const (
	TaskTypeTask   TaskType = "task"
	TaskTypeAnaliz TaskType = "analiz"
	TaskTypeBug    TaskType = "bug"
)

func ValidTaskType(t TaskType) bool {
	switch t {
	case TaskTypeTask, TaskTypeAnaliz, TaskTypeBug:
		return true
	default:
		return false
	}
}

// TaskKeyPrefix is the letter a task's key starts with: T-1 for work, B-1 for a
// bug, A-1 for an analysis.
//
// The prefix used to be one board-wide setting, so the key said nothing about
// the task. Reading the type off the key is what makes "B-4 is back" or "A-2
// first" legible in a chat, a commit trailer or a branch name without looking
// the task up. Each prefix counts on its own, so the three sequences do not
// interleave.
func TaskKeyPrefix(t TaskType) string {
	switch t {
	case TaskTypeBug:
		return "B"
	case TaskTypeAnaliz:
		return "A"
	default:
		return "T"
	}
}

// TaskTypeForKeyPrefix is TaskKeyPrefix backwards, for resolving a key a human
// or an agent typed. Unknown prefixes report false rather than guessing "task":
// a lookup for "X-1" must fail as unknown, not silently answer with T-1.
func TaskTypeForKeyPrefix(prefix string) (TaskType, bool) {
	switch strings.ToUpper(strings.TrimSpace(prefix)) {
	case "T":
		return TaskTypeTask, true
	case "B":
		return TaskTypeBug, true
	case "A":
		return TaskTypeAnaliz, true
	default:
		return "", false
	}
}

// FormatTaskKey renders the key the whole product refers to a task by.
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
	// before the TARGET may be worked on. Read it as "source blocks target".
	//
	// Its arrow points the opposite way to the two relations below, and that is
	// not an accident that can be tidied away — it is the direction migration
	// 022 wrote, the direction task_relations rows have been stored in ever
	// since, and the direction TaskRelationStore.ListBlockingSources reads (it
	// selects the SOURCES of a target's blocks rows). Flipping it would silently
	// invert every stored row. So the tools never ask an agent to think in it:
	// create_board_task takes `blocked_by`, which names the blockers of the task
	// being created and writes them as source rows here.
	//
	// Enforcement lives in two places. repository.Service.validateMoveAllowed
	// refuses a MOVE into in_progress while a blocker is unfinished — todo is
	// joining the queue, not starting work, so it is never refused there.
	// board.WorkOrder parks the card on ResourceWorkOrder when the DISPATCHER is
	// about to start a run on it anyway (a task created straight into todo, a
	// reconciler sweep, a sweeper resume), which is what actually guards todo.
	// The park is released by board.WorkOrderSweeper once every blocker reaches
	// done/released — or disappears, because deleting a task cascades its
	// relations away.
	TaskRelationBlocks TaskRelationType = "blocks"
	// TaskRelationDeployDependsOn is a shipping-order statement, not a planning
	// one: the TARGET must be live in production before the SOURCE may be
	// released. Distinct from blocks, which is about who may start work. Two
	// tasks developed in parallel (neither blocking the other) can still have a
	// hard deploy order — the API before the client that calls it.
	//
	// Direction: source depends on target, so the TARGET ships first. That is
	// what repository.Service.deployDependencyGate reads off ListBySource, and
	// what the `deploy_depends_on` tool argument means ("this task ships AFTER
	// those").
	TaskRelationDeployDependsOn TaskRelationType = "deploy_depends_on"
	// TaskRelationDerivedFrom is a provenance statement: the SOURCE was opened
	// out of the TARGET's investigation, so the target's documents are the
	// source's specification.
	//
	// It exists because neither ordering relation can carry it. `blocks` would
	// make the analysis gate when the implementation may start (it already has,
	// by the time the tasks are created — the human approved it in
	// analiz_review), and `deploy_depends_on` would refuse to release the
	// implementation until an analiz task that ships no code somehow reached
	// production. Nor is there a parent/link column on board_tasks to reuse:
	// InitiativeProjectID names an initiative, not a task. So this is a third
	// type on the table that already models (source, target, type) — no new
	// table, no new read path, and it appears in task.Relations (and therefore
	// in the API payload) the moment it is written.
	//
	// Direction matches deploy_depends_on so both read the same way off
	// task.Relations: the SOURCE is the implementation task, the TARGET is the
	// analiz task it came from.
	TaskRelationDerivedFrom TaskRelationType = "derived_from"
)

// ValidTaskRelationType reports whether a relation type is one the schema's
// CHECK constraint accepts. Rejecting a bad type here turns a 500 from the
// database into an actionable error naming the field.
func ValidTaskRelationType(t TaskRelationType) bool {
	switch t {
	case TaskRelationBlocks, TaskRelationDeployDependsOn, TaskRelationDerivedFrom:
		return true
	default:
		return false
	}
}

type AcceptanceCriterion struct {
	ID        uuid.UUID `json:"id"`
	TaskID    uuid.UUID `json:"task_id"`
	Text      string    `json:"text"`
	Position  int       `json:"position"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"created_at"`
	// Canceled is the third answer, and it is never a synonym for Completed:
	// the criterion was deliberately dropped, and CancelReason says by what
	// argument. Without it "decided against" and "forgotten" were the same
	// unticked row, and both parked the task in front of the criteria gate
	// forever — the only way out being to tick something nobody built.
	Canceled     bool   `json:"canceled"`
	CancelReason string `json:"cancel_reason,omitempty"`
	// Checks are the reviewers' own verdicts on this criterion. Completed is
	// the implementer's claim; QA and PM each verify independently and a
	// criterion is only truly accepted when every role has approved it.
	Checks []CriterionCheck `json:"checks,omitempty"`
}

// Settled reports whether this criterion still owes an answer. A ticked one and
// a cancelled one are both settled; an open one is what holds the hand-off.
func (c AcceptanceCriterion) Settled() bool { return c.Completed || c.Canceled }

// CriterionReviewRole names who is verifying a criterion. The role is derived
// from the column the task is in when the verdict is recorded, never from the
// agent's self-description.
type CriterionReviewRole string

const (
	CriterionReviewRoleQA CriterionReviewRole = "qa"
	CriterionReviewRolePM CriterionReviewRole = "pm"
)

// CriterionCheck is one role's verdict on one acceptance criterion. A
// rejection must say why: the note is what the developer reads in
// need_revision and what the UI shows next to the failed step.
type CriterionCheck struct {
	ID          uuid.UUID           `json:"id"`
	CriterionID uuid.UUID           `json:"criterion_id"`
	Role        CriterionReviewRole `json:"role"`
	AgentID     *uuid.UUID          `json:"agent_id,omitempty"`
	Approved    bool                `json:"approved"`
	Note        string              `json:"note,omitempty"`
	CheckedAt   time.Time           `json:"checked_at"`
}

// ReleasedBoardWindow is how long a released task stays on the board. The
// released column is where finished work piles up: it is kept because a card
// that shipped yesterday is still being talked about, and it stops being useful
// long before it stops accumulating. After this it leaves the board and is
// found through the released archive instead — nothing is deleted, only hidden
// from the working view.
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
	// TargetTitle / SourceKey / SourceTitle are joined in for display. A
	// relation rendered as two UUIDs is unreadable on a card and unquotable in
	// a generated deploy note; "T-12 (API migration)" is what both need.
	//
	// SourceKey/SourceTitle are filled only by the reads that look at a
	// relation from its TARGET end (ListBlockedBy), because that is the only
	// direction where the source is the interesting task.
	TargetTitle string    `json:"target_title,omitempty"`
	SourceKey   string    `json:"source_key,omitempty"`
	SourceTitle string    `json:"source_title,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// RelationLabel renders the far end of a relation the way a human reads it:
// "T-12 (API migration)", falling back to the key alone and then to the UUID.
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
	ID                   uuid.UUID             `json:"id"`
	RepositoryID         uuid.UUID             `json:"repository_id"`
	Key                  string                `json:"key"`
	TaskNumber           int                   `json:"task_number"`
	Title                string                `json:"title"`
	TaskType             TaskType              `json:"task_type"`
	Description          string                `json:"description"`
	TechnicalDescription string                `json:"technical_description"`
	InitiativeProjectID  *uuid.UUID            `json:"initiative_project_id,omitempty"`
	Column               TaskColumn            `json:"column"`
	Position             int                   `json:"position"`
	Priority             TaskPriority          `json:"priority"`
	CreatedBy            string                `json:"created_by"`
	AssigneeAgentID      *uuid.UUID            `json:"assignee_agent_id,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
	AcceptanceCriteria   []AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	// TestCases is the round the task was actually given: every case derived
	// from the request with its verdict, including the ones rejected as not
	// valid. Filled on the single-task read, like criteria; the board's list
	// endpoint does not carry it.
	TestCases []TaskTestCase `json:"test_cases,omitempty"`
	// Relations are the relations this task is the SOURCE of: what it ships
	// after (deploy_depends_on), what analysis it came out of (derived_from),
	// and which tasks it blocks (blocks).
	Relations []TaskRelation `json:"relations,omitempty"`
	// BlockedBy is the other end of the blocks graph: the relations whose
	// TARGET is this task, i.e. the tasks that must be finished before this one
	// may be worked on. It is a separate field rather than more entries in
	// Relations because the two are read in opposite directions and mixing them
	// would make `relations` mean "edges touching this task", which no consumer
	// could interpret without re-deriving which end it was looking at.
	//
	// Populated on the single-task detail path only, like Attachments.
	BlockedBy []TaskRelation `json:"blocked_by,omitempty"`
	Documents []TaskDocument `json:"documents,omitempty"`
	// Attachments are binary files (images/documents) linked to the task.
	// Populated on the single-task detail path only, never in bulk lists.
	Attachments          []AttachmentMeta `json:"attachments,omitempty"`
	LatestPipelineStatus string           `json:"latest_pipeline_status,omitempty"`
	// LatestPipelineGateReason is why the code-review gate opened without a
	// green build (a PipelineGateReason* code), or "" for an ordinary pipeline.
	// It travels beside the status because a card whose spinner stopped needs
	// to say WHY it stopped: "success" and "we gave up waiting" look identical
	// on a board that only renders the status.
	LatestPipelineGateReason string `json:"latest_pipeline_gate_reason,omitempty"`
	// Set while an agent is waiting on a human answer: the question it asked and
	// the chat session it asked in. Answering there clears the block and
	// re-dispatches the task.
	BlockedQuestion  string     `json:"blocked_question,omitempty"`
	BlockedSessionID *uuid.UUID `json:"blocked_session_id,omitempty"`
	BlockedAt        *time.Time `json:"blocked_at,omitempty"`
	// ClarificationSessionID is the chat this task asks ALL its questions in.
	// Unlike BlockedSessionID it survives the answer, so a follow-up question
	// continues the same thread instead of opening a new chat that cannot see
	// what was already asked.
	ClarificationSessionID *uuid.UUID `json:"clarification_session_id,omitempty"`
	// BlockedOriginColumn is the column the task was working in when it blocked;
	// answering returns it there rather than to a guessed default.
	BlockedOriginColumn TaskColumn `json:"blocked_origin_column,omitempty"`
	// BlockedResource names the shared resource this task is queued for, when
	// it was parked by contention rather than by a question or a human. It is
	// what the device sweeper claims by; see domain/resource_block.go.
	//
	// Always serialized, "" when the task is not parked: the board renders the
	// badge off this field, and an omitted key made "not parked" and "parked on
	// something this client does not know about" the same absence.
	BlockedResource string `json:"blocked_resource"`
	// BlockedResumeAt is when a parked task is expected to come back — the reset
	// time the run that parked recorded on its own row
	// (task_agent_runs.quota_resume_at), which is what lets the card say
	// "resumes at 14:30" instead of "blocked, indefinitely".
	//
	// Null for every block that has no predictable time, which is all of them
	// except the Claude Code usage limit: a phone frees when whoever holds it is
	// done, a deploy when GitHub says so, a work order when its blockers land.
	// Guessing a clock for those would be worse than showing none.
	BlockedResumeAt *time.Time `json:"blocked_resume_at"`
	// HasMigration is detected from the task's changed files, not declared by
	// the agent: a task that touches the schema may not reach production
	// before a stage deploy has actually applied it.
	HasMigration bool `json:"has_migration"`
	// StageVerifiedAt stamps the stage deploy that ran this task's code (and
	// its migration) successfully.
	StageVerifiedAt *time.Time `json:"stage_verified_at,omitempty"`
	// VerifiedSHA is the commit the task's branch pointed at when it reached
	// done — the code the review/QA/UAT chain actually signed off on. The
	// release gate re-resolves the branch before dispatching a prod deploy and
	// refuses to ship anything else. Empty means "never stamped" (or a
	// withdrawn sign-off), which blocks the release rather than waving it
	// through: a state alone never proves which code is about to ship.
	VerifiedSHA string `json:"verified_sha,omitempty"`
	// ColumnEnteredAt is when the task entered the column it is in now, read
	// from its open span. Nil for tasks that predate the span ledger.
	ColumnEnteredAt *time.Time `json:"column_entered_at,omitempty"`
	// BeforeDeploy / AfterDeploy / RollbackPlan are the release runbook for
	// this task, as free markdown. They used to be a comment the agent was
	// asked to write before calling trigger_release; as fields the release path
	// can read them back and surface each at the moment it applies — the
	// pre-deploy checklist and rollback plan when the deploy is dispatched, the
	// post-deploy steps when production reports success. Nil means "not
	// written", which is why these are pointers rather than strings.
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
	// PRURL / PRNumber are the pull request this task's branch is reviewed in,
	// recorded the moment one is opened (see BoardTaskStore.SetTaskPullRequest)
	// instead of being re-derived from a working copy plus a GitHub round-trip
	// every time someone asks. Empty / 0 means "no PR known yet", which is why
	// these are plain values rather than pointers: there is no third state to
	// model, and every reader treats them as "name the PR if we have one".
	//
	// The number can be 0 while the URL is set — an html_url in a shape
	// ParsePullRequestNumber does not recognise is still stored, because a link
	// a human can open is worth more than a clean parse.
	PRURL    string `json:"pr_url,omitempty"`
	PRNumber int    `json:"pr_number,omitempty"`
	// MergeCommitSHA is the squash commit the task's pull request produced on
	// the default branch, written by the merge (merge_task_pull_request) and by
	// nothing else. Empty means "not merged from here yet", which is what the
	// dispatcher reads to decide whether a task landing in done still has a PR
	// waiting for someone: once this is set, done stops waking the QA agent, so
	// this field is also the only thing standing between the done column and a
	// re-dispatch loop.
	//
	// It is the commit the FOLLOW-UP deploy should watch and release. The
	// release path today dispatches its prod workflow on the repository's
	// default branch (see repository.TriggerRelease / deployRef), which is a
	// known drift bug — todo.md: "TriggerRelease main drift'ini yakalamıyor" —
	// because the default branch moves on with everyone else's merges while the
	// gate only ever checks THIS task's identity. Once the deploy watch lands,
	// it should key off this SHA rather than off a branch name.
	MergeCommitSHA string `json:"merge_commit_sha,omitempty"`
}

type CreateBoardTaskRequest struct {
	Title                string                     `json:"title"`
	TaskType             TaskType                   `json:"task_type,omitempty"`
	Description          string                     `json:"description,omitempty"`
	TechnicalDescription string                     `json:"technical_description,omitempty"`
	InitiativeProjectID  *uuid.UUID                 `json:"initiative_project_id,omitempty"`
	Column               TaskColumn                 `json:"column,omitempty"`
	Priority             TaskPriority               `json:"priority,omitempty"`
	CreatedBy            string                     `json:"created_by,omitempty"`
	AssigneeAgentID      *uuid.UUID                 `json:"assignee_agent_id,omitempty"`
	AcceptanceCriteria   []AcceptanceCriterionInput `json:"acceptance_criteria,omitempty"`
	// Relations are written with the NEW task as their source: deploy_depends_on
	// (this ships after those) and derived_from (this came out of that analysis).
	Relations []TaskRelationInput `json:"relations,omitempty"`
	// BlockedBy is written with the new task as the TARGET and each entry as the
	// source, because that is the direction TaskRelationBlocks is stored in. It
	// cannot ride in Relations for that reason alone — every entry there shares
	// one source, and these do not.
	BlockedBy []TaskRelationInput         `json:"blocked_by,omitempty"`
	Documents []CreateTaskDocumentRequest `json:"documents,omitempty"`
	// Deploy runbook. Optional at creation like every other non-title field;
	// nil leaves the column NULL.
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
}

type UpdateBoardTaskRequest struct {
	Title                *string       `json:"title,omitempty"`
	TaskType             *TaskType     `json:"task_type,omitempty"`
	Description          *string       `json:"description,omitempty"`
	TechnicalDescription *string       `json:"technical_description,omitempty"`
	InitiativeProjectID  *uuid.UUID    `json:"initiative_project_id,omitempty"`
	Column               *TaskColumn   `json:"column,omitempty"`
	Position             *int          `json:"position,omitempty"`
	Priority             *TaskPriority `json:"priority,omitempty"`
	// The assignee reads three spellings:
	//
	//	omitted   leave whoever is on the card alone
	//	null      unassign
	//	a value   assign that agent ("" also unassigns)
	//
	// It is domain.Nullable rather than a plain pointer for exactly that
	// reason — see its doc for what an omitted key and an explicit null used to
	// have in common, and which control it silently broke.
	AssigneeAgentID Nullable[uuid.UUID] `json:"assignee_agent_id,omitempty"`
	// Deploy runbook. Same optional-pointer contract as the other text fields:
	// nil leaves the stored value alone, a pointer to "" clears it.
	BeforeDeploy *string `json:"before_deploy,omitempty"`
	AfterDeploy  *string `json:"after_deploy,omitempty"`
	RollbackPlan *string `json:"rollback_plan,omitempty"`
	// DeployDependsOn replaces the task's deploy_depends_on relations wholesale
	// when non-nil, leaving every other relation type untouched. Pointer to a
	// slice so an omitted field changes nothing while an explicit [] clears the
	// dependencies — the same contract the agent tool's acceptance_criteria
	// argument already uses.
	DeployDependsOn *[]TaskRelationInput `json:"deploy_depends_on,omitempty"`
	// BlockedBy ADDS work-order blockers; it does not replace them, which is why
	// it is a plain slice where DeployDependsOn is a pointer. Deploy order is one
	// statement the release path reads as a whole and a caller editing it knows
	// the whole set; a blocker is a fact one planner learned, and a second
	// planner adding another must not silently drop the first one's.
	BlockedBy []TaskRelationInput `json:"blocked_by,omitempty"`
	// Actor is who is making the change. It decides review-gate behaviour: an
	// agent's approval is held for a human, a human's rejection can charge the
	// reviewer that approved, and a system move (pipeline, verification) does
	// neither. Never accepted from the client — the transport sets it.
	Actor TaskActor `json:"-"`
	// ActorAgentID identifies WHICH agent made the change when Actor is
	// TaskActorAgent. The dispatcher uses it to avoid re-dispatching the very
	// agent whose own tool call produced the board event (the claim→move→new
	// run feedback loop), and the move guard uses it to stop an assignee from
	// pushing its own task into reviewer-owned columns. Never accepted from
	// the client — the tool layer sets it from the run context.
	ActorAgentID *uuid.UUID `json:"-"`
	// SystemReason explains a move made by the control plane itself (Actor is
	// TaskActorSystem): one of the domain.MoveReason* constants. It rides along
	// into the board event payload so task history can say WHY the task moved
	// on its own instead of attributing the move to the human. Never accepted
	// from the client.
	SystemReason string `json:"-"`
}

// TaskActor names who initiated a task change.
type TaskActor string

const (
	// TaskActorSystem is the zero value: automated moves by the pipeline or
	// the verification step, which are nobody's review decision.
	TaskActorSystem TaskActor = ""
	TaskActorAgent  TaskActor = "agent"
	TaskActorHuman  TaskActor = "human"
)
