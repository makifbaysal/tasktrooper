package domain

import (
	"errors"

	"github.com/google/uuid"
)

const BoardEventTaskRunUndone BoardEventType = "task.run_undone"

const MoveReasonRunUndone = "run_undone"

const (
	UndoModeReset   = "reset"
	UndoModeRevert  = "revert"
	UndoModeNothing = "nothing"
)

// Reasons an undo is not offered, as codes the UI turns into words.
const (
	UndoBlockedNoTrace       = "no_trace"
	UndoBlockedNoBase        = "no_base"
	UndoBlockedRemote        = "remote_not_supported"
	UndoBlockedNoWorkspace   = "no_workspace"
	UndoBlockedLiveRun       = "live_run"
	UndoBlockedOtherBranch   = "not_task_branch"
	UndoBlockedDefaultBranch = "default_branch"
	UndoBlockedBaseDiverged  = "base_not_ancestor"
	UndoBlockedAlreadyUndone = "already_undone"
	UndoBlockedGit           = "git_failed"
	UndoBlockedMerged        = "merged"
	UndoBlockedNothing       = "nothing_to_undo"
)

var (
	ErrUndoConfirmRequired = errors.New("undoing a run needs an explicit confirmation")
	ErrUndoNotAvailable    = errors.New("this run cannot be undone")
	ErrUndoUseRevert       = errors.New("the pull request is merged, so history is not rewritten: open a revert instead")
	ErrUndoRemoteAhead     = errors.New("the remote branch holds commits this checkout does not, so it is not force-pushed")
	ErrUndoNoRelease       = errors.New("this task is not part of a release")
	ErrUndoNotMerged       = errors.New("this task has no merged pull request to revert")
)

type RunUndoCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

type RunUndoPullRequest struct {
	URL      string `json:"url"`
	Number   int    `json:"number,omitempty"`
	Merged   bool   `json:"merged"`
	MergeSHA string `json:"merge_sha,omitempty"`
}

// RunUndoRelease is the release a merged task shipped in, and whether its
// rollback may be proposed.
type RunUndoRelease struct {
	ID              uuid.UUID     `json:"id"`
	Version         string        `json:"version"`
	Status          ReleaseStatus `json:"status"`
	RollbackAllowed bool          `json:"rollback_allowed"`
	RollbackRefusal string        `json:"rollback_refusal,omitempty"`
	Proposed        bool          `json:"proposed"`
}

// RunUndoPreview is what undoing a run would do, computed without changing
// anything.
type RunUndoPreview struct {
	RunID uuid.UUID `json:"run_id"`
	// Supported is false when an undo cannot be run at all; Reason says why.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Mode      string `json:"mode"`
	Branch    string `json:"branch,omitempty"`
	BaseSHA   string `json:"base_sha,omitempty"`
	HeadSHA   string `json:"head_sha,omitempty"`
	// Commits are the commits the run created (newest first); Files every path
	// that goes back to how the base commit had it; Uncommitted the working
	// tree changes that were never committed.
	Commits         []RunUndoCommit     `json:"commits"`
	Files           []TraceFileChange   `json:"files"`
	Uncommitted     []TraceFileChange   `json:"uncommitted"`
	Pushed          bool                `json:"pushed"`
	RemoteBranchSHA string              `json:"remote_branch_sha,omitempty"`
	RemoteAhead     bool                `json:"remote_ahead"`
	PullRequest     *RunUndoPullRequest `json:"pull_request,omitempty"`
	Release         *RunUndoRelease     `json:"release,omitempty"`
	BackupRef       string              `json:"backup_ref"`
	NonUndoable     []RunTraceEffect    `json:"non_undoable"`
	// MoveTo is the column the card returns to; empty when it stays put.
	MoveTo          string `json:"move_to,omitempty"`
	RevertAvailable bool   `json:"revert_available"`
}

type RunUndoRequest struct {
	Confirm   bool `json:"confirm"`
	ForcePush bool `json:"force_push"`
	// KeepColumn leaves the card where it is.
	KeepColumn bool `json:"keep_column"`
	// Note rides on a rollback proposal.
	Note string `json:"note,omitempty"`
}

type RunUndoResult struct {
	RunID     uuid.UUID `json:"run_id"`
	Mode      string    `json:"mode"`
	BackupRef string    `json:"backup_ref"`
	ResetTo   string    `json:"reset_to,omitempty"`
	// PushPending: the branch was pushed and was reset only here; pushing the
	// reset needs the person's go-ahead (force_push).
	PushPending   bool             `json:"push_pending"`
	ForcePushed   bool             `json:"force_pushed"`
	CleanedPaths  []string         `json:"cleaned_paths"`
	LeftUntracked []string         `json:"left_untracked"`
	MovedTo       string           `json:"moved_to,omitempty"`
	StaleCriteria int              `json:"stale_criteria"`
	NonUndoable   []RunTraceEffect `json:"non_undoable"`
}

type RunRevertResult struct {
	RunID    uuid.UUID `json:"run_id"`
	Branch   string    `json:"branch"`
	PRURL    string    `json:"pr_url,omitempty"`
	PRError  string    `json:"pr_error,omitempty"`
	MergeSHA string    `json:"merge_sha"`
}

func UndoBackupRef(runID uuid.UUID) string {
	return "tasktrooper/undo-backup/" + runID.String()
}

// UndoBlockedError is an undo the run's state does not allow; Reason is one of
// the UndoBlocked codes.
type UndoBlockedError struct{ Reason string }

func (e *UndoBlockedError) Error() string { return ErrUndoNotAvailable.Error() + " (" + e.Reason + ")" }
func (e *UndoBlockedError) Unwrap() error { return ErrUndoNotAvailable }

type RunRollbackProposalResult struct {
	RunID     uuid.UUID `json:"run_id"`
	ReleaseID uuid.UUID `json:"release_id"`
	Version   string    `json:"version"`
}
