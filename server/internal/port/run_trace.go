package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type RunTraceFilter struct {
	Category string
	Limit    int
	// After is the seq of the last row already read; rows come back in seq order.
	After int
}

// RunTraceStore keeps what a run did. Writes are append-only: nothing here
// rewrites an event once it was recorded.
type RunTraceStore interface {
	// Begin writes the run's header row, doing nothing when the run already
	// has one (a run that resumes after a quota park keeps its first base).
	Begin(ctx context.Context, trace domain.RunTrace) error
	Header(ctx context.Context, runID uuid.UUID) (domain.RunTrace, error)
	// NextSeq is the sequence number the next event of the run takes.
	NextSeq(ctx context.Context, runID uuid.UUID) (int, error)
	Append(ctx context.Context, events []domain.RunTraceEvent) error
	// Finish stamps the run's verified outcome and event totals.
	Finish(ctx context.Context, runID uuid.UUID, headSHA string, dropped int, verification *domain.RunTraceVerification) error
	List(ctx context.Context, runID uuid.UUID, filter RunTraceFilter) ([]domain.RunTraceEvent, error)
	Counts(ctx context.Context, runID uuid.UUID) (domain.RunTraceCounts, error)
	// Effects are the events that touched something outside the workspace.
	Effects(ctx context.Context, runID uuid.UUID) ([]domain.RunTraceEvent, error)
	// Changes are the change events, in order, for the git comparison.
	Changes(ctx context.Context, runID uuid.UUID) ([]domain.RunTraceEvent, error)
	// DeleteBefore removes the headers (and, through the foreign key, the
	// events) of runs that started before cutoff.
	DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

type RunGitFile struct {
	Path   string
	Status string
	From   string
}

// RunGitInspector is git read in one checkout, for the comparison between a
// run's trace and what actually changed.
type RunGitInspector interface {
	HeadSHA(ctx context.Context, workspacePath string) (string, error)
	// ChangesSince is every path that differs from base in the working tree
	// (committed, staged or not) plus untracked files that are not ignored.
	ChangesSince(ctx context.Context, workspacePath, base string) ([]RunGitFile, error)
	TrackedFileCount(ctx context.Context, workspacePath, rev string) (int, error)
}
