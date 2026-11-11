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
	// ListByTask is every run header of the task, oldest first, with the
	// verification but no event counts.
	ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.RunTrace, error)
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

type UndoCommit struct {
	SHA     string
	Subject string
}

// UndoGitState is the checkout as an undo would find it.
type UndoGitState struct {
	Branch        string
	Head          string
	DefaultBranch string
	// BaseIsAncestor says the run's base commit is still behind HEAD.
	BaseIsAncestor bool
	// Commits are base..HEAD, newest first.
	Commits []UndoCommit
	// Uncommitted is the working tree against HEAD, untracked files included.
	Uncommitted []RunGitFile
	// Changes is the working tree against base: every path that returns.
	Changes []RunGitFile
	// RemoteBranchSHA is where origin/<branch> stood the last time this
	// checkout looked; empty when the branch was never pushed.
	RemoteBranchSHA string
	// Pushed says some commit the run made is on origin/<branch>.
	Pushed bool
	// RemoteAhead says origin/<branch> holds commits this checkout does not.
	RemoteAhead bool
}

type UndoPlan struct {
	Branch string
	Base   string
	// BackupRef is the branch name the pre-reset state is saved under.
	BackupRef string
	// CleanPaths are untracked paths the run's trace says it created; only
	// these are removed after the reset.
	CleanPaths []string
}

type UndoOutcome struct {
	BackupRef     string
	BackupSHA     string
	CleanedPaths  []string
	LeftUntracked []string
}

type RevertRequest struct {
	MergeSHA string
	Branch   string
}

type RevertOutcome struct {
	Branch    string
	RevertSHA string
	PRURL     string
	PRError   string
}

// RunUndoGit is the git side of undoing a run: always one checkout on this
// computer, always the task's own branch.
type RunUndoGit interface {
	UndoState(ctx context.Context, workspacePath, base, branch string) (UndoGitState, error)
	// UndoReset saves the whole working tree (committed and not) under
	// plan.BackupRef, then resets the checked-out task branch to plan.Base. It
	// refuses the repository's default branch and any other branch than
	// plan.Branch.
	UndoReset(ctx context.Context, workspacePath string, plan UndoPlan) (UndoOutcome, error)
	// ForcePushBranch publishes a reset task branch with a lease on the remote
	// tip the person saw; it never pushes the default branch.
	ForcePushBranch(ctx context.Context, workspacePath, branch, expectRemoteSHA string) error
	// RevertMerge cuts a branch from the default branch, reverts the merged
	// change on it, pushes it and opens a pull request. History stays as it is.
	RevertMerge(ctx context.Context, repoRoot string, req RevertRequest) (RevertOutcome, error)
}
