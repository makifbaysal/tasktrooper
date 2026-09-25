package domain

import "errors"

// Merging a task's pull request is the one board action that cannot be undone
// from here, so every refusal is an explicit named sentinel — the tool that
// reports them and the tests that pin them cannot drift apart by a typo.
var (
	// ErrMergeTaskNotDone: merging is what "done" means; a task still in QA or
	// UAT has a PR precisely so the change can be judged before it lands.
	ErrMergeTaskNotDone = errors.New("merge refused: this task is not in the done column, and only a task the board has signed off on may be merged")
	// ErrMergeNoPullRequest: a task whose branch was never pushed has no PR.
	ErrMergeNoPullRequest = errors.New("merge refused: this task has no pull request")
	// ErrMergeAlreadyMerged is a refusal, not a retryable error: the work is
	// already on the default branch.
	ErrMergeAlreadyMerged = errors.New("merge refused: this pull request is already merged")
	// ErrMergeClosed: a PR closed without merging is a decision, not a glitch;
	// reopening it is a human's call.
	ErrMergeClosed = errors.New("merge refused: this pull request is closed without having been merged")
	// ErrMergeChecksNotGreen covers both halves of "the checks are not green":
	// GitHub's own mergeable state and the board's own pipeline ledger.
	ErrMergeChecksNotGreen = errors.New("merge refused: the checks for this pull request are not green")
	// ErrMergeNotConfigured is the "this deployment cannot merge" answer — no
	// GitHub token, no PR client; distinct from a refusal about the PR itself,
	// because the remedy is an operator's, not the agent's.
	ErrMergeNotConfigured = errors.New("merge refused: merging is not configured on this deployment")
)

// PullRequestMergeRequest is one squash-merge, fully specified by the caller.
//
// ExpectedHeadSHA is the load-bearing field: GitHub accepts a merge without it
// and would then take whatever the branch points at when the request lands —
// including a commit pushed in the seconds between the gate reading the PR and
// the merge going out. Passing it turns that race into a 409 instead of a
// silent merge of unreviewed code.
type PullRequestMergeRequest struct {
	Owner  string
	Repo   string
	Number int
	Branch string
	// ExpectedHeadSHA is sent to GitHub as the merge's `sha` precondition.
	ExpectedHeadSHA string
	// Undraft asks for the draft state to be lifted before merging. GitHub
	// refuses to merge a draft PR, and PRs opened before task PRs became
	// ready-for-review PRs are still drafts (see MarkPullRequestReady).
	Undraft bool
	// DeleteBranch removes the head branch after a successful merge. A failure
	// here is reported, never fatal: the merge already happened.
	DeleteBranch bool
	CommitTitle  string
	CommitBody   string
}

// PullRequestMergeResult is what actually happened, in the order it happened.
// Every step is reported separately because they fail independently: a merge
// that landed while the branch delete failed must not read as "nothing
// happened", or the next run would merge a merged PR.
type PullRequestMergeResult struct {
	MergeCommitSHA string
	// Undrafted records that the PR was a draft and was marked ready first; it
	// is normally false now that task PRs open ready for review.
	Undrafted     bool
	BranchDeleted bool
	// BranchDeleteError is the branch delete's failure, kept as text rather
	// than an error so it can travel in a tool result beside the merge that did
	// succeed.
	BranchDeleteError string
}

// TaskPRMergeResult is the merge as the board reports it: what landed, where,
// and what a reader has to know about it.
type TaskPRMergeResult struct {
	Merged         bool   `json:"merged"`
	PRNumber       int    `json:"pr_number,omitempty"`
	PRURL          string `json:"pr_url,omitempty"`
	MergeCommitSHA string `json:"merge_commit_sha,omitempty"`
	Branch         string `json:"branch,omitempty"`
	BaseBranch     string `json:"base_branch,omitempty"`
	BranchDeleted  bool   `json:"branch_deleted"`
	Undrafted      bool   `json:"undrafted,omitempty"`
	// AutoReleased is true when the repository has no deploy_target configured
	// anywhere: the merge already moved the task straight to `released`, and
	// the caller must not call trigger_release afterwards — there is nothing
	// left to deploy or wait on.
	AutoReleased bool `json:"auto_released,omitempty"`
	// Release is what the merge set in motion for the task's component: the
	// release it opened (or joined), or that the merge itself was the release.
	Release *ReleaseOpening `json:"release,omitempty"`
	// Message is the human sentence: what merged, and what did not go perfectly
	// (a branch that could not be deleted, a SHA that could not be recorded)
	// without pretending the merge itself failed.
	Message string `json:"message,omitempty"`
}
