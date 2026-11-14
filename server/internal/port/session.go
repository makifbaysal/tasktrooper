package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SessionStore interface {
	Create(ctx context.Context, title, model, workspaceDir string, projectID, agentID *uuid.UUID, expiresAt *time.Time) (domain.Session, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Session, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, limit, offset int) ([]domain.Session, error)
	ListByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]domain.Session, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]domain.Session, error)
	ListMessages(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionMessage, error)
	AppendMessage(ctx context.Context, sessionID uuid.UUID, role domain.Role, content string, toolCalls []byte, clarification []byte) (domain.SessionMessage, error)
	UpdateWorkspaceDir(ctx context.Context, id uuid.UUID, workspaceDir string) error
	UpdateProjectRoot(ctx context.Context, id uuid.UUID, projectRoot string) error
	// UpdateTitle sets the chat's display name and freezes it: autoTitled=true
	// means the auto-title generator must never touch this session's title
	// again.
	UpdateTitle(ctx context.Context, id uuid.UUID, title string, autoTitled bool) error
	// Also written after a turn that failed — the session it left behind is
	// still the one worth resuming.
	UpdateCLISessionID(ctx context.Context, id uuid.UUID, cliSessionID string) error
	// Also how an already-open thread (the task's clarification chat) is
	// adopted rather than duplicated.
	BindTask(ctx context.Context, id, taskID uuid.UUID) error
	// What lets "open the chat about this task" be idempotent; ok=false when
	// no thread exists yet.
	FindByTask(ctx context.Context, taskID uuid.UUID) (domain.Session, bool, error)
	// The task's thread held with one specific agent (a reviewer, QA, ...).
	FindByTaskAndAgent(ctx context.Context, taskID, agentID uuid.UUID) (domain.Session, bool, error)
	// An UPSERT: a second park on the same session replaces the first, on the
	// invariant that there is at most one run in flight per session.
	ParkPendingTurn(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy, resumeAt time.Time) error
	// Atomically unparks one expired chat turn, oldest park first.
	TakePendingSessionTurn(ctx context.Context, now time.Time) (domain.PendingSessionTurn, bool, error)
	// ListRecentChatMessages feeds the activity stream: the most recent
	// assistant-authored messages from every agent-owned session (never an
	// ungrouped one, agent_id IS NULL).
	ListRecentChatMessages(ctx context.Context, limit int) ([]domain.ChatActivityEntry, error)
}

// SessionActionStore persists the board records an agent touched inside a chat
// session, so a later turn can address them by id instead of creating new ones.
type SessionActionStore interface {
	AppendAction(ctx context.Context, action domain.SessionAction) (domain.SessionAction, error)
	ListActions(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionAction, error)
}

type JobStore interface {
	Create(ctx context.Context, request []byte, callbackURL string) (domain.Job, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Job, error)
	List(ctx context.Context, status string, limit int) ([]domain.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.JobStatus, result []byte, errMsg string) error
	Delete(ctx context.Context, id uuid.UUID) error
	ClaimPending(ctx context.Context) (*domain.Job, error)
}

type FileStore interface {
	Create(ctx context.Context, filename, contentType string, sizeBytes int64) (domain.FileRecord, error)
	Get(ctx context.Context, id uuid.UUID) (domain.FileRecord, error)
	List(ctx context.Context) ([]domain.FileRecord, error)
	Delete(ctx context.Context, id uuid.UUID) error
	SaveChunks(ctx context.Context, fileID uuid.UUID, chunks []domain.FileChunk) error
	SearchChunks(ctx context.Context, fileIDs []uuid.UUID, queryEmbedding []float32, topK int) ([]domain.FileChunk, error)
}

// SessionWakeStore holds the chat agents' "wake me when it is done" rows.
type SessionWakeStore interface {
	// Add is idempotent per (session, kind, ref): a repeat refreshes the label and expiry.
	Add(ctx context.Context, w domain.SessionWake) error
	ListOpen(ctx context.Context) ([]domain.SessionWake, error)
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]domain.SessionWake, error)
	// Delete reports whether this call removed the row: the claim, so two
	// sweeper ticks or a user's Stop never both act on one wake.
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	DeleteBySession(ctx context.Context, sessionID uuid.UUID) (int, error)
}
