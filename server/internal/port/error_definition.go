package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrorDefinitionStore persists error definitions and the error groups the
// task action already opened a task for (migration 191).
type ErrorDefinitionStore interface {
	// ListErrorDefinitions answers newest first.
	ListErrorDefinitions(ctx context.Context, filter domain.ErrorDefinitionFilter) ([]domain.ErrorDefinition, error)
	GetErrorDefinition(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error)
	CreateErrorDefinition(ctx context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error)
	// UpdateErrorDefinition writes what a person sets; status, source,
	// task_id and firing_since are left as stored.
	UpdateErrorDefinition(ctx context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error)
	SetErrorDefinitionStatus(ctx context.Context, id uuid.UUID, status domain.ErrorDefinitionStatus) (domain.ErrorDefinition, error)
	// SetErrorDefinitionFiring never moves updated_at: it is the sweep's
	// state, not an edit.
	SetErrorDefinitionFiring(ctx context.Context, id uuid.UUID, since *time.Time) error
	// SetErrorDefinitionTask records the definition's first task only; a
	// later one leaves it.
	SetErrorDefinitionTask(ctx context.Context, id, taskID uuid.UUID) error
	DeleteErrorDefinition(ctx context.Context, id uuid.UUID) error

	// ClaimErrorGroupLink inserts the link; false when the group already has
	// one.
	ClaimErrorGroupLink(ctx context.Context, link domain.ErrorGroupLink) (bool, error)
	SetErrorGroupLinkTask(ctx context.Context, definitionID uuid.UUID, fingerprint string, taskID uuid.UUID) error
	ReleaseErrorGroupLink(ctx context.Context, definitionID uuid.UUID, fingerprint string) error
}

// AIJobStore persists the release engineer's suggestion jobs (migration 191).
type AIJobStore interface {
	CreateAIJob(ctx context.Context, job domain.AIJob) (domain.AIJob, error)
	GetAIJob(ctx context.Context, id uuid.UUID) (domain.AIJob, error)
	GetAIJobBySession(ctx context.Context, sessionID uuid.UUID) (domain.AIJob, error)
	// RunningAIJob is the running job of that kind for the repository and
	// component (nil: the repository-wide one); ErrNotFound when none runs.
	RunningAIJob(ctx context.Context, kind domain.AIJobKind, repositoryID uuid.UUID, componentID *uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error)
	SetAIJobSession(ctx context.Context, id, sessionID uuid.UUID) error
	AppendAIJobItem(ctx context.Context, id uuid.UUID, item domain.AIJobItem) error
	FinishAIJob(ctx context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) (domain.AIJob, error)
	// FailRunningAIJobs fails every job still running: run at boot, when
	// none of them can still have a turn in flight.
	FailRunningAIJobs(ctx context.Context, reason string) (int64, error)
}
