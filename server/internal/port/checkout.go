package port

import (
	"context"
	"errors"
)

// CheckoutGit is git in one checkout on this computer: what the executor
// answers, once a run is over, for the caller that coordinates the work.
type CheckoutGit interface {
	Status(ctx context.Context, dir string) (CheckoutStatus, error)
	Diff(ctx context.Context, dir string, query CheckoutDiffQuery) (CheckoutDiff, error)
	Log(ctx context.Context, dir string, query CheckoutLogQuery) ([]CheckoutCommit, error)
	// CommitPush commits everything the working tree holds onto Branch,
	// cutting Branch from what is checked out when it does not exist yet, and
	// pushes it to origin.
	CommitPush(ctx context.Context, dir string, req CheckoutCommitPush) (CheckoutPushResult, error)
}

var (
	// ErrCheckoutInvalid is a request the checkout cannot be asked: a
	// malformed branch or revision, an empty message.
	ErrCheckoutInvalid = errors.New("invalid checkout request")
	// ErrCheckoutConflict is a checkout whose state refuses the request: on
	// another branch while the requested one exists, or a branch that is the
	// remote's default.
	ErrCheckoutConflict = errors.New("the checkout refuses the request")
	// ErrCheckoutNotRepository is a directory that holds no git repository.
	ErrCheckoutNotRepository = errors.New("not a git checkout")
)

type CheckoutStatus struct {
	// Branch is empty on a detached HEAD.
	Branch string `json:"branch"`
	// Head is empty in a repository with no commit yet.
	Head     string               `json:"head"`
	Upstream string               `json:"upstream,omitempty"`
	Ahead    int                  `json:"ahead"`
	Behind   int                  `json:"behind"`
	Clean    bool                 `json:"clean"`
	Files    []CheckoutFileStatus `json:"files"`
}

// CheckoutFileStatus is one path git status lists: Index and Worktree are
// its porcelain X and Y letters ("." for unchanged, "?" for untracked).
type CheckoutFileStatus struct {
	Path     string `json:"path"`
	OrigPath string `json:"orig_path,omitempty"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
}

// CheckoutDiffQuery picks what a diff is against. An empty Base is the
// merge-base with the remote's default branch: the task's whole change,
// working tree included. "HEAD" is the uncommitted changes alone.
type CheckoutDiffQuery struct {
	Base     string
	NameOnly bool
	// MaxBytes caps the patch; 0 means the caller's default.
	MaxBytes int
}

type CheckoutDiff struct {
	// Base is the commit the diff is against; empty when the checkout has no
	// base to compare with (no remote default branch).
	Base      string   `json:"base"`
	Files     []string `json:"files"`
	Stat      string   `json:"stat,omitempty"`
	Patch     string   `json:"patch,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

// CheckoutLogQuery reads the newest Limit commits of HEAD, or of
// Base..HEAD when Base is set.
type CheckoutLogQuery struct {
	Base  string
	Limit int
}

type CheckoutCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

type CheckoutCommitPush struct {
	Message string
	Branch  string
	// Token is an optional GitHub token. It reaches git through the child's
	// environment (GIT_CONFIG_*), never its argv; without it the push uses
	// this computer's own git credentials.
	Token string
}

type CheckoutPushResult struct {
	Branch string `json:"branch"`
	// BranchCreated says Branch was cut here from what was checked out.
	BranchCreated bool     `json:"branch_created"`
	HeadBefore    string   `json:"head_before"`
	Head          string   `json:"head"`
	Committed     bool     `json:"committed"`
	Subject       string   `json:"subject"`
	Files         []string `json:"files"`
}
