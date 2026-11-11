package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type GitClient interface {
	// A worktree or submodule checkout counts: .git is a file there, not a
	// directory, and git works in it either way.
	HasGit(rootPath string) bool
	// Answers why when the answer is no (no repo / missing folder / unreadable),
	// for callers that must explain it to a person. Not on a context: it is a
	// stat called once per repository in every list response.
	Presence(rootPath string) domain.GitPresence
	Status(ctx context.Context, rootPath string) domain.GitStatus
	EnsureRepoWithRemote(ctx context.Context, rootPath, name, owner string) error
	CloneRepo(ctx context.Context, cloneURL, dest string) error
	// Refreshes an existing workspace against origin rather than reusing it as
	// found; preserving uncommitted work and local commits, and failing loudly
	// when they conflict with origin.
	EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error
	OriginURL(ctx context.Context, rootPath string) string
	FetchLatest(ctx context.Context, rootPath string) error
	DefaultBranch(ctx context.Context, rootPath string) string
	// The mirror clone is supposed to be written only by agents in per-task
	// workspaces, so when local changes or a wrong branch block the
	// fast-forward it force-resets onto origin rather than freezing the clone.
	SyncDefaultBranch(ctx context.Context, rootPath string) error
	CommitAndPush(ctx context.Context, workspacePath, message string) error
	// Publishes the checked-out branch without committing anything — a
	// reviewer's PR must never include the tree it judged.
	PushBranch(ctx context.Context, workspacePath string) error
	// Opens an OPEN, ready-for-review pull request; it was EnsureDraftPR and
	// drafts are unmergeable, so it was renamed with the behaviour.
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	// Takes no workspace path because the task may be in `done`, whose
	// workspace the reaper may already have deleted. Head-SHA precondition
	// aside, every merge decision lives above this call.
	MergePullRequest(ctx context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error)
	// The rollback for a repository with no deploy workflow: a push-to-deploy
	// host redeploys whatever the default branch points at, so undoing a
	// release means adding a commit. Never force-pushes; aborts a conflicting
	// revert rather than resolving it.
	RevertCommitOnDefaultBranch(ctx context.Context, rootPath, sha, message string) (revertSHA string, err error)
	TaskDiff(ctx context.Context, workspacePath string) (string, error)
	// The paths the task branch changed against its merge base — the input to
	// the schema-change (migration) detector.
	TaskChangedFiles(ctx context.Context, workspacePath string) ([]string, error)
	ChangedFilesSince(ctx context.Context, workspacePath, sha string) ([]string, error)
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
}

// GitFileStat is one changed file of a task's diff as `git diff --numstat`
// counts it; Binary files have no line counts.
type GitFileStat struct {
	Path           string
	Added, Deleted int
	Binary         bool
}
