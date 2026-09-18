package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type GitClient interface {
	// HasGit is the gate: can git be run at rootPath. A worktree or submodule
	// checkout counts — .git is a file there, not a directory, and git works in
	// it either way.
	HasGit(rootPath string) bool
	// Presence answers the same question as HasGit and, when the answer is no,
	// WHY: the folder has no repository, the folder is not there at all, or it
	// could not be read. Callers that only gate a git command keep using
	// HasGit; callers that have to explain the answer to a person need this,
	// because "not a git repository yet" told the user to `git init` a folder
	// that does not exist on this machine.
	//
	// Not on a context: it is a stat, and it is called once per repository in
	// every list response.
	Presence(rootPath string) domain.GitPresence
	Status(ctx context.Context, rootPath string) domain.GitStatus
	// EnsureRepoWithRemote: owner "" ise token sahibinin hesabı, değilse o org.
	EnsureRepoWithRemote(ctx context.Context, rootPath, name, owner string) error
	// CloneRepo, GitHub reposunu (token auth ile) dest klasörüne klonlar.
	CloneRepo(ctx context.Context, cloneURL, dest string) error
	// EnsureTaskWorkspace prepares the task's isolated checkout on branch. A
	// workspace that already exists is refreshed rather than reused as found:
	// origin is fetched and the branch is brought up to its base. It preserves
	// uncommitted work and local commits, and fails loudly (leaving the
	// workspace untouched) when they conflict with what origin has.
	EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error
	// OriginURL returns the working copy's origin remote, "" when absent.
	OriginURL(ctx context.Context, rootPath string) string
	// FetchLatest refreshes remote-tracking refs so a task branch is cut from
	// current origin state rather than a stale local clone.
	FetchLatest(ctx context.Context, rootPath string) error
	// DefaultBranch reports origin's default branch ("" when undeterminable).
	DefaultBranch(ctx context.Context, rootPath string) string
	// SyncDefaultBranch brings the shared mirror clone at rootPath onto
	// origin's default branch so an index pass reads current code. Nothing is
	// supposed to write to that tree (agents work in per-task workspaces), so
	// when local changes or a wrong-branch checkout block the fast-forward it
	// force-resets onto origin rather than freezing the clone at an old
	// commit. What it discarded is logged.
	SyncDefaultBranch(ctx context.Context, rootPath string) error
	CommitAndPush(ctx context.Context, workspacePath, message string) error
	// PushBranch publishes the checked-out branch without committing anything.
	// A reviewer needs the branch on origin to have a PR at all, and it must
	// never commit the tree it is judging — that is what separates it from
	// CommitAndPush.
	PushBranch(ctx context.Context, workspacePath string) error
	// EnsurePullRequest makes sure the workspace's branch has an OPEN, ready-for-
	// review pull request and returns its URL. It was EnsureDraftPR and opened
	// drafts; nothing ever un-drafted them and GitHub refuses to merge a draft,
	// so every task PR was unmergeable by construction. Renamed with the
	// behaviour so no caller can read "draft" and believe it.
	EnsurePullRequest(ctx context.Context, workspacePath string) (string, error)
	// MergePullRequest squash-merges an already-verified pull request and
	// optionally deletes its head branch.
	//
	// Unlike EnsurePullRequest it takes no workspace path: it is called for a
	// task in `done`, whose workspace the reaper may already have deleted, and
	// the caller has resolved owner/repo/number from the board record by then.
	// Every gate that decides WHETHER to merge lives above this call — the
	// adapter's only judgement is the head-SHA precondition it forwards to
	// GitHub.
	MergePullRequest(ctx context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error)
	// RevertCommitOnDefaultBranch reverts sha on origin's default branch and
	// pushes, returning the revert commit.
	//
	// It is the rollback mechanism for a repository with no deploy workflow —
	// a push-to-deploy host redeploys whatever the default branch points at, so
	// undoing a release there means adding a commit, not dispatching anything.
	// The implementation never force-pushes and aborts a conflicting revert
	// rather than resolving it: both of those, done unattended on the branch
	// production builds from, are worse than the outage being rolled back.
	RevertCommitOnDefaultBranch(ctx context.Context, rootPath, sha, message string) (revertSHA string, err error)
	TaskDiff(ctx context.Context, workspacePath string) (string, error)
	// TaskChangedFiles lists the paths the task branch changed against its
	// merge base — the input to the schema-change (migration) detector.
	TaskChangedFiles(ctx context.Context, workspacePath string) ([]string, error)
	// ChangedFilesSince lists the paths that changed between sha and HEAD —
	// what a reviewer is shown in place of re-litigating a verdict already on
	// record for code that never moved.
	ChangedFilesSince(ctx context.Context, workspacePath, sha string) ([]string, error)
	// TaskGitInfo resolves origin owner/repo + branch + HEAD SHA of a workspace.
	TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error)
}
