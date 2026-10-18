//go:build !windows

package ghrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	githubapi "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/github"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeAPI struct {
	mu      sync.Mutex
	access  map[string]githubapi.RepoAccess
	jitErr  error
	issued  []string
	deleted []int64
	labels  []string
	next    int64
}

func (f *fakeAPI) RepoAccess(_ context.Context, owner, repo string) (githubapi.RepoAccess, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.access[owner+"/"+repo]
	if !ok {
		return githubapi.RepoAccess{}, errors.New("no such repo")
	}
	return a, nil
}

func (f *fakeAPI) GenerateJITConfig(_ context.Context, owner, repo, name string, labels []string, workFolder string) (githubapi.JITRunner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.jitErr != nil {
		return githubapi.JITRunner{}, f.jitErr
	}
	f.next++
	f.issued = append(f.issued, name)
	f.labels = labels
	return githubapi.JITRunner{ID: f.next, EncodedConfig: "jit-config-" + name}, nil
}

func (f *fakeAPI) DeleteRunner(_ context.Context, owner, repo string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeAPI) snapshot() (issued int, deleted []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.issued), append([]int64(nil), f.deleted...)
}

type memStore struct {
	mu  sync.Mutex
	cfg domain.GitHubRunnerConfig
}

func (s *memStore) GitHubRunnerConfig(context.Context) (domain.GitHubRunnerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, nil
}

func (s *memStore) SetGitHubRunnerConfig(_ context.Context, cfg domain.GitHubRunnerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	return nil
}

// fakeRunner packages a run.sh that behaves like the real one from the
// outside: it prints the listener's lines and exits once its "job" is done.
func fakeRunner(t *testing.T, script string) *Distribution {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "actions-runner-fake.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "run.sh", body: script, mode: 0o755}})
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(srv.Close)
	return &Distribution{URL: srv.URL + "/actions-runner-fake.tar.gz", SHA256: hex.EncodeToString(sum[:])}
}

// jobScript takes one job, the first time; every later registration waits
// idle, as a real runner does when nothing is queued.
const jobScript = `#!/bin/sh
here=$(cd "$(dirname "$0")" && pwd)
printf '%s' "$ACTIONS_RUNNER_INPUT_JITCONFIG" > "$here/seen-config"
env > "$here/seen-env"
echo "2026-10-16 10:00:00Z: Listening for Jobs"
if [ -f "$here/job-done" ]; then
  trap 'exit 0' TERM
  while :; do sleep 0.05; done
fi
touch "$here/job-done"
echo "2026-10-16 10:00:01Z: Running job: build"
mkdir -p "$here/_work/checkout"
echo "2026-10-16 10:00:02Z: Job build completed with result: Succeeded"
exit 0
`

const idleScript = `#!/bin/sh
echo "2026-10-16 10:00:00Z: Listening for Jobs"
trap 'exit 0' TERM
while :; do sleep 0.05; done
`

func newTestManager(t *testing.T, api *fakeAPI, dist *Distribution) (*Manager, *memStore) {
	t.Helper()
	store := &memStore{cfg: domain.GitHubRunnerConfig{Repos: []string{}}}
	m := New(Config{
		Dir:      t.TempDir(),
		API:      api,
		Store:    store,
		Dist:     dist,
		Hostname: "Test-Mac.local",
		Environ: func() []string {
			return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "SERVER_API_KEY=must-not-leak", "MCP_SECRETS_KEY=must-not-leak"}
		},
		Candidates: func(context.Context) ([]string, error) { return []string{"acme/app", "acme/site"}, nil },
		RetryMin:   20 * time.Millisecond,
		RetryMax:   50 * time.Millisecond,
		StopGrace:  2 * time.Second,
	})
	t.Cleanup(m.Close)
	return m, store
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func repoStatus(t *testing.T, m *Manager, repo string) domain.GitHubRunnerRepoStatus {
	t.Helper()
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range st.Repos {
		if r.Repo == repo {
			return r
		}
	}
	return domain.GitHubRunnerRepoStatus{}
}

func TestARunnerTakesAJobAndRegistersAgainForTheNext(t *testing.T) {
	api := &fakeAPI{access: map[string]githubapi.RepoAccess{"acme/app": {Private: true, Admin: true}}}
	m, store := newTestManager(t, api, fakeRunner(t, jobScript))

	st, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: true, Repos: []string{"acme/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || len(st.Repos) != 1 {
		t.Fatalf("status after enabling: %+v", st)
	}
	waitFor(t, "a finished job and a second registration", func() bool {
		issued, _ := api.snapshot()
		r := repoStatus(t, m, "acme/app")
		return issued >= 2 && r.LastJob == "build" && r.LastResult == "Succeeded"
	})
	if !store.cfg.Enabled || store.cfg.Repos[0] != "acme/app" {
		t.Fatalf("saved config: %+v", store.cfg)
	}

	dir := m.launcher.(*Local).dir("acme/app")
	seen, err := os.ReadFile(filepath.Join(dir, "seen-config"))
	if err != nil || !strings.HasPrefix(string(seen), "jit-config-tasktrooper-test-mac-") {
		t.Fatalf("the runner did not get its config through the environment: %q %v", seen, err)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "seen-env"))
	if strings.Contains(string(env), "must-not-leak") {
		t.Fatal("a server secret reached the runner's environment")
	}
	if got := strings.Join(api.labels, ","); got != strings.Join(m.launcher.Labels(), ",") || !strings.Contains(got, "self-hosted") {
		t.Fatalf("labels = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "_work", "checkout")); err == nil {
		t.Fatal("the last job's work folder was left behind")
	}
}

func TestStoppingAnIdleRunnerRemovesItsRegistrationAndInstall(t *testing.T) {
	api := &fakeAPI{access: map[string]githubapi.RepoAccess{"acme/app": {Private: true, Admin: true}}}
	m, _ := newTestManager(t, api, fakeRunner(t, idleScript))

	if _, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: true, Repos: []string{"acme/app"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "an idle runner", func() bool { return repoStatus(t, m, "acme/app").State == domain.GitHubRunnerIdle })
	dir := m.launcher.(*Local).dir("acme/app")

	st, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: false, Repos: []string{"acme/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Repos[0].State != domain.GitHubRunnerStopped {
		t.Fatalf("status after turning off: %+v", st)
	}
	if _, deleted := api.snapshot(); len(deleted) != 1 || deleted[0] != 1 {
		t.Fatalf("deleted registrations = %v, want [1]", deleted)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a stopped repository's install was left on disk")
	}
}

func TestConfigureRefusesARepositoryThatCannotSafelyHostARunner(t *testing.T) {
	api := &fakeAPI{access: map[string]githubapi.RepoAccess{
		"acme/site":  {Private: false, Admin: true},
		"acme/other": {Private: true, Admin: false},
	}}
	m, store := newTestManager(t, api, fakeRunner(t, idleScript))

	for repo, code := range map[string]string{
		"acme/site":    domain.GitHubRunnerErrPublicRepo,
		"acme/other":   domain.GitHubRunnerErrNeedsAdmin,
		"not a repo":   domain.GitHubRunnerErrInvalidRepo,
		"acme/missing": domain.GitHubRunnerErrGitHubRefused,
	} {
		_, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: true, Repos: []string{repo}})
		var repoErr *domain.GitHubRunnerRepoError
		if !errors.As(err, &repoErr) || repoErr.Code != code {
			t.Errorf("%s: err = %v, want code %s", repo, err, code)
		}
	}
	if store.cfg.Enabled || len(store.cfg.Repos) != 0 {
		t.Fatalf("a refused configuration was saved: %+v", store.cfg)
	}
	if issued, _ := api.snapshot(); issued != 0 {
		t.Fatal("a runner was registered for a refused repository")
	}
}

func TestAGitHubRefusalShowsOnTheRepositoryAndIsRetried(t *testing.T) {
	api := &fakeAPI{
		access: map[string]githubapi.RepoAccess{"acme/app": {Private: true, Admin: true}},
		jitErr: githubapi.ErrNotConnected,
	}
	m, _ := newTestManager(t, api, fakeRunner(t, idleScript))
	if _, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: true, Repos: []string{"acme/app"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the refusal on the repository", func() bool {
		r := repoStatus(t, m, "acme/app")
		return r.State == domain.GitHubRunnerFailed && r.ErrorCode == domain.GitHubRunnerErrNotConnected
	})
	api.mu.Lock()
	api.jitErr = nil
	api.mu.Unlock()
	waitFor(t, "a retry once GitHub answers", func() bool { return repoStatus(t, m, "acme/app").State == domain.GitHubRunnerIdle })
}

func TestStatusOffersTheProjectsRepositoriesAndASavedOne(t *testing.T) {
	api := &fakeAPI{access: map[string]githubapi.RepoAccess{"acme/api": {Private: true, Admin: true}}}
	m, _ := newTestManager(t, api, fakeRunner(t, idleScript))
	if _, err := m.Configure(context.Background(), domain.GitHubRunnerConfig{Enabled: false, Repos: []string{"acme/api"}}); err != nil {
		t.Fatal(err)
	}
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.Candidates, ","); got != "acme/api,acme/app,acme/site" {
		t.Fatalf("candidates = %q", got)
	}
	if !st.Supported || st.Version != Version || len(st.Labels) != 4 {
		t.Fatalf("status: %+v", st)
	}
}

func TestStartStopsARunnerAPreviousServerLeftGoing(t *testing.T) {
	api := &fakeAPI{access: map[string]githubapi.RepoAccess{}}
	m, _ := newTestManager(t, api, fakeRunner(t, idleScript))
	leftover := filepath.Join(m.cfg.Dir, "repos", "old")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "30")
	cmd.Dir = leftover
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	m.Start(context.Background())
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("a runner left in an install directory was not stopped")
	}
}
