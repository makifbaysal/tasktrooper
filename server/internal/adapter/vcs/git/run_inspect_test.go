package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace"
	"github.com/makifbaysal/tasktrooper/server/internal/application/runtrace/runtracetest"
)

func newInspectRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "--initial-branch=main")
	for name, body := range map[string]string{"a.txt": "a1\n", "b.txt": "b1\n", "keep.txt": "k\n", "dir/c.txt": "c\n"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	return dir
}

func TestChangesSinceSeesCommittedStagedUnstagedAndUntracked(t *testing.T) {
	dir := newInspectRepo(t)
	c := NewClient()
	base, err := c.HeadSHA(context.Background(), dir)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a2\n"), 0o644))
	gitRun(t, dir, "commit", "-am", "edit a")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b2\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "dir/c.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("n\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ignored.log"), []byte("x"), 0o644))

	files, err := c.ChangesSince(context.Background(), dir, base)
	require.NoError(t, err)
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	require.Equal(t, map[string]string{"a.txt": "M", "b.txt": "M", "dir/c.txt": "D", "fresh.txt": "?", ".gitignore": "?"}, got)

	n, err := c.TrackedFileCount(context.Background(), dir, base)
	require.NoError(t, err)
	require.Equal(t, 4, n)
}

func TestTraceVersusGitFlagsTheShellEditButNotTheToolEdit(t *testing.T) {
	dir := newInspectRepo(t)
	c := NewClient()
	base, err := c.HeadSHA(context.Background(), dir)
	require.NoError(t, err)

	store := runtracetest.NewMemStore()
	meta := runtrace.Meta{RunID: uuid.New(), TaskID: uuid.New(), AgentID: uuid.New(), WorkDir: dir, WorkspacePath: dir, BaseSHA: base}
	rec := runtrace.Start(context.Background(), store, meta)

	edit := rec.Begin(runtrace.CallStart{Tool: "edit_file", Arguments: `{"path":"a.txt"}`})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a2\n"), 0o644))
	edit.End(runtrace.CallResult{Content: "ok"})

	shell := rec.Begin(runtrace.CallStart{Tool: "run_terminal", Arguments: `{"command":"sed -i s/b1/b2/ b.txt"}`})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b2\n"), 0o644))
	shell.End(runtrace.CallResult{Content: "[no output]"})

	read := rec.Begin(runtrace.CallStart{Tool: "read_file", Arguments: `{"path":"keep.txt"}`})
	read.End(runtrace.CallResult{Content: "k"})

	runtrace.Finalize(context.Background(), store, c, meta, 0)
	v := store.Finished
	require.NotNil(t, v)
	require.True(t, v.Available)
	require.Equal(t, []string{"b.txt"}, v.Unrecorded, "b.txt changed in git but no change call recorded it")
	require.Empty(t, v.RecordedOnly)
	require.Len(t, v.Files, 2)
	require.Equal(t, 4, v.TrackedFiles)
	require.Equal(t, 1, v.Untouched, "c.txt is the only tracked file neither read nor changed")
	head, _ := c.HeadSHA(context.Background(), dir)
	require.Equal(t, head, store.Head)
}
