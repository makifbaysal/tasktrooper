package localpreview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
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

	assert.Contains(t, []domain.LocalPreviewStatus{domain.LocalPreviewStarting, domain.LocalPreviewRunning}, preview.Status)

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

func TestAFailedPreviewStaysVisibleUntilCleared(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	repositoryID, taskID := uuid.New(), uuid.New()

	_, err := svc.Start(context.Background(), repositoryID, taskID, `echo "Another dev server is already running"; exit 1`)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		p, ok := svc.Status(repositoryID)
		return ok && p.Status == domain.LocalPreviewFailed
	}, 3*time.Second, 20*time.Millisecond, "a preview that died must still be reported, not vanish")

	p, _ := svc.Status(repositoryID)
	assert.Equal(t, taskID, p.TaskID)
	assert.NotEmpty(t, p.Detail)
	assert.Contains(t, p.LogTail, "Another dev server is already running")
	assert.Empty(t, loadState(svc.workspaceRoot), "an exited process is never persisted for the next boot to signal")

	svc.Stop(repositoryID)
	_, ok := svc.Status(repositoryID)
	assert.False(t, ok)
}

func TestStartStopsAStaleNextDevServerHoldingTheCheckout(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	taskID := uuid.New()
	workspacePath, err := workspace.TaskDir(svc.workspaceRoot, taskID)
	require.NoError(t, err)

	script := filepath.Join(t.TempDir(), "next-dev.sh")
	require.NoError(t, os.WriteFile(script, []byte("sleep 30\n"), 0o755))
	stale := exec.Command("sh", script)
	stale.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, stale.Start())
	exited := make(chan struct{})
	go func() { _ = stale.Wait(); close(exited) }()
	t.Cleanup(func() { _ = syscall.Kill(-stale.Process.Pid, syscall.SIGKILL) })

	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".next", "dev"), 0o755))
	lock := fmt.Sprintf(`{"pid":%d,"port":3000,"appUrl":"http://localhost:3000"}`, stale.Process.Pid)
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, ".next", "dev", "lock"), []byte(lock), 0o644))

	repositoryID := uuid.New()
	_, err = svc.Start(context.Background(), repositoryID, taskID, "sleep 30")
	require.NoError(t, err)
	t.Cleanup(func() { svc.Stop(repositoryID) })

	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("the next dev server named by the checkout's lock must be stopped before the preview starts")
	}
}

func TestStartLeavesALockWhosePidIsNotNextAlone(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	taskID := uuid.New()
	workspacePath, err := workspace.TaskDir(svc.workspaceRoot, taskID)
	require.NoError(t, err)

	other := exec.Command("sleep", "30")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, other.Start())
	t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); _ = other.Wait() })

	require.NoError(t, os.MkdirAll(filepath.Join(workspacePath, ".next", "dev"), 0o755))
	lock := fmt.Sprintf(`{"pid":%d}`, other.Process.Pid)
	require.NoError(t, os.WriteFile(filepath.Join(workspacePath, ".next", "dev", "lock"), []byte(lock), 0o644))

	repositoryID := uuid.New()
	_, err = svc.Start(context.Background(), repositoryID, taskID, "sleep 30")
	require.NoError(t, err)
	t.Cleanup(func() { svc.Stop(repositoryID) })

	assert.NoError(t, syscall.Kill(other.Process.Pid, 0), "a reused pid that is not a Next server must not be touched")
}

func TestNewServiceReapsAPreviousProcessesOrphan(t *testing.T) {
	workspaceRoot := t.TempDir()
	deps := func() Deps {
		return Deps{
			Tasks:         stubTasks{task: domain.BoardTask{ID: uuid.New(), Key: "T-9", TaskNumber: 9}},
			Repositories:  stubRepos{root: t.TempDir()},
			Git:           stubGit{has: true},
			WorkspaceRoot: workspaceRoot,
		}
	}

	first := NewService(deps())
	repositoryID := uuid.New()
	_, err := first.Start(context.Background(), repositoryID, uuid.New(), "sleep 30")
	require.NoError(t, err)

	first.mu.Lock()
	pid := first.active[repositoryID].cmd.Process.Pid
	first.mu.Unlock()
	require.NoError(t, syscall.Kill(pid, 0), "the preview's process must actually be running before this test means anything")

	NewService(deps())

	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil
	}, 2*time.Second, 20*time.Millisecond, "the orphaned process from the old Service must be reaped by the new one")
}

func TestStartRefusesAnEmptyCommand(t *testing.T) {
	svc := newTestService(t, t.TempDir())
	_, err := svc.Start(context.Background(), uuid.New(), uuid.New(), "   ")
	require.Error(t, err)
}

func TestDetectRunCommandPrefersNpmDevScript(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite","start":"node server.js"}}`), 0o644))
	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "npm run dev", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandFallsBackToNpmStart(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node server.js"}}`), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "npm start", cmd)
}

func TestDetectRunCommandFallsBackToGoRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "go run .", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandFindsNothingItRecognises(t *testing.T) {
	cmd, detail := DetectRunCommand(t.TempDir())
	assert.Empty(t, cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandUsesRootMainPackageOverCmd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "worker"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "worker", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "go run .", cmd)
}

func TestDetectRunCommandFindsTheSingleCmdMainPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "api"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", "api", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	cmd, detail := DetectRunCommand(dir)
	assert.Equal(t, "go run ./cmd/api", cmd)
	assert.Empty(t, detail)
}

func TestDetectRunCommandNamesSeveralCmdMainCandidates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644))
	for _, name := range []string{"api", "worker"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cmd", name, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	}

	cmd, detail := DetectRunCommand(dir)
	assert.Empty(t, cmd)
	assert.Contains(t, detail, "api, worker")
	assert.Contains(t, detail, "commandOverride")
}

func TestDetectRunCommandUsesMakefileRunTargetWhenNoDevTarget(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("run:\n\tgo run .\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "make run", cmd)
}

func TestDetectRunCommandPrefersMakefileDevOverRunTarget(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("dev:\n\tnpm run dev\nrun:\n\tnpm start\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "make dev", cmd)
}

func TestDetectRunCommandMavenSpringBoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./mvnw spring-boot:run", cmd)
}

func TestDetectRunCommandMavenQuarkus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><groupId>io.quarkus</groupId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./mvnw quarkus:dev", cmd)
}

func TestDetectRunCommandGradleSpringBoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins { id 'org.springframework.boot' version '3.3.0' }\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./gradlew bootRun", cmd)
}

func TestDetectRunCommandGradleQuarkusKotlinDSL(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradlew"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte("plugins {\n    id(\"io.quarkus\")\n}\n"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Equal(t, "./gradlew quarkusDev", cmd)
}

func TestDetectRunCommandSkipsMavenWithoutWrapper(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies><dependency><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>"), 0o644))
	cmd, _ := DetectRunCommand(dir)
	assert.Empty(t, cmd)
}

func TestAppendLineSynthesizesURLFromBarePort(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"go default log line", "2026/10/01 12:00:00 listening on :8080", "http://127.0.0.1:8080"},
		{"generic phrasing", "Listening on port 8080", "http://127.0.0.1:8080"},
		{"spring boot tomcat banner", "Tomcat started on port 8080 (http) with context path ''", "http://127.0.0.1:8080"},
		{"bare ports line", "port(s): 8080", "http://127.0.0.1:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &process{done: make(chan struct{})}
			p.appendLine(tc.line)
			assert.Equal(t, tc.want, p.url)
			assert.Equal(t, domain.LocalPreviewRunning, p.status)
		})
	}
}

func TestAppendLinePrefersExplicitURLOverPort(t *testing.T) {
	p := &process{done: make(chan struct{})}
	p.appendLine("listening on :8080")
	p.appendLine("Local: http://localhost:3000/")
	assert.Equal(t, "http://127.0.0.1:8080", p.url, "the first match wins; a later line must not override it")
}

func TestCloseIsSafeWithNothingActive(t *testing.T) {
	s := NewService(Deps{})
	s.Close()
	s.Close()
	if len(s.active) != 0 {
		t.Fatalf("active = %d after Close", len(s.active))
	}
}
