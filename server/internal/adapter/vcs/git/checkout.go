package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	defaultDiffBytes = 256 << 10
	maxDiffBytes     = 4 << 20
	defaultLogLimit  = 20
	maxLogLimit      = 200
	maxCommitMessage = 64 << 10
)

// branchName and revisionName are narrower than git's own rules: both become
// argv, and a name that cannot begin with a dash cannot become a flag.
var (
	branchName   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,254}$`)
	revisionName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/@^~-]{0,254}$`)
)

// Checkout is port.CheckoutGit over this computer's git. A push token is per
// call, so each call gets a client of its own.
type Checkout struct {
	clientFor func(token string) *Client
}

var _ port.CheckoutGit = (*Checkout)(nil)

func NewCheckout() *Checkout {
	return &Checkout{clientFor: tokenClient}
}

func tokenClient(token string) *Client {
	c := NewClient()
	if token = strings.TrimSpace(token); token != "" {
		c.SetTokenSource(func(context.Context) (string, error) { return token, nil })
	}
	return c
}

func (k *Checkout) repository(dir, token string) (*Client, error) {
	c := k.clientFor(token)
	if !c.HasGit(dir) {
		return nil, fmt.Errorf("%w: %s", port.ErrCheckoutNotRepository, dir)
	}
	return c, nil
}

func (k *Checkout) Status(ctx context.Context, dir string) (port.CheckoutStatus, error) {
	c, err := k.repository(dir, "")
	if err != nil {
		return port.CheckoutStatus{}, err
	}
	out, err := c.run(ctx, dir, "git", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return port.CheckoutStatus{}, fmt.Errorf("git status: %w (%s)", err, strings.TrimSpace(out))
	}
	return parseStatusV2(out), nil
}

// parseStatusV2 reads `git status --porcelain=v2 --branch -z`: NUL-separated
// entries, a rename carrying its original path as the next entry.
func parseStatusV2(out string) port.CheckoutStatus {
	status := port.CheckoutStatus{Files: []port.CheckoutFileStatus{}}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		switch {
		case strings.HasPrefix(entry, "# branch.oid "):
			if oid := strings.TrimPrefix(entry, "# branch.oid "); oid != "(initial)" {
				status.Head = oid
			}
		case strings.HasPrefix(entry, "# branch.head "):
			if head := strings.TrimPrefix(entry, "# branch.head "); head != "(detached)" {
				status.Branch = head
			}
		case strings.HasPrefix(entry, "# branch.upstream "):
			status.Upstream = strings.TrimPrefix(entry, "# branch.upstream ")
		case strings.HasPrefix(entry, "# branch.ab "):
			for _, field := range strings.Fields(strings.TrimPrefix(entry, "# branch.ab ")) {
				n, _ := strconv.Atoi(strings.TrimLeft(field, "+-"))
				if strings.HasPrefix(field, "+") {
					status.Ahead = n
				} else {
					status.Behind = n
				}
			}
		case strings.HasPrefix(entry, "1 "):
			if f := strings.SplitN(entry, " ", 9); len(f) == 9 {
				status.Files = append(status.Files, fileStatus(f[1], f[8], ""))
			}
		case strings.HasPrefix(entry, "2 "):
			if f := strings.SplitN(entry, " ", 10); len(f) == 10 {
				orig := ""
				if i+1 < len(entries) {
					orig = entries[i+1]
					i++
				}
				status.Files = append(status.Files, fileStatus(f[1], f[9], orig))
			}
		case strings.HasPrefix(entry, "u "):
			if f := strings.SplitN(entry, " ", 11); len(f) == 11 {
				status.Files = append(status.Files, fileStatus(f[1], f[10], ""))
			}
		case strings.HasPrefix(entry, "? "):
			status.Files = append(status.Files, port.CheckoutFileStatus{Path: strings.TrimPrefix(entry, "? "), Index: "?", Worktree: "?"})
		}
	}
	status.Clean = len(status.Files) == 0
	return status
}

func fileStatus(xy, path, orig string) port.CheckoutFileStatus {
	index, worktree := ".", "."
	if len(xy) == 2 {
		index, worktree = xy[:1], xy[1:]
	}
	return port.CheckoutFileStatus{Path: path, OrigPath: orig, Index: index, Worktree: worktree}
}

func (k *Checkout) Diff(ctx context.Context, dir string, query port.CheckoutDiffQuery) (port.CheckoutDiff, error) {
	c, err := k.repository(dir, "")
	if err != nil {
		return port.CheckoutDiff{}, err
	}
	base, err := k.diffBase(ctx, c, dir, query.Base)
	if err != nil {
		return port.CheckoutDiff{}, err
	}
	diff := port.CheckoutDiff{Base: base, Files: []string{}}
	if base == "" {
		return diff, nil
	}
	names, err := c.run(ctx, dir, "git", "diff", "--name-only", "-z", base)
	if err != nil {
		return port.CheckoutDiff{}, fmt.Errorf("git diff --name-only: %w (%s)", err, strings.TrimSpace(names))
	}
	diff.Files = append(diff.Files, nulSeparated(names)...)
	if query.NameOnly || len(diff.Files) == 0 {
		return diff, nil
	}
	stat, err := c.run(ctx, dir, "git", "-c", "core.quotepath=off", "diff", "--stat", base)
	if err != nil {
		return port.CheckoutDiff{}, fmt.Errorf("git diff --stat: %w (%s)", err, strings.TrimSpace(stat))
	}
	patch, err := c.run(ctx, dir, "git", "-c", "core.quotepath=off", "diff", base)
	if err != nil {
		return port.CheckoutDiff{}, fmt.Errorf("git diff: %w (%s)", err, strings.TrimSpace(patch))
	}
	limit := query.MaxBytes
	if limit <= 0 {
		limit = defaultDiffBytes
	}
	if limit > maxDiffBytes {
		limit = maxDiffBytes
	}
	if len(patch) > limit {
		patch, diff.Truncated = patch[:limit], true
	}
	diff.Stat, diff.Patch = strings.TrimSpace(stat), patch
	return diff, nil
}

// diffBase resolves an empty base to the merge-base with the remote's default
// branch, the base a task's own diff is read against everywhere else.
func (k *Checkout) diffBase(ctx context.Context, c *Client, dir, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master"} {
			if out, err := c.run(ctx, dir, "git", "merge-base", "HEAD", ref); err == nil {
				return strings.TrimSpace(out), nil
			}
		}
		return "", nil
	}
	if !revisionName.MatchString(base) || strings.Contains(base, "..") {
		return "", fmt.Errorf("%w: base %q is not a revision this executor will use", port.ErrCheckoutInvalid, base)
	}
	sha := c.revParse(ctx, dir, base+"^{commit}")
	if sha == "" {
		return "", fmt.Errorf("%w: base %q names no commit in this checkout", port.ErrCheckoutInvalid, base)
	}
	return sha, nil
}

const logFieldSep, logRecordSep = "\x1f", "\x1e"

func (k *Checkout) Log(ctx context.Context, dir string, query port.CheckoutLogQuery) ([]port.CheckoutCommit, error) {
	c, err := k.repository(dir, "")
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}
	args := []string{"log", "-n", strconv.Itoa(limit), "--format=%H%x1f%s%x1f%an%x1f%aI%x1e"}
	if base := strings.TrimSpace(query.Base); base != "" {
		resolved, err := k.diffBase(ctx, c, dir, base)
		if err != nil {
			return nil, err
		}
		args = append(args, resolved+"..HEAD")
	}
	commits := []port.CheckoutCommit{}
	if _, head := c.headState(ctx, dir); head == "" {
		return commits, nil
	}
	out, err := c.run(ctx, dir, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("git log: %w (%s)", err, strings.TrimSpace(out))
	}
	for _, record := range strings.Split(out, logRecordSep) {
		fields := strings.Split(strings.TrimLeft(record, "\n"), logFieldSep)
		if len(fields) != 4 || fields[0] == "" {
			continue
		}
		commits = append(commits, port.CheckoutCommit{SHA: fields[0], Subject: fields[1], Author: fields[2], Date: fields[3]})
	}
	return commits, nil
}

// CommitPush never moves work off the branch it was written on: a checkout
// on another branch while Branch already exists here is refused rather than
// switched, which would leave that work behind.
func (k *Checkout) CommitPush(ctx context.Context, dir string, req port.CheckoutCommitPush) (port.CheckoutPushResult, error) {
	branch := strings.TrimSpace(req.Branch)
	if !branchName.MatchString(branch) || strings.Contains(branch, "..") || strings.HasSuffix(branch, ".lock") || strings.HasSuffix(branch, "/") {
		return port.CheckoutPushResult{}, fmt.Errorf("%w: branch %q is not a branch name this executor will push", port.ErrCheckoutInvalid, branch)
	}
	message := strings.TrimSpace(req.Message)
	if message == "" || len(message) > maxCommitMessage || strings.ContainsRune(message, 0) {
		return port.CheckoutPushResult{}, fmt.Errorf("%w: a commit message of 1 to %d bytes without NUL is required", port.ErrCheckoutInvalid, maxCommitMessage)
	}
	c, err := k.repository(dir, req.Token)
	if err != nil {
		return port.CheckoutPushResult{}, err
	}
	if def := c.DefaultBranch(ctx, dir); def != "" && def == branch {
		return port.CheckoutPushResult{}, fmt.Errorf("%w: %q is the remote's default branch; a task's work is pushed to a branch of its own", port.ErrCheckoutConflict, branch)
	}

	current, before := c.headState(ctx, dir)
	result := port.CheckoutPushResult{Branch: branch, HeadBefore: before, Files: []string{}}
	if current != branch {
		if c.refExists(ctx, dir, "refs/heads/"+branch) {
			on := current
			if on == "" {
				on = "a detached HEAD"
			}
			return port.CheckoutPushResult{}, fmt.Errorf("%w: the checkout is on %s and a branch %q already exists here; switching would leave the work on %s behind",
				port.ErrCheckoutConflict, on, branch, on)
		}
		if out, err := c.run(ctx, dir, "git", "checkout", "-b", branch); err != nil {
			return port.CheckoutPushResult{}, fmt.Errorf("git checkout -b: %w (%s)", err, strings.TrimSpace(out))
		}
		result.BranchCreated = true
	}

	if err := c.CommitAndPush(ctx, dir, message); err != nil {
		return result, err
	}
	_, result.Head = c.headState(ctx, dir)
	result.Committed = result.Head != "" && result.Head != before
	if out, err := c.run(ctx, dir, "git", "log", "-1", "--pretty=%s"); err == nil {
		result.Subject = strings.TrimSpace(out)
	}
	if files, err := c.TaskChangedFiles(ctx, dir); err == nil && files != nil {
		result.Files = files
	}
	return result, nil
}
