package localpreview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stubTasks struct {
	task domain.BoardTask
}

func (s stubTasks) Get(context.Context, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return s.task, nil
}

type stubRepos struct {
	root string
}

func (s stubRepos) ResolveRootPath(context.Context, uuid.UUID) (string, error) {
	return s.root, nil
}

// stubGit skips the real checkout: EnsureTaskWorkspace just points the caller
// at a directory the test already created, so the process-management half of
// this package can be tested without a git repository on disk.
type stubGit struct {
	has bool
}

func (s stubGit) HasGit(string) bool { return s.has }
func (s stubGit) EnsureTaskWorkspace(_ context.Context, _, workspacePath, _ string) error {
	return os.MkdirAll(workspacePath, 0o755)
}

func newTestService(t *testing.T, root string) *Service {
	t.Helper()
	workspaceRoot := t.TempDir()
	return NewService(Deps{
		Tasks:         stubTasks{task: domain.BoardTask{ID: uuid.New(), Key: "T-9", TaskNumber: 9}},
		Repositories:  stubRepos{root: root},
		Git:           stubGit{has: true},
		WorkspaceRoot: workspaceRoot,
	})
}

func TestStartDetectsTheURLTheCommandPrints(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	preview, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Local: http://localhost:4321/"; sleep 5`)
	require.NoError(t, err)
	assert.Equal(t, domain.LocalPreviewStarting, preview.Status)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewRunning
	}, 3*time.Second, 20*time.Millisecond)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, "http://localhost:4321/", p.URL)
	assert.NotEmpty(t, p.LogTail)

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok, "a stopped preview is no longer the repository's active one")
}

func TestStartingASecondPreviewReplacesTheFirst(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID := uuid.New()
	secondTaskID := uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, uuid.New(), "sleep 30")
	require.NoError(t, err)

	second, err := svc.Start(context.Background(), repositoryID, secondTaskID, "sleep 30")
	require.NoError(t, err)
	assert.Equal(t, secondTaskID, second.TaskID)

	p, ok := svc.Status(repositoryID)
	require.True(t, ok)
	assert.Equal(t, secondTaskID, p.TaskID, "only the second preview may be the repository's active one")

	svc.Stop(repositoryID)
	_, ok = svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestStartRefusesAnEmptyCommand(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	_, err := svc.Start(context.Background(), uuid.New(), uuid.New(), "   ")
	require.Error(t, err)
}

func TestDetectRunCommandPrefersNpmDevScript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite","start":"node server.js"}}`), 0o644))
	assert.Equal(t, "npm run dev", DetectRunCommand(dir))
}

func TestDetectRunCommandFallsBackToNpmStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o644))
	assert.Equal(t, "npm start", DetectRunCommand(dir))
}

func TestDetectRunCommandFallsBackToGoRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	assert.Equal(t, "go run .", DetectRunCommand(dir))
}

func TestDetectRunCommandFindsNothingItRecognises(t *testing.T) {
	assert.Equal(t, "", DetectRunCommand(t.TempDir()))
}
