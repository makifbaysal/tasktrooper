//go:build unix

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
	persisted := loadState(workspaceRoot)
	require.Len(t, persisted, 1)
	assert.Equal(t, "sleep 30", persisted[0].Command, "the next boot needs the command to recognise the pid by")

	NewService(deps())

	require.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) != nil
	}, 2*time.Second, 20*time.Millisecond, "the orphaned process from the old Service must be reaped by the new one")
}

func TestNewServiceLeavesAPersistedPidItCannotVouchFor(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"the pid now runs a different command", "npm run dev"},
		{"a state file from before commands were recorded", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := exec.Command("sleep", "30")
			other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			require.NoError(t, other.Start())
			exited := make(chan struct{})
			go func() { _ = other.Wait(); close(exited) }()
			t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); <-exited })

			workspaceRoot := t.TempDir()
			saveState(workspaceRoot, []persistedEntry{{RepositoryID: uuid.New(), PID: other.Process.Pid, Command: tc.command}})

			NewService(Deps{WorkspaceRoot: workspaceRoot})

			assert.Empty(t, loadState(workspaceRoot), "a record that was not acted on is still forgotten")
			select {
			case <-exited:
				t.Fatal("a pid that cannot be shown to be the preview must not be signalled")
			case <-time.After(500 * time.Millisecond):
			}
		})
	}
}

func TestLeadsOwnGroupOnlyForAGroupLeader(t *testing.T) {
	leader := exec.Command("sleep", "30")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, leader.Start())
	t.Cleanup(func() { _ = leader.Process.Kill(); _ = leader.Wait() })

	member := exec.Command("sleep", "30")
	require.NoError(t, member.Start())
	t.Cleanup(func() { _ = member.Process.Kill(); _ = member.Wait() })

	assert.True(t, leadsOwnGroup(leader.Process.Pid))
	assert.False(t, leadsOwnGroup(member.Process.Pid))
	assert.False(t, isStalePreview(member.Process.Pid, "java -jar x.jar", "./mvnw spring-boot:run"),
		"a recycled pid inside someone else's group is never taken for the preview")
}
