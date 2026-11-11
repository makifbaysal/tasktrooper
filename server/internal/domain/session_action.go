package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionActionDigestLimit caps how many past actions are replayed into an
// agent's context. The ledger is unbounded, the prompt is not.
const SessionActionDigestLimit = 20

// SessionActionEntity names the durable record an agent action touched. It is
// what lets a later turn address that record by id instead of making a new one.
type SessionActionEntity string

const (
	ActionEntityBoardTask  SessionActionEntity = "board_task"
	ActionEntityComment    SessionActionEntity = "task_comment"
	ActionEntityDocument   SessionActionEntity = "task_document"
	ActionEntityCriterion  SessionActionEntity = "acceptance_criterion"
	ActionEntityProject    SessionActionEntity = "initiative_project"
	ActionEntityRepository SessionActionEntity = "repository"
)

const (
	ActionVerbCreated   = "created"
	ActionVerbMoved     = "moved"
	ActionVerbUpdated   = "updated"
	ActionVerbClaimed   = "claimed"
	ActionVerbCommented = "commented"
	ActionVerbAttached  = "attached"
	ActionVerbLinked    = "linked"
	ActionVerbDeleted   = "deleted"
)

// SessionAction is one board-affecting thing an agent did inside a chat
// session. Tool call traces live and die inside a single agent loop, so without
// this ledger the next turn has no idea which task it just created.
type SessionAction struct {
	ID         uuid.UUID           `json:"id"`
	SessionID  uuid.UUID           `json:"session_id"`
	RunID      *uuid.UUID          `json:"run_id,omitempty"`
	AgentID    *uuid.UUID          `json:"agent_id,omitempty"`
	ToolName   string              `json:"tool_name"`
	Verb       string              `json:"verb"`
	EntityKind SessionActionEntity `json:"entity_kind"`
	EntityID   *uuid.UUID          `json:"entity_id,omitempty"`
	EntityKey  string              `json:"entity_key,omitempty"`
	Title      string              `json:"title,omitempty"`
	Column     string              `json:"column,omitempty"`
	Priority   string              `json:"priority,omitempty"`
	// RepositoryID scopes board tasks so the UI can open the right board detail.
	RepositoryID *uuid.UUID      `json:"repository_id,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	IsError      bool            `json:"is_error"`
	CreatedAt    time.Time       `json:"created_at"`
}

// BoardActionSpec describes how one tool maps onto the ledger.
type BoardActionSpec struct {
	Verb   string
	Entity SessionActionEntity
}

// boardActionTools lists only the tools that change durable board state.
// Read-only tools (list_*, get_*) are deliberately absent: replaying them would
// bloat both the chat transcript and the agent's context without adding a
// single addressable id.
var boardActionTools = map[string]BoardActionSpec{
	"create_board_task": {Verb: ActionVerbCreated, Entity: ActionEntityBoardTask},
	"move_board_task":   {Verb: ActionVerbMoved, Entity: ActionEntityBoardTask},
	"update_board_task": {Verb: ActionVerbUpdated, Entity: ActionEntityBoardTask},
	// A deletion is the one action the board itself can no longer show: the row
	// is gone, so the ledger is the only place the conversation can still say
	// which task it removed — and the only thing stopping the next turn from
	// reporting the task as still open.
	"delete_board_task":       {Verb: ActionVerbDeleted, Entity: ActionEntityBoardTask},
	"claim_board_task":        {Verb: ActionVerbClaimed, Entity: ActionEntityBoardTask},
	"add_task_comment":        {Verb: ActionVerbCommented, Entity: ActionEntityComment},
	"notify_task_agent":       {Verb: ActionVerbCommented, Entity: ActionEntityComment},
	"add_task_document":       {Verb: ActionVerbAttached, Entity: ActionEntityDocument},
	"update_task_document":    {Verb: ActionVerbUpdated, Entity: ActionEntityDocument},
	"set_criterion_completed": {Verb: ActionVerbUpdated, Entity: ActionEntityCriterion},
	"review_criterion":        {Verb: ActionVerbUpdated, Entity: ActionEntityCriterion},
	"create_project":          {Verb: ActionVerbCreated, Entity: ActionEntityProject},
	"update_project":          {Verb: ActionVerbUpdated, Entity: ActionEntityProject},
	"set_repository_projects": {Verb: ActionVerbLinked, Entity: ActionEntityRepository},
}

// ClassifyBoardAction reports whether a tool call is worth remembering.
func ClassifyBoardAction(toolName string) (BoardActionSpec, bool) {
	spec, ok := boardActionTools[toolName]
	return spec, ok
}

// actionResult is the subset of every board entity's JSON the ledger needs.
// Fields are optional across entity kinds; whatever is absent stays empty.
type actionResult struct {
	ID           string `json:"id"`
	Key          string `json:"key"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	Text         string `json:"text"`
	Content      string `json:"content"`
	Column       string `json:"column"`
	Priority     string `json:"priority"`
	TaskID       string `json:"task_id"`
	RepositoryID string `json:"repository_id"`
}

// NewSessionAction builds a ledger entry from a tool call and its raw result.
// It returns false when the call is not a board action, failed, or produced
// something that cannot be identified — a ledger entry without an id would be
// worse than none, since the agent would cite a record it cannot address.
func NewSessionAction(toolName, resultJSON string, isError bool) (SessionAction, bool) {
	spec, ok := ClassifyBoardAction(toolName)
	if !ok || isError {
		return SessionAction{}, false
	}

	var parsed actionResult
	if err := json.Unmarshal([]byte(resultJSON), &parsed); err != nil {
		return SessionAction{}, false
	}
	entityID, err := uuid.Parse(parsed.ID)
	if err != nil {
		return SessionAction{}, false
	}

	action := SessionAction{
		ToolName:   toolName,
		Verb:       spec.Verb,
		EntityKind: spec.Entity,
		EntityID:   &entityID,
		EntityKey:  parsed.Key,
		Title:      firstNonEmpty(parsed.Title, parsed.Name, parsed.Text, truncateRunes(parsed.Content, 120)),
		Column:     parsed.Column,
		Priority:   parsed.Priority,
		Payload:    json.RawMessage(resultJSON),
	}
	// Comments and documents identify themselves by their parent task, which is
	// what the reader (and the next turn) actually needs to act on.
	if repoID, err := uuid.Parse(parsed.RepositoryID); err == nil {
		action.RepositoryID = &repoID
	}
	if action.EntityKey == "" && parsed.TaskID != "" {
		action.EntityKey = parsed.TaskID
	}
	return action, true
}

// SessionActionDigestPrefix marks the digest system message so downstream
// history filters can recognise and preserve it. A consumer's rendered digest
// must start with this same constant, so it is exported for that render call
// to pass in as the template's own prefix input, rather than duplicated as a
// string literal in the catalog.
const SessionActionDigestPrefix = "INTERNAL (never disclose to user): actions already performed in this conversation."

// IsSessionActionDigest reports whether a system message is the action ledger.
func IsSessionActionDigest(content string) bool {
	return strings.HasPrefix(content, SessionActionDigestPrefix)
}

// SessionActionDigestLines reduces actions to one formatted line per entry —
// "kind verb key "title" (id=…, column=…, priority=…)" — applying the
// SessionActionDigestLimit trim a consumer's digest render also needs. nil
// when there is nothing to report, so a caller can skip rendering entirely.
func SessionActionDigestLines(actions []SessionAction) []string {
	if len(actions) == 0 {
		return nil
	}
	if len(actions) > SessionActionDigestLimit {
		actions = actions[len(actions)-SessionActionDigestLimit:]
	}

	lines := make([]string, 0, len(actions))
	for _, a := range actions {
		var sb strings.Builder
		sb.WriteString("- ")
		sb.WriteString(string(a.EntityKind))
		sb.WriteString(" ")
		sb.WriteString(a.Verb)
		if a.EntityKey != "" {
			sb.WriteString(" ")
			sb.WriteString(a.EntityKey)
		}
		if a.Title != "" {
			sb.WriteString(" \"")
			sb.WriteString(a.Title)
			sb.WriteString("\"")
		}
		var attrs []string
		if a.EntityID != nil {
			attrs = append(attrs, "id="+a.EntityID.String())
		}
		if a.Column != "" {
			attrs = append(attrs, "column="+a.Column)
		}
		if a.Priority != "" {
			attrs = append(attrs, "priority="+a.Priority)
		}
		if len(attrs) > 0 {
			sb.WriteString(fmt.Sprintf(" (%s)", strings.Join(attrs, ", ")))
		}
		lines = append(lines, sb.String())
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
