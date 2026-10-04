package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workingCopyGit struct {
	port.GitClient
	cloneCalls       []string
	cloneErr         error
	cloneCreatesRepo bool
	origin           string
}

func (g *workingCopyGit) OriginURL(context.Context, string) string { return g.origin }

func (g *workingCopyGit) HasGit(rootPath string) bool {
	info, err := os.Stat(filepath.Join(rootPath, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func (g *workingCopyGit) CloneRepo(_ context.Context, cloneURL, dest string) error {
	g.cloneCalls = append(g.cloneCalls, cloneURL+" -> "+dest)
	if g.cloneErr != nil {
		return g.cloneErr
	}
	if g.cloneCreatesRepo {
		return os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	}
	return nil
}

func newWorkingCopyRunner(g port.GitClient) *Runner {
	return &Runner{git: g}
}

func TestEnsureWorkingCopy_ExistingRepoIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	g := &workingCopyGit{cloneCreatesRepo: true, origin: "git@github.com:acme/acme-web.git"}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	assert.Empty(t, g.cloneCalls, "an intact working copy must not be re-cloned")
}

func TestEnsureWorkingCopy_RestoresMissingCopyFromRemote(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	require.Len(t, g.cloneCalls, 1)
	assert.Contains(t, g.cloneCalls[0], "https://github.com/acme/acme-web.git")
}

func TestEnsureWorkingCopy_EmptyDirIsRestored(t *testing.T) {
	root := t.TempDir()
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.NoError(t, err)
	assert.Len(t, g.cloneCalls, 1)
}

func TestEnsureWorkingCopy_NoRemoteFailsLoudly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no remote_url on record")
	assert.Empty(t, g.cloneCalls)
	assert.NoDirExists(t, root, "a missing root must not be conjured into existence")
}

func TestEnsureWorkingCopy_NonEmptyNonRepoRefusesToRun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644))
	g := &workingCopyGit{cloneCreatesRepo: true}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.Empty(t, g.cloneCalls)
}

func TestEnsureWorkingCopy_CloneThatLeavesNoRepoIsAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "acme-web")
	g := &workingCopyGit{cloneCreatesRepo: false}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "still not a git repository after clone")
}

func TestEnsureWorkingCopy_RefusesADifferentRepository(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	g := &workingCopyGit{origin: "https://github.com/rival/acme-web.git"}

	err := newWorkingCopyRunner(g).ensureWorkingCopy(
		context.Background(),
		domain.Repository{Name: "acme-web", RemoteURL: "https://github.com/acme/acme-web.git"},
		root,
	)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "rival", "the refusal must not name the other repository")
	assert.Empty(t, g.cloneCalls, "the refusal must not clone over the directory")
	assert.DirExists(t, root, "the refusal must not remove what it refused")
}

type refreshFailingGit struct {
	workingCopyGit
	ensureCalls int
}

func (g *refreshFailingGit) EnsureTaskWorkspace(context.Context, string, string, string) error {
	g.ensureCalls++
	return errors.New("task branch feature/t-64 conflicts with origin/main and could not be rebased onto it")
}

func TestPrepareTaskWorkspace_MergedTaskRunsOnTheWorkspaceAsItIs(t *testing.T) {
	existing := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(existing, ".git"), 0o755))
	missing := filepath.Join(t.TempDir(), "never-cloned")

	cases := []struct {
		name    string
		task    domain.BoardTask
		ws      string
		wantErr bool
	}{
		{name: "merged task in done", task: domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "b53ee526cc18"}, ws: existing},
		{name: "merged task in released", task: domain.BoardTask{Column: domain.TaskColumnReleased, MergeCommitSHA: "b53ee526cc18"}, ws: existing},
		{name: "unmerged task in done", task: domain.BoardTask{Column: domain.TaskColumnDone}, ws: existing, wantErr: true},
		{name: "merged task sent back to need_revision", task: domain.BoardTask{Column: domain.TaskColumnNeedRevision, MergeCommitSHA: "b53ee526cc18"}, ws: existing, wantErr: true},
		{name: "merged task with no workspace on disk", task: domain.BoardTask{Column: domain.TaskColumnDone, MergeCommitSHA: "b53ee526cc18"}, ws: missing, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &refreshFailingGit{}
			err := (&Runner{git: g}).prepareTaskWorkspace(context.Background(), tc.task, "/root", tc.ws, "feature/t-64")
			assert.Equal(t, 1, g.ensureCalls)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
