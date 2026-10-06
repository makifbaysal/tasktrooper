package domain

import (
	"errors"

	"github.com/google/uuid"
)

// ErrColumnHasWorkflowStages is UpdateColumns' refusal to remove a column slug
// a workflow stage with behaviours still references: workflow_stages carries no
// FK to board_columns (ReplaceColumns deletes and reinserts every row), so this
// is the only thing that stops a rename/delete from silently orphaning a stage.
var ErrColumnHasWorkflowStages = errors.New("column is referenced by a workflow stage with behaviours")

type BoardColumn struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Label     string    `json:"label"`
	Position  int       `json:"position"`
	IsBacklog bool      `json:"is_backlog"`
}

type BoardMember struct {
	AgentID uuid.UUID `json:"agent_id"`
}

type BoardSubscription struct {
	AgentID    uuid.UUID `json:"agent_id"`
	ColumnSlug string    `json:"column_slug"`
}

// AgentColumnSubscription is one column an agent subscribes to, with its
// optional per-column task-type filter. TaskTypes nil means "every type"; a
// non-nil slice narrows dispatch on that column to only those types.
type AgentColumnSubscription struct {
	ColumnSlug string
	TaskTypes  []string
}

// AgentColumnInstruction is text handed to an agent when a run dispatches it
// for a task that arrived in column_slug. A separate store from
// AgentColumnSubscription: the column that dispatches an agent need not be one
// it watches, and instructions must never change watch-based dispatch.
type AgentColumnInstruction struct {
	AgentID     uuid.UUID `json:"agent_id"`
	ColumnSlug  string    `json:"column_slug"`
	Instruction string    `json:"instruction"`
	// CatalogSHA is sha256 hex of Instruction as last written by the catalog
	// sync; '' means operator-owned or unknown provenance. Equal to
	// sha256(Instruction) means untouched since that write, so the sync may
	// still overwrite or delete the row on the catalog's behalf.
	CatalogSHA string `json:"catalog_sha,omitempty"`
}

type BoardTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SetBoardTransitionsRequest struct {
	Transitions []BoardTransition `json:"transitions"`
}

type BoardColumnInput struct {
	Slug      string `json:"slug"`
	Label     string `json:"label"`
	Position  int    `json:"position"`
	IsBacklog bool   `json:"is_backlog"`
}

type BoardSubscriptionInput struct {
	AgentID        uuid.UUID `json:"agent_id"`
	ColumnSlugs    []string  `json:"column_slugs"`
	TaskTypeFilter []string  `json:"task_type_filter,omitempty"`
}

type UpdateBoardColumnsRequest struct {
	Columns []BoardColumnInput `json:"columns"`
}

type SetBoardMembersRequest struct {
	AgentIDs []uuid.UUID `json:"agent_ids"`
}

type SetBoardSubscriptionsRequest struct {
	Subscriptions []BoardSubscriptionInput `json:"subscriptions"`
}

type UpdateBoardSettingsRequest struct {
	KeyPrefix string `json:"key_prefix"`
}

type BoardSettings struct {
	KeyPrefix string `json:"key_prefix"`
}

type WorkspaceConfig struct {
	Settings      BoardSettings       `json:"settings"`
	Columns       []BoardColumn       `json:"columns"`
	Members       []BoardMember       `json:"members"`
	Subscriptions []BoardSubscription `json:"subscriptions"`
	Transitions   []BoardTransition   `json:"transitions"`
	// DefaultTransitions is what "Reset to defaults" applies.
	DefaultTransitions []BoardTransition `json:"default_transitions"`
}

// requiredTransitions are the moves the product itself makes or tells an agent
// to make (runner auto-enter and hand-offs, the review verdict sweep, the
// unchanged-diff skip, release finish/rollback, human review decisions). A
// restricted graph missing one of them stalls the pipeline in that column,
// since ValidateTransition checks system and agent moves too. Parks into and
// out of blocked bypass the check, so blocked needs none.
var requiredTransitions = []BoardTransition{
	{From: "backlog", To: "todo"},
	{From: "todo", To: "in_progress"},
	{From: "in_progress", To: "code_review"},
	{From: "in_progress", To: "analiz_review"},
	{From: "in_progress", To: "need_revision"},
	{From: "in_progress", To: "human_uat"},
	{From: "analiz_review", To: "done"},
	{From: "analiz_review", To: "need_revision"},
	{From: "code_review", To: "ready_for_qa"},
	{From: "code_review", To: "need_revision"},
	{From: "ready_for_qa", To: "in_qa"},
	{From: "ready_for_qa", To: "need_revision"},
	{From: "in_qa", To: "pm_uat"},
	{From: "in_qa", To: "human_uat"},
	{From: "in_qa", To: "need_revision"},
	{From: "need_revision", To: "in_progress"},
	{From: "need_revision", To: "code_review"},
	{From: "need_revision", To: "analiz_review"},
	{From: "pm_uat", To: "human_uat"},
	{From: "pm_uat", To: "need_revision"},
	{From: "human_uat", To: "done"},
	{From: "human_uat", To: "need_revision"},
	{From: "done", To: "released"},
	{From: "done", To: "need_revision"},
	{From: "released", To: "need_revision"},
}

// DefaultBoardTransitions is the recommended graph for the stock columns: the
// required moves, plus parking by hand from any active column, the diff-skip
// fallback past in_qa, a rare todo bounce and un-starting a task. blocked has
// no rows on purpose — a park returns to whatever column it came from.
func DefaultBoardTransitions() []BoardTransition {
	out := append([]BoardTransition{}, requiredTransitions...)
	out = append(out,
		BoardTransition{From: "todo", To: "need_revision"},
		BoardTransition{From: "in_progress", To: "todo"},
		BoardTransition{From: "ready_for_qa", To: "pm_uat"},
		BoardTransition{From: "ready_for_qa", To: "human_uat"},
	)
	for _, from := range []string{"todo", "in_progress", "analiz_review", "code_review", "ready_for_qa", "in_qa", "need_revision", "pm_uat", "human_uat", "done"} {
		out = append(out, BoardTransition{From: from, To: "blocked"})
	}
	return out
}

// RequiredBoardTransitions is the subset DefaultBoardTransitions cannot drop
// without stalling the automation.
func RequiredBoardTransitions() []BoardTransition {
	return append([]BoardTransition{}, requiredTransitions...)
}

func DefaultBoardColumnTemplate() []BoardColumnInput {
	out := []BoardColumnInput{
		{Slug: string(TaskColumnBacklog), Label: "Backlog", Position: 0, IsBacklog: true},
	}
	pos := 1
	for _, col := range BoardColumns {
		out = append(out, BoardColumnInput{
			Slug:      string(col),
			Label:     defaultColumnLabel(col),
			Position:  pos,
			IsBacklog: false,
		})
		pos++
	}
	return out
}

func defaultColumnLabel(col TaskColumn) string {
	labels := map[TaskColumn]string{
		TaskColumnTodo:         "Todo",
		TaskColumnInProgress:   "In Progress",
		TaskColumnAnalizReview: "Analiz Review",
		TaskColumnCodeReview:   "Code Review",
		TaskColumnReadyForQA:   "Ready for QA",
		TaskColumnInQA:         "In QA",
		TaskColumnNeedRevision: "Need Revision",
		TaskColumnPMUAT:        "PM UAT",
		TaskColumnHumanUAT:     "Human UAT",
		TaskColumnBlocked:      "Blocked",
		TaskColumnDone:         "Done",
		TaskColumnReleased:     "Released",
	}
	if label, ok := labels[col]; ok {
		return label
	}
	return string(col)
}
