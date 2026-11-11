package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var _ port.RunUndoGit = (*Client)(nil)

var (
	branchNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/\-]*$`)
	undoBackupRe = regexp.MustCompile(`^tasktrooper/undo-backup/[0-9a-f\-]{8,40}(-[0-9]+)?$`)

	// Names no undo resets even if the repository's default-branch lookup fails.
	neverResetBranches = map[string]bool{"main": true, "master": true, "trunk": true, "develop": true, "production": true, "HEAD": true}
)

func validBranch(name string) bool {
	return branchNameRe.MatchString(name) && !strings.Contains(name, "..") && !strings.HasSuffix(name, "/") && !strings.HasSuffix(name, ".lock")
}

func validCleanPath(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "-") || strings.ContainsRune(p, 0) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func (c *Client) gitLines(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := c.run(ctx, dir, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git %s: %w (%s)", args[0], err, strings.TrimSpace(out))
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func (c *Client) countRange(ctx context.Context, dir, rng string) int {
	out, err := c.run(ctx, dir, "git", "rev-list", "--count", rng)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

func (c *Client) UndoState(ctx context.Context, ws, base, branch string) (port.UndoGitState, error) {
	var st port.UndoGitState
	if _, err := validateRevertSHA(base); err != nil {
		return st, err
	}
	head, err := c.HeadSHA(ctx, ws)
	if err != nil {
		return st, err
	}
	st.Head = head
	if out, err := c.run(ctx, ws, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(out)
	}
	st.DefaultBranch = c.DefaultBranch(ctx, ws)
	_, ancestorErr := c.run(ctx, ws, "git", "merge-base", "--is-ancestor", base, "HEAD")
	st.BaseIsAncestor = ancestorErr == nil
	if !st.BaseIsAncestor {
		return st, nil
	}
	lines, err := c.gitLines(ctx, ws, "log", "--format=%H%x1f%s", base+"..HEAD")
	if err != nil {
		return st, err
	}
	for _, l := range lines {
		sha, subject, _ := strings.Cut(l, "\x1f")
		st.Commits = append(st.Commits, port.UndoCommit{SHA: sha, Subject: subject})
	}
	if st.Uncommitted, err = c.ChangesSince(ctx, ws, "HEAD"); err != nil {
		return st, err
	}
	if st.Changes, err = c.ChangesSince(ctx, ws, base); err != nil {
		return st, err
	}
	if validBranch(branch) {
		remote := "refs/remotes/origin/" + branch
		if out, err := c.run(ctx, ws, "git", "rev-parse", "--verify", "-q", remote); err == nil {
			st.RemoteBranchSHA = strings.TrimSpace(out)
			st.Pushed = c.countRange(ctx, ws, base+".."+remote) > 0
			st.RemoteAhead = c.countRange(ctx, ws, "HEAD.."+remote) > 0
		}
	}
	return st, nil
}

// snapshotWorkingTree commits everything the checkout holds, tracked or not
// and committed or not, onto a commit whose parent is HEAD, through a private
// index so the real index and working tree are untouched.
func (c *Client) snapshotWorkingTree(ctx context.Context, ws, message string) (string, error) {
	tmp, err := os.MkdirTemp("", "tasktrooper-undo-index-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	if out, err := c.runEnv(ctx, ws, env, "git", "read-tree", "HEAD"); err != nil {
		return "", fmt.Errorf("git read-tree: %w (%s)", err, strings.TrimSpace(out))
	}
	if out, err := c.runEnv(ctx, ws, env, "git", "add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w (%s)", err, strings.TrimSpace(out))
	}
	tree, err := c.runEnv(ctx, ws, env, "git", "write-tree")
	if err != nil {
		return "", fmt.Errorf("git write-tree: %w (%s)", err, strings.TrimSpace(tree))
	}
	commit, err := c.runEnv(ctx, ws, nil, "git", "commit-tree", strings.TrimSpace(tree), "-p", "HEAD", "-m", message)
	if err != nil {
		return "", fmt.Errorf("git commit-tree: %w (%s)", err, strings.TrimSpace(commit))
	}
	return strings.TrimSpace(commit), nil
}

func (c *Client) UndoReset(ctx context.Context, ws string, plan port.UndoPlan) (port.UndoOutcome, error) {
	var out port.UndoOutcome
	if !validBranch(plan.Branch) {
		return out, fmt.Errorf("undo: %q is not a branch name", plan.Branch)
	}
	base, err := validateRevertSHA(plan.Base)
	if err != nil {
		return out, err
	}
	if !undoBackupRe.MatchString(plan.BackupRef) {
		return out, fmt.Errorf("undo: %q is not an undo backup name", plan.BackupRef)
	}
	current, err := c.run(ctx, ws, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return out, fmt.Errorf("git rev-parse: %w", err)
	}
	if strings.TrimSpace(current) != plan.Branch {
		return out, fmt.Errorf("undo: the checkout is on %q, not on the task branch %q", strings.TrimSpace(current), plan.Branch)
	}
	if def := c.DefaultBranch(ctx, ws); plan.Branch == def || neverResetBranches[plan.Branch] {
		return out, fmt.Errorf("undo: %q is the repository's main line and is never reset", plan.Branch)
	}
	if _, err := c.run(ctx, ws, "git", "merge-base", "--is-ancestor", base, "HEAD"); err != nil {
		return out, fmt.Errorf("undo: the run's starting commit is no longer behind this branch")
	}

	backup, err := c.snapshotWorkingTree(ctx, ws, "tasktrooper undo backup of "+plan.Branch)
	if err != nil {
		return out, fmt.Errorf("undo: the backup could not be made, nothing was changed: %w", err)
	}
	ref := plan.BackupRef
	for i := 2; c.refExists(ctx, ws, "refs/heads/"+ref); i++ {
		ref = fmt.Sprintf("%s-%d", plan.BackupRef, i)
	}
	if o, err := c.run(ctx, ws, "git", "update-ref", "refs/heads/"+ref, backup); err != nil {
		return out, fmt.Errorf("undo: the backup branch could not be written, nothing was changed: %w (%s)", err, strings.TrimSpace(o))
	}
	out.BackupRef, out.BackupSHA = ref, backup

	if o, err := c.run(ctx, ws, "git", "reset", "--hard", base); err != nil {
		return out, fmt.Errorf("undo: git reset failed (the backup is %s): %w (%s)", ref, err, strings.TrimSpace(o))
	}
	for _, p := range plan.CleanPaths {
		if !validCleanPath(p) {
			continue
		}
		untracked, err := c.gitLines(ctx, ws, "ls-files", "--others", "--exclude-standard", "--", p)
		if err != nil || len(untracked) == 0 {
			continue
		}
		if o, err := c.run(ctx, ws, "git", "clean", "-fd", "--", p); err != nil {
			return out, fmt.Errorf("undo: git clean %s: %w (%s)", p, err, strings.TrimSpace(o))
		}
		out.CleanedPaths = append(out.CleanedPaths, p)
	}
	out.LeftUntracked, _ = c.gitLines(ctx, ws, "ls-files", "--others", "--exclude-standard")
	return out, nil
}

func (c *Client) ForcePushBranch(ctx context.Context, ws, branch, expectRemoteSHA string) error {
	if !validBranch(branch) {
		return fmt.Errorf("undo: %q is not a branch name", branch)
	}
	if def := c.DefaultBranch(ctx, ws); branch == def || neverResetBranches[branch] {
		return fmt.Errorf("undo: %q is the repository's main line and is never force-pushed", branch)
	}
	if _, err := validateRevertSHA(expectRemoteSHA); err != nil {
		return err
	}
	lease := "--force-with-lease=" + branch + ":" + expectRemoteSHA
	if out, err := c.runEnv(ctx, ws, c.authEnv(ctx), "git", "push", lease, "origin", "refs/heads/"+branch+":refs/heads/"+branch); err != nil {
		return pushError(err, out)
	}
	return nil
}

func (c *Client) RevertMerge(ctx context.Context, root string, req port.RevertRequest) (port.RevertOutcome, error) {
	var out port.RevertOutcome
	sha, err := validateRevertSHA(req.MergeSHA)
	if err != nil {
		return out, err
	}
	if !validBranch(req.Branch) || neverResetBranches[req.Branch] {
		return out, fmt.Errorf("revert: %q is not a branch to create", req.Branch)
	}
	if err := c.FetchLatest(ctx, root); err != nil {
		return out, fmt.Errorf("revert: fetching the repository: %w", err)
	}
	def := c.DefaultBranch(ctx, root)
	if def == "" {
		return out, fmt.Errorf("revert: the repository's default branch is unknown")
	}
	wt, remove, err := c.AddDetachedWorktree(ctx, root, "origin/"+def)
	if err != nil {
		return out, err
	}
	defer remove()

	if o, err := c.run(ctx, wt, "git", "checkout", "-b", req.Branch); err != nil {
		return out, fmt.Errorf("revert: git checkout -b: %w (%s)", err, strings.TrimSpace(o))
	}
	args := []string{"revert", "--no-edit"}
	if parents, err := c.run(ctx, wt, "git", "rev-list", "--parents", "-n", "1", sha); err == nil && len(strings.Fields(parents)) > 2 {
		args = append(args, "-m", "1")
	}
	args = append(args, sha)
	if o, err := c.runEnv(ctx, wt, c.commitAuthorEnv(ctx), "git", args...); err != nil {
		_, _ = c.run(ctx, wt, "git", "revert", "--abort")
		return out, fmt.Errorf("revert: git revert %s: %w (%s)", sha, err, strings.TrimSpace(o))
	}
	if head, err := c.HeadSHA(ctx, wt); err == nil {
		out.RevertSHA = head
	}
	if o, err := c.runEnv(ctx, wt, c.authEnv(ctx), "git", "push", "-u", "origin", "HEAD"); err != nil {
		return out, pushError(err, o)
	}
	out.Branch = req.Branch
	if url, err := c.EnsurePullRequest(ctx, wt); err != nil {
		out.PRError = err.Error()
	} else {
		out.PRURL = url
	}
	return out, nil
}
