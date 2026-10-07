package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/localdevice"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/mobilebuild"
	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/git"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// storeTestSource answers storeops.TestBuildSource from the task's own
// checkout: the commit a person reviewed in UAT is that checkout's HEAD.
type storeTestSource struct {
	git           *gitadapter.Client
	workspaceRoot string
	// remoteHead reads a pushed branch's head, for a task whose checkout is
	// not on this machine: one run on a company member's own computer, or one
	// whose checkout was reaped. GitHub Actions can still build that branch.
	remoteHead func(ctx context.Context, repo domain.Repository, branch string) (string, error)
}

var errNoLocalCheckout = errors.New("no checkout of the task on this machine")

var _ storeops.TestBuildSource = (*storeTestSource)(nil)

func (s *storeTestSource) dir(repo domain.Repository, task *domain.BoardTask) (string, error) {
	if task == nil {
		if repo.RootPath == "" {
			return "", errors.New("the repository has no local checkout")
		}
		return repo.RootPath, nil
	}
	dir, err := workspace.TaskDir(s.workspaceRoot, task.ID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("task %s: %w (%v)", task.Key, errNoLocalCheckout, err)
	}
	return dir, nil
}

func (s *storeTestSource) pushedHead(ctx context.Context, repo domain.Repository, task *domain.BoardTask, cause error) (string, string, error) {
	if task == nil || s.remoteHead == nil || !errors.Is(cause, errNoLocalCheckout) {
		return "", "", cause
	}
	branch := domain.TaskBranchName(*task)
	sha, err := s.remoteHead(ctx, repo, branch)
	if err != nil || sha == "" {
		return "", "", fmt.Errorf("%w; its branch %s is not on GitHub either: %v", cause, branch, err)
	}
	return sha, branch, nil
}

func (s *storeTestSource) Head(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (string, string, error) {
	dir, err := s.dir(repo, task)
	if err != nil {
		return s.pushedHead(ctx, repo, task, err)
	}
	info, err := s.git.TaskGitInfo(ctx, dir)
	if err != nil {
		return "", "", err
	}
	sha := info.HeadSHA
	if sha == "" {
		if sha, err = s.git.HeadSHA(ctx, dir); err != nil {
			return "", "", err
		}
	}
	return sha, info.Branch, nil
}

func (s *storeTestSource) Checkout(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (storeops.TestBuildCheckout, error) {
	dir, err := s.dir(repo, task)
	if err != nil {
		return storeops.TestBuildCheckout{}, err
	}
	sha, branch, err := s.Head(ctx, repo, task)
	if err != nil {
		return storeops.TestBuildCheckout{}, err
	}
	wt, remove, err := s.git.AddDetachedWorktree(ctx, dir, sha)
	if err != nil {
		return storeops.TestBuildCheckout{}, err
	}
	return storeops.TestBuildCheckout{Dir: wt, SHA: sha, Branch: branch, Cleanup: remove}, nil
}

func (s *storeTestSource) Publish(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (string, string, error) {
	dir, err := s.dir(repo, task)
	if err != nil {
		return s.pushedHead(ctx, repo, task, err)
	}
	if task != nil {
		if err := s.git.PushBranch(ctx, dir); err != nil {
			return "", "", err
		}
	}
	return s.Head(ctx, repo, task)
}

// simRunCheckout is the same checkout handed to the simulator flow.
type simRunCheckout struct{ src *storeTestSource }

func (c simRunCheckout) CheckoutTask(ctx context.Context, repo domain.Repository, task domain.BoardTask) (string, string, func(), error) {
	co, err := c.src.Checkout(ctx, repo, &task)
	if err != nil {
		return "", "", nil, err
	}
	return co.Dir, co.SHA, co.Cleanup, nil
}

// newLocalMobileBuilder is the engine on this machine. Its run directories
// live under the data directory rather than the system temp directory, and the
// sweep runs before the first build: a server killed mid-build leaves decoded
// signing material and a keychain in the search list behind.
func newLocalMobileBuilder(dataDir string) port.LocalMobileBuilder {
	root := filepath.Join(dataDir, "release-runs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		root = ""
	}
	sdk := ""
	if adb := localdevice.New(localdevice.Config{}).ADBPath(); adb != "" {
		sdk = filepath.Dir(filepath.Dir(adb))
	}
	b := mobilebuild.New(mobilebuild.Config{Root: root, AndroidSDK: sdk})
	b.Sweep()
	return b
}
