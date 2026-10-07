package port

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ActivityStore interface {
	CreateRun(ctx context.Context, sessionID *uuid.UUID, requestID, model string) (domain.SessionRun, error)
	// CompleteRun MUST leave a row that already reads 'cancelled' alone.
	CompleteRun(ctx context.Context, runID uuid.UUID, status string) error
	// CancelRun reports whether this write flipped the run; MUST decide in one
	// atomic statement so a run cannot be cancelled (or resurrected) twice.
	CancelRun(ctx context.Context, runID uuid.UUID) (bool, error)
	RunStatus(ctx context.Context, runID uuid.UUID) (string, error)
	AppendStep(ctx context.Context, runID uuid.UUID, stepType string, payload []byte) error
	ListRunsBySession(ctx context.Context, sessionID uuid.UUID, limit int) ([]domain.SessionRun, error)
	ListStepsByRun(ctx context.Context, runID uuid.UUID) ([]domain.SessionStep, error)
	// ListStepsByRunSince returns steps with created_at >= since, inclusive so
	// clients can de-duplicate by id across equal timestamps.
	ListStepsByRunSince(ctx context.Context, runID uuid.UUID, since time.Time) ([]domain.SessionStep, error)
	ListActiveRuns(ctx context.Context) ([]domain.SessionRun, error)
}

// InterruptedRunFailer settles, as failed, every session run still marked
// running. A run has no heartbeat, so only a process that knows no other one
// runs turns against this database may call it, and only before it starts any.
type InterruptedRunFailer interface {
	FailInterruptedRuns(ctx context.Context) (int, error)
}

type APIKeyStore interface {
	Create(ctx context.Context, name, keyHash, keyPrefix string, policy domain.ToolPolicy) (domain.APIKeyRecord, error)
	List(ctx context.Context) ([]domain.APIKeyRecord, error)
	Delete(ctx context.Context, name string) error
	FindByHash(ctx context.Context, keyHash string) (*domain.APIKeyRecord, error)
}
