package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// ReleaseStore persists releases and which tasks each one carries. Get/ForTask
// return an error wrapping domain.ErrReleaseNotFound when nothing matches.
// Every read fills Release.Tasks (id, key, title, type, column, merge sha).
type ReleaseStore interface {
	Create(ctx context.Context, r domain.Release, taskIDs []uuid.UUID) (domain.Release, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Release, error)
	// ForTask is the newest release carrying the task.
	ForTask(ctx context.Context, taskID uuid.UUID) (domain.Release, error)
	List(ctx context.Context, f domain.ReleaseListFilter) ([]domain.Release, error)
	// Update writes every mutable field (status, version, commit, tag, notes,
	// deploy, checks, verdict, rollback, failure reason, card, timestamps)
	// only when the stored status still equals expect; it returns
	// domain.ErrReleaseWrongStatus otherwise, so two sweepers (or a sweeper and
	// an agent) can never both advance the same release.
	Update(ctx context.Context, r domain.Release, expect domain.ReleaseStatus) (domain.Release, error)
	AddTasks(ctx context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) error
	// AddTasksToDraft adds only while the release is still a draft, in the same
	// statement; false means it was cut (or superseded) meanwhile and nothing
	// was added.
	AddTasksToDraft(ctx context.Context, releaseID uuid.UUID, taskIDs []uuid.UUID) (bool, error)
	RemoveTask(ctx context.Context, releaseID, taskID uuid.UUID) error
	// LastReleased is the newest `released` release of the component finished
	// before the given time — what a rollback redeploys.
	LastReleased(ctx context.Context, repositoryID uuid.UUID, componentID *uuid.UUID, before time.Time) (domain.Release, error)
	// MarkAgentSeen stamps AgentSeenAt now, out of band from Update: it is the
	// sole writer of that column, so a caller updating other fields from a
	// copy read before an agent's own look cannot clobber it back to an
	// older value — the hand-back watchdog's AgentSeenAt-vs-LastHandBackAt
	// race depends on that.
	MarkAgentSeen(ctx context.Context, id uuid.UUID, at time.Time) error
}
