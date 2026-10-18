package ghrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestHost(t *testing.T, dist *Distribution) (*Host, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewHost(LocalConfig{
		Dir:  dir,
		Dist: dist,
		Environ: func() []string {
			return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "SERVER_API_KEY=must-not-leak"}
		},
		StopGrace: 2 * time.Second,
	})
	t.Cleanup(h.Close)
	return h, dir
}

func hostRepoOf(t *testing.T, h *Host, repo string) HostRepo {
	t.Helper()
	for _, r := range h.Status().Repos {
		if r.Repo == repo {
			return r
		}
	}
	return HostRepo{}
}

func TestHostRunsOneRegistrationAtATimeAndReportsTheJob(t *testing.T) {
	h, dir := newTestHost(t, fakeRunner(t, jobScript))
	if err := h.Start("acme/app", "jit-1"); err != ErrHostNotReady {
		t.Fatalf("a repository that was never prepared started: %v", err)
	}
	if err := h.Prepare("acme/app"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "an installed runner", func() bool { return hostRepoOf(t, h, "acme/app").State == HostReady })

	if err := h.Start("acme/app", "jit-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the runner to take its job and end", func() bool { return hostRepoOf(t, h, "acme/app").State == HostExited })
	got := hostRepoOf(t, h, "acme/app")
	if !got.TookJob || got.LastJob != "build" || got.LastResult != "Succeeded" || got.LastFinishedAt == nil {
		t.Fatalf("job not reported: %+v", got)
	}
	seen, err := os.ReadFile(filepath.Join(installDir(dir, "acme/app"), "seen-config"))
	if err != nil || string(seen) != "jit-1" {
		t.Fatalf("the runner did not get its config through the environment: %q %v", seen, err)
	}
	env, _ := os.ReadFile(filepath.Join(installDir(dir, "acme/app"), "seen-env"))
	if strings.Contains(string(env), "must-not-leak") {
		t.Fatal("the executor's environment reached the runner")
	}
	if !h.Status().Supported || len(h.Status().Labels) == 0 {
		t.Fatalf("status: %+v", h.Status())
	}
}

func TestHostRefusesASecondRunnerForTheSameRepositoryAndStopsTheFirst(t *testing.T) {
	h, _ := newTestHost(t, fakeRunner(t, idleScript))
	if err := h.Prepare("acme/app"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "an installed runner", func() bool { return hostRepoOf(t, h, "acme/app").State == HostReady })
	if err := h.Start("acme/app", "jit-1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "an idle runner", func() bool { return hostRepoOf(t, h, "acme/app").State == HostIdle })
	if err := h.Start("acme/app", "jit-2"); err != ErrHostBusy {
		t.Fatalf("a second runner started beside the first: %v", err)
	}
	h.Stop("acme/app", true)
	if got := hostRepoOf(t, h, "acme/app"); got.Repo != "" {
		t.Fatalf("a removed repository is still listed: %+v", got)
	}
	if _, err := os.Stat(installDir(h.l.cfg.Dir, "acme/app")); !os.IsNotExist(err) {
		t.Fatalf("the install was not deleted: %v", err)
	}
}
