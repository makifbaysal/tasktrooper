package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type BoardConfigStore interface {
	GetSettings(ctx context.Context) (domain.BoardSettings, error)
	UpdateSettings(ctx context.Context, keyPrefix string) (domain.BoardSettings, error)

	ListColumns(ctx context.Context) ([]domain.BoardColumn, error)
	ReplaceColumns(ctx context.Context, columns []domain.BoardColumnInput) error

	ListMembers(ctx context.Context) ([]domain.BoardMember, error)
	SetMembers(ctx context.Context, agentIDs []uuid.UUID) error

	ListSubscriptions(ctx context.Context) ([]domain.BoardSubscription, error)
	SetSubscriptions(ctx context.Context, subs []domain.BoardSubscriptionInput) error

	ListAgentSubscriptions(ctx context.Context, agentID uuid.UUID) ([]string, error)
	SetAgentSubscriptions(ctx context.Context, agentID uuid.UUID, columnSlugs []string) error
	ListAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnSubscription, error)
	SetAgentSubscriptionsDetailed(ctx context.Context, agentID uuid.UUID, subs []domain.AgentColumnSubscription) error

	ListAgentColumnInstructions(ctx context.Context, agentID uuid.UUID) ([]domain.AgentColumnInstruction, error)
	SetAgentColumnInstruction(ctx context.Context, agentID uuid.UUID, columnSlug, instruction string) error

	ListTransitions(ctx context.Context) ([]domain.BoardTransition, error)
	SetTransitions(ctx context.Context, transitions []domain.BoardTransition) error

	AgentsForColumn(ctx context.Context, columnSlug string, taskType string) ([]uuid.UUID, error)
	ValidateColumnSlug(ctx context.Context, slug string) (bool, error)
}

type TaskCommentStore interface {
	Create(ctx context.Context, comment domain.TaskComment) (domain.TaskComment, error)
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskComment, error)
}

type BoardEventStore interface {
	Create(ctx context.Context, event domain.BoardEvent) (domain.BoardEvent, error)
	ListRecent(ctx context.Context, limit int) ([]domain.BoardEvent, error)
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.BoardEvent, error)
}

type TaskAgentRunStore interface {
	Create(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error)
	Update(ctx context.Context, run domain.TaskAgentRun) (domain.TaskAgentRun, error)
	ListByTask(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.TaskAgentRun, error)
	ListRecent(ctx context.Context, limit int) ([]domain.TaskAgentRun, error)
	HasPendingForEvent(ctx context.Context, boardEventID, agentID uuid.UUID) (bool, error)
	HasPendingForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error)
	HasLiveForTask(ctx context.Context, taskID, agentID uuid.UUID) (bool, error)
	ListStale(ctx context.Context, cutoff time.Time) ([]domain.TaskAgentRun, error)
	Touch(ctx context.Context, id uuid.UUID) (status string, err error)
	ClaimRun(ctx context.Context, claim RunClaim) (RunClaimResult, error)
	FailIfStale(ctx context.Context, id uuid.UUID, cutoff time.Time, summary string) (bool, error)
	HasLiveRunForTask(ctx context.Context, taskID uuid.UUID, liveWithin time.Duration) (bool, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.TaskAgentRun, error)
	CancelIfLive(ctx context.Context, id uuid.UUID, reason string) (domain.TaskAgentRun, bool, error)
}

type RunClaim struct {
	RunID      uuid.UUID
	TaskID     uuid.UUID
	LiveWithin time.Duration
}

type RunClaimResult struct {
	Claimed bool
	Reason  string
}

type TaskColumnSpanStore interface {
	RecordMove(ctx context.Context, repositoryID, taskID uuid.UUID, toColumn string, at time.Time) error
	AttachAgent(ctx context.Context, taskID, agentID uuid.UUID) error
	SetReviewVerdict(ctx context.Context, taskID uuid.UUID, column, verdict string) error
	OpenSpan(ctx context.Context, taskID uuid.UUID) (domain.TaskColumnSpan, bool, error)
	OwnersForTask(ctx context.Context, taskID uuid.UUID) (map[string]uuid.UUID, error)
	HasVisited(ctx context.Context, taskID uuid.UUID, column string) (bool, error)
	LatestVerdicts(ctx context.Context, taskID uuid.UUID) (map[string]string, error)
	CleanTaskHours(ctx context.Context, agentID uuid.UUID, columns []string, from, to time.Time) ([]float64, error)
}
