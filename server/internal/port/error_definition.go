package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ErrErrorDefinitionExists is a suggestion whose component and match an
// earlier suggestion of the repository already holds.
var ErrErrorDefinitionExists = errors.New("the same error definition is already suggested")

// ErrorDefinitionStore persists error definitions and the error groups the
// task action already opened a task for (migration 191).
type ErrorDefinitionStore interface {
	// ListErrorDefinitions answers newest first.
	ListErrorDefinitions(ctx context.Context, filter domain.ErrorDefinitionFilter) ([]domain.ErrorDefinition, error)
	GetErrorDefinition(ctx context.Context, id uuid.UUID) (domain.ErrorDefinition, error)
	// CreateErrorDefinition answers ErrErrorDefinitionExists for a suggestion
	// another suggestion of the same component and match already holds.
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

	// ClaimErrorGroupLink inserts the link, or takes over one whose claim
	// opened no task and is older than staleBefore (its process died before
	// linking one); false when the group already has a live one.
	ClaimErrorGroupLink(ctx context.Context, link domain.ErrorGroupLink, staleBefore time.Time) (bool, error)
	// SetErrorGroupLinkTask links the task and ends the claim.
	SetErrorGroupLinkTask(ctx context.Context, definitionID uuid.UUID, fingerprint string, taskID uuid.UUID) error
	ReleaseErrorGroupLink(ctx context.Context, definitionID uuid.UUID, fingerprint string) error
}

// AIJobStore persists the release engineer's suggestion jobs (migrations 191,
// 192).
type AIJobStore interface {
	// CreateAIJob inserts job as running, unless one of the same kind,
	// repository, component (nil: the repository-wide one) and environment
	// already runs: that one is answered instead, with created false.
	CreateAIJob(ctx context.Context, job domain.AIJob) (out domain.AIJob, created bool, err error)
	GetAIJob(ctx context.Context, id uuid.UUID) (domain.AIJob, error)
	GetAIJobBySession(ctx context.Context, sessionID uuid.UUID) (domain.AIJob, error)
	SetAIJobSession(ctx context.Context, id, sessionID uuid.UUID) error
	// StartAIJob stamps started_at on a job still running; false when it is
	// not (it was failed while it waited).
	StartAIJob(ctx context.Context, id uuid.UUID) (bool, error)
	// AppendAIJobItem adds item to a running job's created list unless the
	// same kind and id is already there; on a job that ended it is no change.
	AppendAIJobItem(ctx context.Context, id uuid.UUID, item domain.AIJobItem) error
	// FinishAIJob ends a running job; ErrNotFound when it is not running.
	FinishAIJob(ctx context.Context, id uuid.UUID, status domain.AIJobStatus, summary, errMsg string) (domain.AIJob, error)
	// FailStaleAIJobs fails the running jobs that started before
	// startedBefore, or have waited to start since before queuedBefore.
	FailStaleAIJobs(ctx context.Context, startedBefore, queuedBefore time.Time, reason string) (int64, error)
	// FailRunningAIJobs fails the running jobs created before before by an
	// owner other than owner (or by none): run at boot, for the jobs a
	// previous process left.
	FailRunningAIJobs(ctx context.Context, owner string, before time.Time, reason string) (int64, error)
}
