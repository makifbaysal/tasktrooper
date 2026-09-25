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
	"regexp"
	"strings"
	"sync"
	"time"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/rs/zerolog/log"
)

type TokenSource func(ctx context.Context) (string, error)

type Client struct {
	tokens TokenSource

	identityMu    sync.Mutex
	identityToken string
	identity      githubapi.Identity
	identityAt    time.Time
}

const identityTTL = time.Hour

func NewClient() *Client {
	return &Client{}
}

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

func authFlags(token string) []string {
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{"-c", "http.https://github.com/.extraheader=Authorization: Basic " + basic}
}

func (c *Client) HasGit(rootPath string) bool {
	return c.Presence(rootPath).IsRepository()
}

func (c *Client) Presence(rootPath string) domain.GitPresence {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return domain.GitPresence{State: domain.GitPresencePathMissing}
	}
	info, err := os.Stat(filepath.Join(root, ".git"))
	if err == nil {
		if info.IsDir() || info.Mode().IsRegular() {
			return domain.GitPresence{State: domain.GitPresenceRepository}
		}

		return domain.GitPresence{State: domain.GitPresenceNoRepository}
	}
	if errors.Is(err, fs.ErrNotExist) {
		if _, rootErr := os.Stat(root); rootErr != nil {
			if errors.Is(rootErr, fs.ErrNotExist) {
				return domain.GitPresence{State: domain.GitPresencePathMissing}
			}
			return domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: statReason(rootErr)}
		}
		return domain.GitPresence{State: domain.GitPresenceNoRepository}
	}
	return domain.GitPresence{State: domain.GitPresenceUnreadable, Reason: statReason(err)}
}

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

	if out, err := c.run(ctx, rootPath, "git", "checkout", "-f", "-B", branch, target); err != nil {
		return fmt.Errorf("git checkout -f -B %s %s: %w (%s)", branch, target, err, strings.TrimSpace(out))
	}
	if out, err := c.run(ctx, rootPath, "git", "reset", "--hard", target); err != nil {
		return fmt.Errorf("git reset --hard %s: %w (%s)", target, err, strings.TrimSpace(out))
	}
	return nil
}

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

func truncateForLog(s string) string {
	const max = 1000
	if len(s) <= max {
		return s
	}
	return s[:max] + " …(truncated)"
}

// RemoteHead resolves the default branch's head commit on origin — a fetch
// plus a rev-parse, never touching rootPath's working tree or index.
func (c *Client) RemoteHead(ctx context.Context, rootPath string) (string, error) {
	if err := c.FetchLatest(ctx, rootPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	branch := c.DefaultBranch(ctx, rootPath)
	if branch == "" {
		return "", fmt.Errorf("git: origin's default branch could not be resolved")
	}
	sha := c.revParse(ctx, rootPath, "origin/"+branch)
	if sha == "" {
		return "", fmt.Errorf("git: origin/%s could not be resolved to a commit", branch)
	}
	return sha, nil
}

// IsAncestor reports whether ancestor is reachable from descendant
// (`git merge-base --is-ancestor`) — read-only, no fetch of its own: callers
// that need descendant fresh resolve it first (RemoteHead already fetches).
func (c *Client) IsAncestor(ctx context.Context, rootPath, ancestor, descendant string) (bool, error) {
	ancestor = strings.TrimSpace(ancestor)
	descendant = strings.TrimSpace(descendant)
	if ancestor == "" || descendant == "" {
		return false, fmt.Errorf("git merge-base --is-ancestor: both commits are required")
	}
	out, err := c.run(ctx, rootPath, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w (%s)", err, strings.TrimSpace(out))
}

// LatestTag is the newest tag matching glob (by version-aware sort — two tags
// cut moments apart must not tie on creation-date granularity), fetched fresh
// from origin first so a tag pushed by another process is seen; "" with a nil
// error means no matching tag exists yet.
func (c *Client) LatestTag(ctx context.Context, rootPath, glob string) (string, error) {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		glob = "*"
	}
	args := []string{"fetch", "--prune", "--tags", "origin"}
	if tok := c.token(ctx); tok != "" {
		args = append(authFlags(tok), args...)
	}
	if out, err := c.run(ctx, rootPath, "git", args...); err != nil {
		return "", fmt.Errorf("git fetch --tags: %w (%s)", err, strings.TrimSpace(out))
	}
	out, err := c.run(ctx, rootPath, "git", "tag", "--list", glob, "--sort=-v:refname")
	if err != nil {
		return "", fmt.Errorf("git tag --list: %w (%s)", err, strings.TrimSpace(out))
	}
	for _, line := range strings.Split(out, "\n") {
		if tag := strings.TrimSpace(line); tag != "" {
			return tag, nil
		}
	}
	return "", nil
}

func (c *Client) EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error {
	if c.HasGit(workspacePath) {
		return c.refreshTaskWorkspace(ctx, projectRoot, workspacePath, branch)
	}
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		return fmt.Errorf("workspace parent: %w", err)
	}

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

	startPoint := ""
	if origin != "" && baseIsFresh {
		if err := c.FetchLatest(ctx, workspacePath); err == nil {

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

const workspaceRefreshTimeout = 5 * time.Minute

const workspaceStashLabel = "tasktrooper: workspace refresh"

func (c *Client) refreshTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error {
	ctx, cancel := context.WithTimeout(ctx, workspaceRefreshTimeout)
	defer cancel()

	c.alignWorkspaceOrigin(ctx, projectRoot, workspacePath)

	if isLocalPathRemote(c.OriginURL(ctx, workspacePath)) {
		if err := c.FetchLatest(ctx, projectRoot); err != nil {
			log.Debug().Err(err).Str("root", projectRoot).
				Msg("task workspace reuse: project root could not be refreshed, using it as it is")
		}
	}

	fetched := true
	if err := c.FetchLatest(ctx, workspacePath); err != nil {
		fetched = false
		log.Warn().Err(err).Str("workspace", workspacePath).Str("branch", branch).
			Msg("task workspace reuse: origin fetch failed, continuing on the tree as it is")
	}

	dirty, err := c.hasLocalChanges(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("task workspace %s: cannot read the working tree status: %w", workspacePath, err)
	}
	preRef, preSHA := c.headState(ctx, workspacePath)

	stashed := false
	if dirty && preSHA != "" {
		saved, err := c.stashLocalChanges(ctx, workspacePath)
		if err != nil {
			return fmt.Errorf("task workspace %s holds uncommitted work that could not be set aside before refreshing it: %w", workspacePath, err)
		}
		stashed = saved
	}

	restore := func() {
		rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer rcancel()
		_, _ = c.run(rctx, workspacePath, "git", "rebase", "--abort")
		if stashed {
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

	if preRef != branch {
		if err := c.checkoutTaskBranch(ctx, workspacePath, branch); err != nil {
			restore()
			return fmt.Errorf("task workspace %s: %w", workspacePath, err)
		}
		if preRef == "" && preSHA != "" {
			if _, err := c.run(ctx, workspacePath, "git", "merge-base", "--is-ancestor", preSHA, "HEAD"); err != nil {
				log.Warn().Str("workspace", workspacePath).Str("detached_head", preSHA).Str("branch", branch).
					Msg("task workspace reuse: workspace was on a detached HEAD whose commit is not on the task branch; it is still reachable by SHA")
			}
		}
	}

	if target := c.rebaseTarget(ctx, workspacePath, branch, fetched); target != "" && (!dirty || stashed) {
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

func (c *Client) rebaseTarget(ctx context.Context, workspacePath, branch string, fetched bool) string {
	if !fetched {
		return ""
	}
	if c.refExists(ctx, workspacePath, "refs/remotes/origin/"+branch) {
		return "origin/" + branch
	}

	def := c.DefaultBranch(ctx, workspacePath)
	if def == "" || !c.refExists(ctx, workspacePath, "refs/remotes/origin/"+def) {
		return ""
	}
	return "origin/" + def
}

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

func (c *Client) hasLocalChanges(ctx context.Context, workspacePath string) (bool, error) {
	out, err := c.run(ctx, workspacePath, "git", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, fmt.Errorf("git status: %w (%s)", err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *Client) stashLocalChanges(ctx context.Context, workspacePath string) (bool, error) {
	before := c.revParse(ctx, workspacePath, "refs/stash")
	if out, err := c.run(ctx, workspacePath, "git", "stash", "push", "--include-untracked", "-m", workspaceStashLabel); err != nil {
		return false, fmt.Errorf("git stash push: %w (%s)", err, strings.TrimSpace(out))
	}
	after := c.revParse(ctx, workspacePath, "refs/stash")
	return after != "" && after != before, nil
}

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

func isLocalPathRemote(remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return false
	}
	info, err := os.Stat(remote)
	return err == nil && info.IsDir()
}

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

// RevertCommitOnDefaultBranch is RevertOnDefaultBranch for a single commit,
// kept for callers with only one sha to undo.
func (c *Client) RevertCommitOnDefaultBranch(ctx context.Context, rootPath, sha, message string) (string, error) {
	return c.RevertOnDefaultBranch(ctx, rootPath, []string{sha}, message)
}

var revertSHARe = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)

func validateRevertSHA(sha string) (string, error) {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return "", fmt.Errorf("git revert: no commit given")
	}
	if strings.HasPrefix(sha, "-") {
		return "", fmt.Errorf("git revert: %q is not a commit", sha)
	}
	if !revertSHARe.MatchString(sha) {
		return "", fmt.Errorf("git revert: %q is not a commit", sha)
	}
	return sha, nil
}

// RevertOnDefaultBranch is the rollback mechanism for a repository with no
// deploy workflow to dispatch: it undoes shas (in the given order) on the
// default branch and pushes. It runs entirely inside a detached git worktree
// cloned off origin — never `checkout`/`reset --hard` in rootPath itself — so
// an unattended rollback can never discard a human's uncommitted work sitting
// in the repository's root clone; the old behaviour did exactly that.
func (c *Client) RevertOnDefaultBranch(ctx context.Context, rootPath string, shas []string, message string) (string, error) {
	if len(shas) == 0 {
		return "", fmt.Errorf("git revert: no commits given")
	}
	valid := make([]string, 0, len(shas))
	for _, sha := range shas {
		v, err := validateRevertSHA(sha)
		if err != nil {
			return "", err
		}
		valid = append(valid, v)
	}
	if strings.TrimSpace(message) == "" {
		message = "Revert " + strings.Join(shasLabel(valid), ", ")
	}

	if err := c.FetchLatest(ctx, rootPath); err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	branch := c.DefaultBranch(ctx, rootPath)
	if branch == "" {
		return "", fmt.Errorf("git revert: origin's default branch could not be resolved")
	}
	for _, sha := range valid {
		if out, err := c.run(ctx, rootPath, "git", "cat-file", "-e", sha+"^{commit}"); err != nil {
			return "", fmt.Errorf("git revert: %s is not a commit in this repository (%s)", sha, strings.TrimSpace(out))
		}
	}
	// The caller's order (e.g. release.revertSHAsNewestFirst, which only
	// reverses release_tasks.added_at) is not trustworthy on its own — ties or
	// out-of-order carried tasks make it wrong — so the actual git history is
	// the source of truth here, regardless of what order was given.
	valid = c.revertOrderNewestFirst(ctx, rootPath, valid)

	parent, err := os.MkdirTemp("", "tasktrooper-revert-")
	if err != nil {
		return "", fmt.Errorf("git revert: creating the detached worktree directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(parent); rmErr != nil {
			log.Warn().Err(rmErr).Str("dir", parent).Msg("git revert: removing the worktree's temp directory failed")
		}
	}()
	worktree := filepath.Join(parent, "wt")

	if out, err := c.run(ctx, rootPath, "git", "worktree", "add", "--detach", worktree, "origin/"+branch); err != nil {
		return "", fmt.Errorf("git worktree add: %w (%s)", err, strings.TrimSpace(out))
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if out, rmErr := c.run(cleanupCtx, rootPath, "git", "worktree", "remove", "--force", worktree); rmErr != nil {
			log.Warn().Str("output", strings.TrimSpace(out)).Err(rmErr).Msg("git revert: removing the detached worktree failed")
		}
		if out, pruneErr := c.run(cleanupCtx, rootPath, "git", "worktree", "prune"); pruneErr != nil {
			log.Warn().Str("output", strings.TrimSpace(out)).Err(pruneErr).Msg("git revert: pruning worktrees failed")
		}
	}()

	author := c.commitAuthorEnv(ctx)

	revertArgs := append([]string{"revert", "--no-edit", "--no-commit"}, valid...)
	if out, err := c.runEnv(ctx, worktree, author, "git", revertArgs...); err != nil {
		detail := strings.TrimSpace(out)
		if conflicts := c.conflictingPaths(ctx, worktree); len(conflicts) > 0 {
			detail = "conflicts in " + strings.Join(conflicts, ", ")
		}
		if abortOut, abortErr := c.run(ctx, worktree, "git", "revert", "--abort"); abortErr != nil {
			log.Debug().Str("output", strings.TrimSpace(abortOut)).Msg("git revert --abort after a failed revert")
		}
		return "", fmt.Errorf("git revert %s: %w (%s)", strings.Join(shasLabel(valid), ", "), err, detail)
	}

	if out, err := c.runEnv(ctx, worktree, author, "git", "commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit on the revert: %w (%s)", err, strings.TrimSpace(out))
	}

	pushArgs := []string{"push", "origin", "HEAD:refs/heads/" + branch}
	if tok := c.token(ctx); tok != "" {
		pushArgs = append(authFlags(tok), pushArgs...)
	}
	if out, err := c.run(ctx, worktree, "git", pushArgs...); err != nil {
		return "", fmt.Errorf("git push of the revert to origin/%s failed — the revert was NOT pushed and production is unchanged: %w (%s)",
			branch, err, strings.TrimSpace(out))
	}
	revertSHA := c.revParse(ctx, worktree, "HEAD")
	if revertSHA == "" {
		return "", fmt.Errorf("git revert: the revert was pushed but its commit could not be read back")
	}
	return revertSHA, nil
}

// revertOrderNewestFirst sorts shas newest-first by git's own commit graph
// (§H5), ignoring whatever order the caller passed in — release_tasks'
// added_at (what the caller's best-effort order is built from) can tie or be
// out of order for carried tasks, and reverting out of order conflicts with
// itself (undoing an older change while a newer one still sits on top of it).
// `git rev-list --no-walk=sorted` (commit-date order) was tried first and
// rejected: task merge commits landing within the same wall-clock second give
// it nothing to break the tie with, so its order is not reliable. Ancestry
// is: every one of a release's own commits sits on the SAME default-branch
// history, so any two of them are always comparable — one is always an
// ancestor of the other — which a plain insertion sort on `git merge-base
// --is-ancestor` gets right regardless of timestamps.
func (c *Client) revertOrderNewestFirst(ctx context.Context, rootPath string, shas []string) []string {
	ordered := append([]string{}, shas...)
	for i := 1; i < len(ordered); i++ {
		// ordered[j-1] being an ancestor of ordered[j] means it is the OLDER
		// of the two, so newest-first puts it after — swap leftward until
		// that is no longer true (or the two are unrelated).
		for j := i; j > 0 && c.isAncestor(ctx, rootPath, ordered[j-1], ordered[j]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}

// isAncestor reports whether ancestor is an ancestor of (or equal to)
// descendant.
func (c *Client) isAncestor(ctx context.Context, rootPath, ancestor, descendant string) bool {
	_, err := c.run(ctx, rootPath, "git", "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// conflictingPaths reads the paths a failed `git revert` left unmerged, so
// the caller's error can name them instead of dumping raw git output.
func (c *Client) conflictingPaths(ctx context.Context, dir string) []string {
	out, err := c.run(ctx, dir, "git", "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

func shasLabel(shas []string) []string {
	out := make([]string, len(shas))
	for i, s := range shas {
		out[i] = domain.ShortSHA(s)
	}
	return out
}

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

func (c *Client) runEnv(ctx context.Context, dir string, extra []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin")
	cmd.Env = append(cmd.Env, commitIdentityEnv()...)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

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
