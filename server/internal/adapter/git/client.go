package git

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

// TokenSource, kayıtlı GitHub token'ını döndürür ("" = bağlı değil).
type TokenSource func(ctx context.Context) (string, error)

type Client struct {
	tokens TokenSource

	// identity caches the connected GitHub account behind the current token.
	// Resolving it costs an API round-trip, and every commit needs it, so it is
	// looked up once per token and kept for identityTTL — long enough that a
	// busy board does not spend a request per commit, short enough that
	// reconnecting a different account takes effect within the hour.
	identityMu    sync.Mutex
	identityToken string
	identity      githubapi.Identity
	identityAt    time.Time
}

const identityTTL = time.Hour

func NewClient() *Client {
	return &Client{}
}

// SetTokenSource, gh CLI yerine token tabanlı GitHub erişimini etkinleştirir.
func (c *Client) SetTokenSource(ts TokenSource) {
	c.tokens = ts
}

func (c *Client) token(ctx context.Context) string {
	if c.tokens == nil {
		return ""
	}
	tok, err := c.tokens(ctx)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tok)
}

// authFlags, https push/fetch için Authorization header'ı enjekte eder.
// x-access-token kullanıcı adı hem PAT hem GitHub App token'ları ile çalışır.
func authFlags(token string) []string {
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"-c", "http.https://github.com/.extraheader=Authorization: Basic " + basic}
}

// HasGit is the plain gate — "can git be run here" — that most callers want.
// Callers that have to explain a `false` to a human want Presence instead.
func (c *Client) HasGit(rootPath string) bool {
	return c.Presence(rootPath).IsRepository()
}

// Presence reports what is at rootPath, distinguishing a folder with no
// repository from a folder that is not there at all or cannot be read.
//
// Two stats at worst, and no `git rev-parse`: this runs once per repository in
// every list response, and a subprocess per row on that path is not a cost
// worth paying to be marginally more correct about pathological layouts.
func (c *Client) Presence(rootPath string) domain.GitPresence {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return domain.GitPresence{State: domain.GitPresencePathMissing}
	}
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err == nil {
		// A .git DIRECTORY is an ordinary clone. A .git FILE is a worktree or a
		// submodule checkout, where it holds a `gitdir:` pointer at the real
		// git directory — git works there exactly as it does in a clone, and
		// requiring IsDir() reported every one of them as "not a repository".
		if info.IsDir() || info.Mode().IsRegular() {
			return domain.GitPresence{State: domain.GitPresenceRepository}
		}
		// Something named .git that is neither: a socket, a device node. Not a
		// repository, and not a permissions problem either.
		return domain.GitPresence{State: domain.GitPresenceNoRepository}
	}
	if errors.Is(err, fs.ErrNotExist) {
		// No .git — but that only means "no repository" if the folder itself is
		// there. Otherwise the honest answer is about the missing folder.
		if _, rootErr := os.Stat(root); rootErr != nil {
			if errors.Is(rootErr, fs.ErrNotExist) {
				return domain.GitPresence{State: domain.GitPresencePathMissing}
			}
			return domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: statReason(rootErr)}
		}
		return domain.GitPresence{State: domain.GitPresenceNoRepository}
	}
	// Permissions, an I/O error, or a root path that is a file (ENOTDIR).
	return domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: statReason(err)}
}

// statReason strips the *fs.PathError wrapper so the reason reads "permission
// denied" rather than "stat /srv/work/repos/app/.git: permission denied": the
// path is already on screen next to the warning, and repeating it there buries
// the one word the reader needs.
func statReason(err error) string {
	var perr *fs.PathError
	if errors.As(err, &perr) && perr.Err != nil {
		return perr.Err.Error()
	}
	return err.Error()
}

func (c *Client) Status(ctx context.Context, rootPath string) domain.GitStatus {
	presence := c.Presence(rootPath)
	status := domain.GitStatus{Initialized: presence.IsRepository()}
	if !status.Initialized {
		// Same distinctions as the repository card: a path that is missing or
		// unreadable is not a project someone forgot to `git init`.
		status.Warning = presence.Warning()
		return status
	}
	if _, err := c.run(ctx, rootPath, "git", "remote", "get-url", "origin"); err == nil {
		status.HasOrigin = true
	} else {
		status.Warning = "no origin remote is configured"
	}
	return status
}

func (c *Client) EnsureRepoWithRemote(ctx context.Context, rootPath, name, owner string) error {
	if !c.HasGit(rootPath) {
		if _, err := c.run(ctx, rootPath, "git", "init"); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
		if _, err := c.run(ctx, rootPath, "git", "add", "-A"); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		if _, err := c.runEnv(ctx, rootPath, c.commitAuthorEnv(ctx), "git", "commit", "--allow-empty", "-m", "initial commit"); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}
	if _, err := c.run(ctx, rootPath, "git", "remote", "get-url", "origin"); err == nil {
		return nil
	}
	if tok := c.token(ctx); tok != "" {
		repo, err := githubapi.CreateRepoIn(ctx, tok, owner, sanitizeRepoName(name))
		if err != nil {
			return fmt.Errorf("github repo create: %w", err)
		}
		if _, err := c.run(ctx, rootPath, "git", "remote", "add", "origin", repo.CloneURL); err != nil {
			return fmt.Errorf("git remote add: %w", err)
		}
		args := append(authFlags(tok), "push", "-u", "origin", "HEAD")
		if out, err := c.run(ctx, rootPath, "git", args...); err != nil {
			return fmt.Errorf("git push: %w (%s)", err, strings.TrimSpace(out))
		}
		return nil
	}
	out, err := c.run(ctx, rootPath, "gh", "repo", "create", sanitizeRepoName(name), "--private", "--source=.", "--remote=origin", "--push")
	if err != nil {
		return fmt.Errorf("gh repo create: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// CloneRepo, GitHub reposunu dest'e klonlar; token varsa auth header eklenir.
func (c *Client) CloneRepo(ctx context.Context, cloneURL, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("clone parent: %w", err)
	}
	args := []string{"clone", cloneURL, dest}
	if tok := c.token(ctx); tok != "" {
		args = append(authFlags(tok), args...)
	}
	if out, err := c.run(ctx, filepath.Dir(dest), "git", args...); err != nil {
		return fmt.Errorf("git clone: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// OriginURL returns the working copy's origin remote, or "" when the path is
// not a repo or has no origin. This is how a repository row registered before
// remote_url existed gets backfilled while a clone is still on disk.
func (c *Client) OriginURL(ctx context.Context, rootPath string) string {
	if !c.HasGit(rootPath) {
		return ""
	}
	out, err := c.run(ctx, rootPath, "git", "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// FetchLatest updates the working copy's remote-tracking refs. It leaves the
// checked-out tree alone — SyncDefaultBranch is what advances that. Task
// branches are cut from origin's default branch, so a stale project root would
// otherwise base every new task on old code.
func (c *Client) FetchLatest(ctx context.Context, rootPath string) error {
	if !c.HasGit(rootPath) {
		return fmt.Errorf("not a git repository: %s", rootPath)
	}
	args := []string{"fetch", "--prune", "origin"}
	if tok := c.token(ctx); tok != "" {
		args = append(authFlags(tok), args...)
	}
	if out, err := c.run(ctx, rootPath, "git", args...); err != nil {
		return fmt.Errorf("git fetch: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// DefaultBranch reports origin's default branch, falling back to main/master
// when the symbolic ref is not set (a bare `git clone` of some hosts omits it).
func (c *Client) DefaultBranch(ctx context.Context, rootPath string) string {
	if out, err := c.run(ctx, rootPath, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return strings.TrimPrefix(ref, "origin/")
		}
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := c.run(ctx, rootPath, "git", "rev-parse", "--verify", "origin/"+candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// SyncDefaultBranch advances the working tree at rootPath to origin's default
// branch. Indexing walks the checked-out files, so without this a clone keeps
// serving the commit it was created at and every later push stays invisible to
// search no matter how often the index is rebuilt.
//
// The tree it advances is the platform's shared mirror of the repository —
// agents work in per-task workspaces cloned from it, never in it — so nothing
// is supposed to write here. When something has anyway (a stray edit, a
// half-applied patch, a wrong-branch checkout), the fast-forward fails and the
// clone silently freezes at an old commit: the UI kept showing a green
// "completed 100%" index built from stale code, with a merge error nobody
// could act on because the tree it complained about is not one a human works
// in.
//
// So a blocked sync recovers instead of reporting: force the checkout back
// onto origin's default branch, discarding whatever local state was in the
// way. That is safe precisely because this tree is disposable — the same
// reasoning that makes a hard reset unacceptable in a task workspace (see
// refreshTaskWorkspace, which preserves and stashes) makes it correct here.
// What was discarded is logged, never dropped in silence.
func (c *Client) SyncDefaultBranch(ctx context.Context, rootPath string) error {
	if !c.HasGit(rootPath) {
		return fmt.Errorf("not a git repository: %s", rootPath)
	}
	if err := c.FetchLatest(ctx, rootPath); err != nil {
		return err
	}
	branch := c.DefaultBranch(ctx, rootPath)
	if branch == "" {
		return fmt.Errorf("origin default branch could not be resolved")
	}
	target := "origin/" + branch

	out, err := c.run(ctx, rootPath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve current branch: %w (%s)", err, strings.TrimSpace(out))
	}
	current := strings.TrimSpace(out)

	// The happy path is unchanged: on the right branch with a clean tree, a
	// fast-forward is all this needs, and it touches nothing else.
	if current == branch {
		dirty, derr := c.hasLocalChanges(ctx, rootPath)
		if derr == nil && !dirty {
			if out, err := c.run(ctx, rootPath, "git", "merge", "--ff-only", target); err == nil {
				return nil
			} else {
				log.Warn().Str("root", rootPath).Str("branch", branch).Str("git", strings.TrimSpace(out)).Err(err).
					Msg("mirror clone: fast-forward failed, resetting onto origin")
			}
		}
	}

	c.logDiscardedMirrorState(ctx, rootPath, current, branch, target)

	// checkout -f -B handles both blockers in one command: it moves (or
	// creates) the default-branch ref at origin's commit and overwrites
	// modified tracked files. Untracked files are left alone — they are not
	// what blocks a fast-forward, and deleting them here could take out
	// anything a deployment keeps beside the checkout.
	if out, err := c.run(ctx, rootPath, "git", "checkout", "-f", "-B", branch, target); err != nil {
		return fmt.Errorf("git checkout -f -B %s %s: %w (%s)", branch, target, err, strings.TrimSpace(out))
	}
	if out, err := c.run(ctx, rootPath, "git", "reset", "--hard", target); err != nil {
		return fmt.Errorf("git reset --hard %s: %w (%s)", target, err, strings.TrimSpace(out))
	}
	return nil
}

// logDiscardedMirrorState records what the recovery is about to throw away.
// A hard reset that leaves no trace is indistinguishable from data loss when
// someone later asks where their edit went.
func (c *Client) logDiscardedMirrorState(ctx context.Context, rootPath, current, branch, target string) {
	event := log.Warn().Str("root", rootPath).Str("branch", branch)
	if current != branch {
		event = event.Str("was_on_branch", current)
	}
	if out, err := c.run(ctx, rootPath, "git", "status", "--porcelain", "--untracked-files=no"); err == nil {
		if changed := strings.TrimSpace(out); changed != "" {
			event = event.Str("discarded_changes", truncateForLog(changed))
		}
	}
	if out, err := c.run(ctx, rootPath, "git", "log", "--oneline", target+"..HEAD"); err == nil {
		if ahead := strings.TrimSpace(out); ahead != "" {
			event = event.Str("discarded_commits", truncateForLog(ahead))
		}
	}
	event.Msg("mirror clone had local state; resetting it onto origin (agents work in task workspaces, this tree is disposable)")
}

// truncateForLog keeps a log line readable when the discarded state is large.
func truncateForLog(s string) string {
	const max = 1000
	if len(s) <= max {
		return s
	}
	return s[:max] + " …(truncated)"
}

func (c *Client) EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error {
	if c.HasGit(workspacePath) {
		return c.refreshTaskWorkspace(ctx, projectRoot, workspacePath, branch)
	}
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		return fmt.Errorf("workspace parent: %w", err)
	}
	// Refresh the project root first: the task workspace is cloned from it, so
	// whatever is stale here becomes the base of the new task branch. A fetch
	// failure is not fatal — an offline run on slightly old code beats no run —
	// but it must be visible.
	baseIsFresh := true
	if err := c.FetchLatest(ctx, projectRoot); err != nil {
		baseIsFresh = false
		log.Warn().Err(err).Str("root", projectRoot).Msg("task workspace: could not refresh project root, branching from local state")
	}
	if out, err := c.run(ctx, filepath.Dir(workspacePath), "git", "clone", projectRoot, workspacePath); err != nil {
		return fmt.Errorf("git clone: %w (%s)", err, strings.TrimSpace(out))
	}
	origin := c.OriginURL(ctx, projectRoot)
	if origin != "" {
		if _, err := c.run(ctx, workspacePath, "git", "remote", "set-url", "origin", origin); err != nil {
			return fmt.Errorf("git remote set-url: %w", err)
		}
	}
	// Base the task branch on origin's current default branch rather than
	// whatever HEAD the source clone happened to sit on.
	startPoint := ""
	if origin != "" && baseIsFresh {
		if err := c.FetchLatest(ctx, workspacePath); err == nil {
			// The task branch may already exist on origin: the developer's run
			// pushed it and this workspace is a fresh clone (new pod, wiped
			// disk, or a reviewer picking the task up on another host). Cutting
			// a new branch off the default branch there would hand the run an
			// EMPTY diff of work that is sitting on origin — which is how a
			// code_review run got a branch with nothing to review.
			if _, err := c.run(ctx, workspacePath, "git", "rev-parse", "--verify", "origin/"+branch); err == nil {
				if _, err := c.run(ctx, workspacePath, "git", "checkout", "-B", branch, "origin/"+branch); err == nil {
					return nil
				}
				log.Warn().Str("workspace", workspacePath).Str("branch", branch).
					Msg("task workspace: origin has the task branch but checking it out failed, falling back to a fresh branch")
			}
			if def := c.DefaultBranch(ctx, workspacePath); def != "" {
				if _, err := c.run(ctx, workspacePath, "git", "rev-parse", "--verify", "origin/"+def); err == nil {
					startPoint = "origin/" + def
				}
			}
		} else {
			log.Warn().Err(err).Str("workspace", workspacePath).Msg("task workspace: origin fetch failed, branching from cloned HEAD")
		}
	}
	checkout := []string{"checkout", "-b", branch}
	if startPoint != "" {
		checkout = append(checkout, startPoint)
	}
	if _, err := c.run(ctx, workspacePath, "git", checkout...); err != nil {
		if _, chkErr := c.run(ctx, workspacePath, "git", "checkout", branch); chkErr != nil {
			return fmt.Errorf("git checkout %s: %w", branch, err)
		}
	}
	return nil
}

// workspaceRefreshTimeout bounds a whole refresh-on-reuse pass. The board run
// context stays alive for the agent's entire run, so without a deadline of its
// own a hung fetch (dead remote, credential prompt) would stall the run before
// the agent even starts. An incremental fetch plus a rebase on a clone that
// already exists is seconds of work; five minutes is the "something is wrong"
// line, and blowing it degrades to running on the tree as found, not to a
// half-rebased one — see restore below.
const workspaceRefreshTimeout = 5 * time.Minute

// workspaceStashLabel marks the stash entry a refresh creates, so an operator
// who has to recover one by hand can tell it from an agent's own stash.
const workspaceStashLabel = "tasktrooper: workspace refresh"

// refreshTaskWorkspace brings an already-existing task workspace up to date
// instead of handing it back as found.
//
// Task workspaces are keyed by task id and are never deleted, so every run on a
// task reuses the same directory: the developer's run, its verify/fix rounds,
// the pipeline, the reviewer, and any retry days later. Reuse used to be a bare
// `return nil`, which meant every run after the first worked on the tree the
// first one left behind — an agent could start on a base many commits behind
// origin and hand back a diff against code nobody runs any more.
//
// Rebase, not `reset --hard`. The workspace is emphatically not disposable: a
// run that ends in a clarification question commits nothing and leaves the
// agent's edits uncommitted for the run that answers it; a run that dies mid-way
// leaves them too; the out-of-budget path commits WIP precisely so the next run
// continues from it; and a review run never commits the tree it is judging. A
// reset would delete all of that, and the deletion would be invisible. So the
// tree's own work is preserved and conflicts are reported, never resolved by
// throwing one side away.
//
// Two invariants shape what "up to date" means:
//   - Published history is never rewritten. When origin already has the task
//     branch, local commits are replayed onto origin's version of it and that is
//     all: rebasing a published branch onto the default branch would rewrite
//     commits origin has, and the non-force `git push` this client uses would
//     then be rejected. The drift from the default branch is logged instead.
//   - Unpublished history is always re-based on the current default branch.
//     Nothing is at risk there, and that is where the stale base came from.
//
// Failures degrade rather than escalate, except conflicts: a fetch that fails
// (offline, no token) logs and leaves the branch alone — an old base beats no
// run — while a rebase conflict is fatal and actionable, because the alternative
// is an agent producing a diff that can never merge.
func (c *Client) refreshTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, workspaceRefreshTimeout)
	defer cancel()

	c.alignWorkspaceOrigin(ctx, projectRoot, workspacePath)

	// A workspace still pointing at the project root can only ever be as fresh as
	// the project root is, so refresh that first. Best effort in both directions:
	// a purely local project has no upstream to refresh from, which is the normal
	// state for that setup rather than an error.
	if isLocalPathRemote(c.OriginURL(ctx, workspacePath)) {
		if err := c.FetchLatest(ctx, projectRoot); err != nil {
			// Expected when the project is purely local (nothing upstream to
			// refresh from) and when two task refreshes fetch the shared root at
			// once (git declines the ref lock). Neither is worth failing a run.
			log.Debug().Err(err).Str("root", projectRoot).
				Msg("task workspace reuse: project root could not be refreshed, using it as it is")
		}
	}

	// A failed fetch is not fatal, but it does mean the remote-tracking refs are
	// whatever they were: rebasing onto them would be theatre, so the branch is
	// left alone below.
	fetched := true
	if err := c.FetchLatest(ctx, workspacePath); err != nil {
		fetched = false
		log.Warn().Err(err).Str("workspace", workspacePath).Str("branch", branch).
			Msg("task workspace reuse: origin fetch failed, continuing on the tree as it is")
	}

	dirty, err := c.hasLocalChanges(ctx, workspacePath)
	if err != nil {
		// The status of the tree is the one thing every decision below depends on.
		// Guessing it is how uncommitted work gets destroyed.
		return fmt.Errorf("task workspace %s: cannot read the working tree status: %w", workspacePath, err)
	}
	preRef, preSHA := c.headState(ctx, workspacePath)

	stashed := false
	// preSHA == "" is a checkout with no commit yet (cloned from an empty repo):
	// there is no state for a stash to be taken against, and nothing to rebase.
	// The branch checkout below is still worth doing.
	if dirty && preSHA != "" {
		saved, err := c.stashLocalChanges(ctx, workspacePath)
		if err != nil {
			return fmt.Errorf("task workspace %s holds uncommitted work that could not be set aside before refreshing it: %w", workspacePath, err)
		}
		stashed = saved
	}

	// restore returns the workspace to exactly the state it was found in. Every
	// failure path runs it: a half-rebased tree, or the agent's work stranded in
	// a stash nobody will look at, is worse than a stale workspace.
	//
	// It runs on its own context because the most likely reason to need it is the
	// refresh deadline expiring mid-rebase, and a cancelled context cannot clean
	// anything up.
	restore := func() {
		rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer rcancel()
		_, _ = c.run(rctx, workspacePath, "git", "rebase", "--abort")
		if stashed {
			// Clears a half-applied `stash pop`: the conflict left markers in the
			// tree but kept the stash entry, so nothing is lost by wiping it.
			_, _ = c.run(rctx, workspacePath, "git", "reset", "--hard")
		}
		if preSHA != "" {
			if preRef != "" {
				_, _ = c.run(rctx, workspacePath, "git", "checkout", "-B", preRef, preSHA)
			} else {
				_, _ = c.run(rctx, workspacePath, "git", "checkout", "--detach", preSHA)
			}
			_, _ = c.run(rctx, workspacePath, "git", "reset", "--hard", preSHA)
		}
		if stashed {
			if out, popErr := c.run(rctx, workspacePath, "git", "stash", "pop"); popErr != nil {
				log.Error().Err(popErr).Str("workspace", workspacePath).Str("output", strings.TrimSpace(out)).
					Msg("task workspace reuse: uncommitted work could not be put back and is still in `git stash` — recover it with `git stash list` in the workspace")
			}
		}
	}

	// Detached HEAD, a branch deleted from under the workspace, or a workspace
	// left on some other branch: all of them mean the run would otherwise commit
	// somewhere nobody is looking for its diff.
	if preRef != branch {
		if err := c.checkoutTaskBranch(ctx, workspacePath, branch); err != nil {
			restore()
			return fmt.Errorf("task workspace %s: %w", workspacePath, err)
		}
		if preRef == "" && preSHA != "" {
			// Commits made on a detached HEAD are not deleted by the checkout, but
			// they do become unreachable by name. Say where they went.
			if _, err := c.run(ctx, workspacePath, "git", "merge-base", "--is-ancestor", preSHA, "HEAD"); err != nil {
				log.Warn().Str("workspace", workspacePath).Str("detached_head", preSHA).Str("branch", branch).
					Msg("task workspace reuse: workspace was on a detached HEAD whose commit is not on the task branch; it is still reachable by SHA")
			}
		}
	}

	// The rebase only ever runs over a tree this function controls: one that was
	// clean to begin with, or one it set aside itself.
	if target := c.rebaseTarget(ctx, workspacePath, branch, fetched); target != "" && (!dirty || stashed) {
		// --no-autostash: the host's git config must not decide where the agent's
		// uncommitted work goes. This function owns the stash, or there is none.
		if out, err := c.run(ctx, workspacePath, "git", "rebase", "--no-autostash", target); err != nil {
			restore()
			return fmt.Errorf(
				"task workspace %s: task branch %s conflicts with %s and could not be rebased onto it; the rebase was aborted and the workspace left untouched — resolve it by hand (cd %s && git rebase %s) or delete the workspace to start the branch again: %w (%s)",
				workspacePath, branch, target, workspacePath, target, err, strings.TrimSpace(out))
		}
		c.logDefaultBranchDrift(ctx, workspacePath, branch, target)
	}

	if stashed {
		if out, err := c.run(ctx, workspacePath, "git", "stash", "pop"); err != nil {
			restore()
			return fmt.Errorf(
				"task workspace %s: uncommitted work in the workspace conflicts with what origin changed, so the refresh was rolled back and the tree left exactly as it was found — commit or resolve it before this task runs again: %w (%s)",
				workspacePath, err, strings.TrimSpace(out))
		}
	}
	return nil
}

// alignWorkspaceOrigin points a workspace at the project root's origin once the
// project has one. A workspace cloned while the project root had no remote kept
// the local path as its origin forever, so it kept "refreshing" from a directory
// that nobody pushes to. Reuse should converge on the shape EnsureTaskWorkspace
// gives a fresh clone. A workspace whose origin is already a URL is left alone —
// it is not this function's business which URL that is.
func (c *Client) alignWorkspaceOrigin(ctx context.Context, projectRoot, workspacePath string) {
	origin := c.OriginURL(ctx, projectRoot)
	if origin == "" {
		return
	}
	current := c.OriginURL(ctx, workspacePath)
	if current == origin || (current != "" && !isLocalPathRemote(current)) {
		return
	}
	verb := "set-url"
	if current == "" {
		verb = "add"
	}
	if out, err := c.run(ctx, workspacePath, "git", "remote", verb, "origin", origin); err != nil {
		log.Warn().Err(err).Str("workspace", workspacePath).Str("output", strings.TrimSpace(out)).
			Msg("task workspace reuse: origin could not be realigned with the project root")
	}
}

// rebaseTarget picks the ref a reused task branch should be brought up to, or ""
// when there is nothing trustworthy to move onto. See refreshTaskWorkspace for
// why a published branch resolves to itself rather than to the default branch.
func (c *Client) rebaseTarget(ctx context.Context, workspacePath, branch string, fetched bool) string {
	if !fetched {
		return ""
	}
	if c.refExists(ctx, workspacePath, "refs/remotes/origin/"+branch) {
		return "origin/" + branch
	}
	// No branch on origin: either it was never pushed, or it was merged and
	// deleted. Both make the default branch the right base — in the merged case
	// the rebase simply drops the commits origin already has.
	def := c.DefaultBranch(ctx, workspacePath)
	if def == "" || !c.refExists(ctx, workspacePath, "refs/remotes/origin/"+def) {
		return ""
	}
	return "origin/" + def
}

// checkoutTaskBranch puts the workspace back on the task branch, recreating it
// from origin (its own ref first, the default branch otherwise) when the local
// branch is gone.
func (c *Client) checkoutTaskBranch(ctx context.Context, workspacePath, branch string) error {
	if c.refExists(ctx, workspacePath, "refs/heads/"+branch) {
		if out, err := c.run(ctx, workspacePath, "git", "checkout", branch); err != nil {
			return fmt.Errorf("git checkout %s: %w (%s)", branch, err, strings.TrimSpace(out))
		}
		return nil
	}
	args := []string{"checkout", "-B", branch}
	if start := c.rebaseTarget(ctx, workspacePath, branch, true); start != "" {
		args = append(args, start)
	}
	if out, err := c.run(ctx, workspacePath, "git", args...); err != nil {
		return fmt.Errorf("git checkout -B %s: %w (%s)", branch, err, strings.TrimSpace(out))
	}
	return nil
}

// logDefaultBranchDrift reports a published task branch that has fallen behind
// the default branch. Nothing can be done about it here — rebasing it would
// rewrite commits origin already has — but the point of this whole function is
// that a stale workspace stops being silent.
func (c *Client) logDefaultBranchDrift(ctx context.Context, workspacePath, branch, target string) {
	def := c.DefaultBranch(ctx, workspacePath)
	if def == "" || target != "origin/"+branch {
		return
	}
	out, err := c.run(ctx, workspacePath, "git", "rev-list", "--count", "HEAD..origin/"+def)
	if err != nil {
		return
	}
	if behind := strings.TrimSpace(out); behind != "" && behind != "0" {
		log.Warn().Str("workspace", workspacePath).Str("branch", branch).Str("behind", behind).
			Msg("task workspace reuse: task branch is published and behind the default branch; it was not rebased because that would rewrite pushed commits")
	}
}

// hasLocalChanges reports whether the tree carries work that a refresh must
// preserve. Untracked files count: a file the agent created but never staged is
// exactly the work that must not disappear.
func (c *Client) hasLocalChanges(ctx context.Context, workspacePath string) (bool, error) {
	out, err := c.run(ctx, workspacePath, "git", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, fmt.Errorf("git status: %w (%s)", err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out) != "", nil
}

// stashLocalChanges sets the working tree aside and reports whether an entry was
// actually created. The answer comes from refs/stash moving rather than from
// git's "No local changes to save" message, which is translated in a localised
// environment — and popping a stash that was never pushed would fail the refresh
// for no reason.
func (c *Client) stashLocalChanges(ctx context.Context, workspacePath string) (bool, error) {
	before := c.revParse(ctx, workspacePath, "refs/stash")
	if out, err := c.run(ctx, workspacePath, "git", "stash", "push", "--include-untracked", "-m", workspaceStashLabel); err != nil {
		return false, fmt.Errorf("git stash push: %w (%s)", err, strings.TrimSpace(out))
	}
	after := c.revParse(ctx, workspacePath, "refs/stash")
	return after != "" && after != before, nil
}

// headState reports the checked-out branch ("" when HEAD is detached) and the
// commit HEAD points at ("" in a repository with no commits yet).
func (c *Client) headState(ctx context.Context, workspacePath string) (branch, sha string) {
	if out, err := c.run(ctx, workspacePath, "git", "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		branch = strings.TrimSpace(out)
	}
	return branch, c.revParse(ctx, workspacePath, "HEAD")
}

func (c *Client) refExists(ctx context.Context, workspacePath, ref string) bool {
	return c.revParse(ctx, workspacePath, ref) != ""
}

func (c *Client) revParse(ctx context.Context, workspacePath, ref string) string {
	out, err := c.run(ctx, workspacePath, "git", "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// isLocalPathRemote reports whether a remote is a directory on this machine —
// the shape EnsureTaskWorkspace leaves behind when it clones from the project
// root and the project root has no origin of its own. Asking the filesystem
// beats parsing, and a URL simply fails to stat.
func isLocalPathRemote(remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return false
	}
	info, err := os.Stat(remote)
	return err == nil && info.IsDir()
}

// TaskDiff returns stat + patch of the task branch against its base (branch
// point). Uncommitted changes are included. Empty string when nothing changed.
func (c *Client) TaskDiff(ctx context.Context, workspacePath string) (string, error) {
	if !c.HasGit(workspacePath) {
		return "", nil
	}
	base := ""
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if out, err := c.run(ctx, workspacePath, "git", "merge-base", "HEAD", ref); err == nil {
			base = strings.TrimSpace(out)
			break
		}
	}
	if base == "" {
		return "", nil
	}
	stat, err := c.run(ctx, workspacePath, "git", "diff", "--stat", base)
	if err != nil {
		return "", fmt.Errorf("git diff --stat: %w", err)
	}
	patch, err := c.run(ctx, workspacePath, "git", "diff", base)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	combined := strings.TrimSpace(stat)
	if p := strings.TrimSpace(patch); p != "" {
		combined += "\n\n" + p
	}
	return strings.TrimSpace(combined), nil
}

// TaskChangedFiles lists the paths the task branch changed against its merge
// base. The migration gate reads this: what a task touches is decided by the
// diff, never by what the agent says it did.
func (c *Client) TaskChangedFiles(ctx context.Context, workspacePath string) ([]string, error) {
	if !c.HasGit(workspacePath) {
		return nil, nil
	}
	base := ""
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if out, err := c.run(ctx, workspacePath, "git", "merge-base", "HEAD", ref); err == nil {
			base = strings.TrimSpace(out)
			break
		}
	}
	if base == "" {
		return nil, nil
	}
	out, err := c.run(ctx, workspacePath, "git", "diff", "--name-only", base)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only: %w", err)
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// ChangedFilesSince lists the paths that changed between sha and HEAD. Used
// to show a reviewer exactly what moved since their last verdict, instead of
// re-asking about everything.
func (c *Client) ChangedFilesSince(ctx context.Context, workspacePath, sha string) ([]string, error) {
	if sha == "" || !c.HasGit(workspacePath) {
		return nil, nil
	}
	out, err := c.run(ctx, workspacePath, "git", "diff", "--name-only", sha)
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only: %w", err)
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func (c *Client) CommitAndPush(ctx context.Context, workspacePath, message string) error {
	if _, err := c.run(ctx, workspacePath, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := c.run(ctx, workspacePath, "git", "diff", "--cached", "--quiet"); err != nil {
		author := c.commitAuthorEnv(ctx)
		if out, err := c.runEnv(ctx, workspacePath, author, "git", "commit", "-m", message); err != nil {
			return fmt.Errorf("git commit: %w (%s)", err, strings.TrimSpace(out))
		}
	}
	pushArgs := []string{"push", "-u", "origin", "HEAD"}
	if tok := c.token(ctx); tok != "" {
		pushArgs = append(authFlags(tok), pushArgs...)
	}
	if out, err := c.run(ctx, workspacePath, "git", pushArgs...); err != nil {
		return fmt.Errorf("git push: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// PushBranch publishes the checked-out branch as-is. Nothing is staged and
// nothing is committed: this runs in review workspaces, where the tree belongs
// to the developer being reviewed and `git add -A` would fold their leftovers
// into the branch under review.
func (c *Client) PushBranch(ctx context.Context, workspacePath string) error {
	pushArgs := []string{"push", "-u", "origin", "HEAD"}
	if tok := c.token(ctx); tok != "" {
		pushArgs = append(authFlags(tok), pushArgs...)
	}
	if out, err := c.run(ctx, workspacePath, "git", pushArgs...); err != nil {
		return fmt.Errorf("git push: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// RevertCommitOnDefaultBranch undoes sha on origin's default branch and pushes
// the revert. It is the rollback mechanism for a repository that has no deploy
// workflow to dispatch — a push-to-deploy host builds whatever the default
// branch points at, so the only thing that redeploys it is a new commit.
//
// Five properties, each one a way this could have gone wrong:
//
//	no force, ever. The push is a plain `git push origin <branch>`. A rollback
//	    that force-pushes production's branch to undo one commit destroys every
//	    commit that landed after it, which is the opposite of a rollback. A
//	    rejected push (somebody else merged between the fetch and the push) is
//	    returned as an error for a human to look at.
//	a hard reset onto origin BEFORE the revert. This tree is the platform's
//	    shared mirror, which SyncDefaultBranch already treats as disposable; a
//	    revert computed against a stale or dirty tree would push a diff nobody
//	    wrote.
//	`git revert` with no -m. A squash merge is an ordinary single-parent commit.
//	    `-m 1` is for a true merge commit and fails here with "mainline was
//	    specified but commit is not a merge" — the error a reader would spend an
//	    hour on.
//	--no-edit, and a conflicting revert is ABORTED rather than resolved. A
//	    revert that conflicts means later commits touched the same lines, and
//	    guessing which side wins in production is not something to do
//	    unattended. The tree is left clean and the error says what happened.
//	the revert SHA is read back and returned, so the caller reports the commit
//	    that actually landed rather than the one it asked for.
func (c *Client) RevertCommitOnDefaultBranch(ctx context.Context, rootPath, sha, message string) (string, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", fmt.Errorf("git revert: no commit given")
	}
	// A ref that starts with '-' is an argument, not a commit. git parses its
	// own argv and there is no shell here to blame it on.
	if strings.HasPrefix(sha, "-") {
		return "", fmt.Errorf("git revert: %q is not a commit", sha)
	}
	if err := c.FetchLatest(ctx, rootPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	branch := c.DefaultBranch(ctx, rootPath)
	if branch == "" {
		return "", fmt.Errorf("git revert: origin's default branch could not be resolved")
	}
	if out, err := c.run(ctx, rootPath, "git", "checkout", branch); err != nil {
		return "", fmt.Errorf("git checkout %s: %w (%s)", branch, err, strings.TrimSpace(out))
	}
	if out, err := c.run(ctx, rootPath, "git", "reset", "--hard", "origin/"+branch); err != nil {
		return "", fmt.Errorf("git reset onto origin/%s: %w (%s)", branch, err, strings.TrimSpace(out))
	}
	if out, err := c.run(ctx, rootPath, "git", "cat-file", "-e", sha+"^{commit}"); err != nil {
		return "", fmt.Errorf("git revert: %s is not a commit in this repository (%s)", sha, strings.TrimSpace(out))
	}

	author := c.commitAuthorEnv(ctx)
	// `git revert -m N` means "mainline parent N", not "message" — so the
	// custom message cannot be passed to revert at all and goes in through a
	// follow-up amend below.
	if out, err := c.runEnv(ctx, rootPath, author, "git", "revert", "--no-edit", sha); err != nil {
		// Leave nothing half-applied. `git revert --abort` restores the tree
		// whether the failure was a conflict or anything else; its own failure
		// is logged and does not replace the real error.
		if abortOut, abortErr := c.run(ctx, rootPath, "git", "revert", "--abort"); abortErr != nil {
			log.Debug().Str("output", strings.TrimSpace(abortOut)).Msg("git revert --abort after a failed revert")
		}
		return "", fmt.Errorf("git revert %s: %w (%s)", domain.ShortSHA(sha), err, strings.TrimSpace(out))
	}
	if strings.TrimSpace(message) != "" {
		if out, err := c.runEnv(ctx, rootPath, author, "git", "commit", "--amend", "-m", message); err != nil {
			return "", fmt.Errorf("git commit --amend on the revert: %w (%s)", err, strings.TrimSpace(out))
		}
	}

	pushArgs := []string{"push", "origin", branch}
	if tok := c.token(ctx); tok != "" {
		pushArgs = append(authFlags(tok), pushArgs...)
	}
	if out, err := c.run(ctx, rootPath, "git", pushArgs...); err != nil {
		// The revert exists locally and was NOT pushed. Say exactly that: a
		// caller that reads this as "rolled back" would report production
		// recovered while it is still running the bad release.
		return "", fmt.Errorf("git push of the revert to origin/%s failed — the revert was made locally and production is UNCHANGED: %w (%s)",
			branch, err, strings.TrimSpace(out))
	}
	revertSHA := c.revParse(ctx, rootPath, "HEAD")
	if revertSHA == "" {
		return "", fmt.Errorf("git revert: the revert was pushed but its commit could not be read back")
	}
	return revertSHA, nil
}

// EnsurePullRequest returns the branch's open pull request, opening one ready
// for review if it has none.
//
// It opened drafts until now (as EnsureDraftPR, and `gh pr create --draft`
// below). That made every task PR unmergeable: GitHub refuses to merge a draft
// and nothing in this system ever marked one ready, so the board could carry a
// task all the way to `done` with its change still sitting on a branch. The
// draft never bought anything either — the PR is opened at the moment the work
// is handed to review, which is precisely when a PR should be ready.
func (c *Client) EnsurePullRequest(ctx context.Context, workspacePath string) (string, error) {
	if tok := c.token(ctx); tok != "" {
		return c.ensurePullRequestAPI(ctx, workspacePath, tok)
	}
	if out, err := c.run(ctx, workspacePath, "gh", "pr", "view", "--json", "url", "-q", ".url"); err == nil {
		url := strings.TrimSpace(out)
		if url != "" {
			return url, nil
		}
	}
	// --fill alone lets gh derive the title from commit history, which on a
	// multi-commit branch collapses to the branch name — so the title is
	// taken explicitly from the last commit subject, same as the API path
	// below, and --fill is left to autofill only the body. `--title`
	// overrides `--fill`'s own title when both are given (confirmed with
	// `gh pr create --help`).
	title := ""
	if out, err := c.run(ctx, workspacePath, "git", "log", "-1", "--pretty=%s"); err == nil {
		title = strings.TrimSpace(out)
	}
	createArgs := []string{"pr", "create", "--fill"}
	if title != "" {
		createArgs = append(createArgs, "--title", title)
	}
	if out, err := c.run(ctx, workspacePath, "gh", createArgs...); err != nil {
		return "", fmt.Errorf("gh pr create: %w (%s)", err, strings.TrimSpace(out))
	}
	out, err := c.run(ctx, workspacePath, "gh", "pr", "view", "--json", "url", "-q", ".url")
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// MergePullRequest squash-merges a pull request the caller has already gated,
// then deletes its head branch when asked to.
//
// There is deliberately NO gh-CLI fallback, unlike every other method here.
// EnsurePullRequest can fall back because opening a PR twice is harmless and a
// missing PR is recoverable; a merge is neither. The safety property this whole
// path rests on is the expected-head-SHA precondition — merge exactly the commit
// the board verified, or nothing — and expressing it through the CLI depends on
// `gh pr merge --match-head-commit`, whose presence varies with the gh version
// installed on the host and cannot be verified from here. A merge that silently
// drops that precondition because the local gh is old is exactly the failure the
// precondition exists to prevent, so a deployment with no GitHub token gets a
// clear refusal instead. (The CLI-only path is the self-hosted developer setup;
// merging by hand there is one click.)
func (c *Client) MergePullRequest(ctx context.Context, req domain.PullRequestMergeRequest) (domain.PullRequestMergeResult, error) {
	var out domain.PullRequestMergeResult
	token := c.token(ctx)
	if token == "" {
		return out, fmt.Errorf("no GitHub token is connected, so the pull request cannot be merged from here (merging is not done through the gh CLI: the head-commit precondition that keeps an unreviewed push from being merged is not reliably available there)")
	}
	if req.Owner == "" || req.Repo == "" || req.Number <= 0 {
		return out, fmt.Errorf("merge needs owner, repo and pull request number (got %q/%q #%d)", req.Owner, req.Repo, req.Number)
	}
	if req.Undraft {
		if err := githubapi.MarkPullRequestReady(ctx, token, req.Owner, req.Repo, req.Number); err != nil {
			return out, err
		}
		out.Undrafted = true
	}
	merge, err := githubapi.MergePullRequest(ctx, token, req.Owner, req.Repo, req.Number,
		req.ExpectedHeadSHA, req.CommitTitle, req.CommitBody)
	if err != nil {
		return out, err
	}
	out.MergeCommitSHA = merge.SHA
	if !req.DeleteBranch || strings.TrimSpace(req.Branch) == "" {
		return out, nil
	}
	// Past the merge, nothing is fatal any more: the change is on the default
	// branch whatever happens to the branch that carried it. A failure here is
	// carried back as text so the caller can say "merged, branch left behind"
	// instead of reporting a merge that did happen as an error.
	if err := githubapi.DeleteBranch(ctx, token, req.Owner, req.Repo, req.Branch); err != nil {
		out.BranchDeleteError = err.Error()
		log.Warn().Err(err).Str("owner", req.Owner).Str("repo", req.Repo).Str("branch", req.Branch).
			Int("pull_request", req.Number).Msg("pull request merged but its branch could not be deleted")
		return out, nil
	}
	out.BranchDeleted = true
	return out, nil
}

func (c *Client) ensurePullRequestAPI(ctx context.Context, workspacePath, token string) (string, error) {
	origin, err := c.run(ctx, workspacePath, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url: %w", err)
	}
	owner, repoName, ok := githubapi.ParseOwnerRepo(origin)
	if !ok {
		return "", fmt.Errorf("origin is not a GitHub URL: %s", strings.TrimSpace(origin))
	}
	branchOut, err := c.run(ctx, workspacePath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	branch := strings.TrimSpace(branchOut)

	if url, err := githubapi.FindOpenPR(ctx, token, owner, repoName, owner, branch); err == nil && url != "" {
		return url, nil
	}

	repo, err := githubapi.GetRepo(ctx, token, owner, repoName)
	if err != nil {
		return "", fmt.Errorf("github repo get: %w", err)
	}
	title := branch
	if out, err := c.run(ctx, workspacePath, "git", "log", "-1", "--pretty=%s"); err == nil && strings.TrimSpace(out) != "" {
		title = strings.TrimSpace(out)
	}
	url, err := githubapi.CreatePullRequest(ctx, token, owner, repoName, branch, repo.DefaultBranch, title)
	if err != nil {
		return "", fmt.Errorf("github pr create: %w", err)
	}
	return url, nil
}

// TaskGitInfo resolves the GitHub coordinates the pipeline needs from a task
// workspace: origin owner/repo, the current branch, and its HEAD commit SHA.
func (c *Client) TaskGitInfo(ctx context.Context, workspacePath string) (domain.TaskGitInfo, error) {
	var info domain.TaskGitInfo
	origin, err := c.run(ctx, workspacePath, "git", "remote", "get-url", "origin")
	if err != nil {
		return info, fmt.Errorf("git remote get-url: %w", err)
	}
	owner, repoName, ok := githubapi.ParseOwnerRepo(origin)
	if !ok {
		return info, fmt.Errorf("origin is not a GitHub URL: %s", strings.TrimSpace(origin))
	}
	branchOut, err := c.run(ctx, workspacePath, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return info, fmt.Errorf("git rev-parse branch: %w", err)
	}
	shaOut, err := c.run(ctx, workspacePath, "git", "rev-parse", "HEAD")
	if err != nil {
		return info, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	info.Owner = owner
	info.Repo = repoName
	info.Branch = strings.TrimSpace(branchOut)
	info.HeadSHA = strings.TrimSpace(shaOut)
	return info, nil
}

func (c *Client) run(ctx context.Context, dir, name string, args ...string) (string, error) {
	return c.runEnv(ctx, dir, nil, name, args...)
}

// runEnv is run with per-command environment overrides appended last. os/exec
// de-duplicates the environment keeping the final occurrence of each key, so
// `extra` wins over the fallback identity below.
func (c *Client) runEnv(ctx context.Context, dir string, extra []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin")
	cmd.Env = append(cmd.Env, commitIdentityEnv()...)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// commitAuthorEnv attributes the commit to the connected GitHub account.
//
// GitHub links a commit to an account by the author email and nothing else. The
// fallback identity below (agents@tasktrooper.ai) belongs to no account, so
// every agent commit landed unattributed — and Vercel refuses to build a PR
// whose head commit it cannot trace to an account ("GitHub couldn't verify an
// account for commit ..."). Using the account's own noreply address makes the
// commit the user's, without ever touching their real email address.
//
// Returns nil when there is no token or the lookup failed: the fallback
// identity still produces a valid commit, and losing the work would be worse
// than losing the attribution.
func (c *Client) commitAuthorEnv(ctx context.Context) []string {
	id, ok := c.githubIdentity(ctx)
	if !ok {
		return nil
	}
	email := id.NoReplyEmail()
	return []string{
		"GIT_AUTHOR_NAME=" + id.Login,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + id.Login,
		"GIT_COMMITTER_EMAIL=" + email,
	}
}

// githubIdentity resolves (and caches) the account behind the current token.
// A failed lookup is cached too — a board committing every few seconds must not
// re-ask GitHub about a token it already knows is unusable.
func (c *Client) githubIdentity(ctx context.Context) (githubapi.Identity, bool) {
	token := c.token(ctx)
	if token == "" {
		return githubapi.Identity{}, false
	}
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if c.identityToken == token && time.Since(c.identityAt) < identityTTL {
		return c.identity, c.identity.NoReplyEmail() != ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	id, err := githubapi.UserIdentity(lookupCtx, token)
	if err != nil || id.NoReplyEmail() == "" {
		if err != nil {
			log.Warn().Err(err).Msg("github identity lookup failed; commits keep the fallback author")
		}
		c.identityToken, c.identity, c.identityAt = token, githubapi.Identity{}, time.Now()
		return githubapi.Identity{}, false
	}
	c.identityToken, c.identity, c.identityAt = token, id, time.Now()
	return id, true
}

// commitIdentityEnv gives every git process an author/committer identity.
//
// Container images have no global git config and no resolvable hostname, so
// `git commit` aborted with "Author identity unknown" — an agent's whole run
// (including the out-of-budget partial commit that exists to save the work)
// was discarded at the last step. The identity travels in the environment
// rather than `git config --global` so it holds for every working copy,
// including ones cloned before the process started, and is overridable per
// deployment.
func commitIdentityEnv() []string {
	name := envOrDefault("GIT_COMMITTER_NAME", "TaskTrooper Agent")
	email := envOrDefault("GIT_COMMITTER_EMAIL", "agents@tasktrooper.ai")
	return []string{
		"GIT_AUTHOR_NAME=" + envOrDefault("GIT_AUTHOR_NAME", name),
		"GIT_AUTHOR_EMAIL=" + envOrDefault("GIT_AUTHOR_EMAIL", email),
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
	}
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func sanitizeRepoName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-._")
	if s == "" {
		s = "project"
	}
	return s
}
