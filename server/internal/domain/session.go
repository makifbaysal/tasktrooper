package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrSessionNotFound names the one reason a session command can fail that the
// caller could have avoided, so the transport can answer 404 for it without
// having to read every store error as "missing".
var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title,omitempty"`
	Model        string     `json:"model,omitempty"`
	WorkspaceDir string     `json:"workspace_dir,omitempty"`
	ProjectRoot  string     `json:"project_root,omitempty"`
	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	AgentID      *uuid.UUID `json:"agent_id,omitempty"`
	// TaskID makes this chat be about ONE board task. It changes two things
	// about every turn: the workspace is the task's own branch checkout instead
	// of the shared mirror clone (which SyncDefaultBranch force-resets, so work
	// done there would be silently thrown away and could never reach the task's
	// PR), and the prompt carries the task and its pull request. Nil for every
	// other chat.
	TaskID *uuid.UUID `json:"task_id,omitempty"`
	// CLISessionID is the Claude Code conversation behind this chat, for an
	// agent on the claude_code provider. Empty for every other chat, and for the
	// first turn of one of these.
	//
	// It is what makes the chat multi-turn: with it, the next message resumes
	// the live CLI session and sends only what the user typed; without it, every
	// turn would re-flatten the whole transcript into a new session, paying for
	// the conversation again each time and handing the model its own memory back
	// as a fresh instruction. See migration 103.
	//
	// Not in the JSON view: it names a process-local artefact on the runner host
	// and means nothing to a client.
	CLISessionID string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// PendingSessionTurn is a chat turn parked on the Claude Code usage limit
// (see QuotaBlock), waiting for a SessionQuotaSweeper to rerun it once
// ResumeAt has passed. It is the chat's counterpart to the board's own
// quota park (migration 101) — see migration 137 for where it lives.
//
// The user's message is not carried here: it was already appended to the
// session's transcript before the run that hit the limit, so the sweeper
// rebuilds history from the store like any other turn and only needs the
// original request (for its Content, FileIDs, Model, ...) and the policy
// that request resolved to.
type PendingSessionTurn struct {
	SessionID uuid.UUID
	Request   SessionMessageRequest
	Policy    ToolPolicy
	ResumeAt  time.Time
}

type SessionMessage struct {
	ID            uuid.UUID             `json:"id"`
	SessionID     uuid.UUID             `json:"session_id"`
	Role          Role                  `json:"role"`
	Content       string                `json:"content"`
	ToolCalls     json.RawMessage       `json:"tool_calls,omitempty"`
	Clarification *ClarificationRequest `json:"clarification,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	// Attachments are the binary files sent with this message, enriched on the
	// history read path. The image ones also reach the model as multimodal
	// content; documents ride the transcript only.
	Attachments []AttachmentMeta `json:"attachments,omitempty"`
}

type CreateSessionRequest struct {
	Title     string     `json:"title,omitempty"`
	Model     string     `json:"model,omitempty"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	// TaskID binds the new chat to a board task (see Session.TaskID). Only
	// meaningful together with ProjectID: the task is read through its
	// repository, so a task without one cannot be resolved or ownership-checked.
	TaskID *uuid.UUID `json:"task_id,omitempty"`
}

type SessionMessageRequest struct {
	Role        Role             `json:"role"`
	Content     string           `json:"content"`
	Model       string           `json:"model,omitempty"`
	ToolPolicy  ToolPolicy       `json:"tool_policy,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	FileIDs     []string         `json:"file_ids,omitempty"`
	Orchestrate *bool            `json:"orchestrate,omitempty"`
	Mentions    []MessageMention `json:"mentions,omitempty"`
	// AttachmentIDs are already-uploaded binary attachments (POST
	// /v1/attachments) to link to the persisted user message. Unlike FileIDs
	// (RAG context, chunked text) the image ones go to the model as native
	// multimodal content; documents are linked but not sent.
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

// MessageMention is an @-tag the user picked from the composer's autocomplete.
// The message text keeps the human-readable "@Name"; this carries the exact
// entity the user selected, so the backend never has to guess between
// same-named entities of different kinds.
type MessageMention struct {
	Kind string    `json:"kind"` // "agent" | "project" | "repository"
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
