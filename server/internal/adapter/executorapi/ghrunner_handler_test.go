package executorapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/ghrunner"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
)

type fakeRunnerHost struct {
	prepared, started []string
	err               error
}

func (f *fakeRunnerHost) Prepare(repo string) error {
	f.prepared = append(f.prepared, repo)
	return nil
}
func (f *fakeRunnerHost) Start(repo, jit string) error {
	f.started = append(f.started, repo+"="+jit)
	return f.err
}
func (f *fakeRunnerHost) Stop(string, bool) {}
func (f *fakeRunnerHost) Status() ghrunner.HostStatus {
	return ghrunner.HostStatus{Supported: true, Labels: []string{"self-hosted"}, Repos: []ghrunner.HostRepo{}}
}

func ghPost(t *testing.T, h http.Handler, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	raw, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(raw)
}

func TestGitHubRunnerRoutesDriveTheHostAndNeverEchoTheConfig(t *testing.T) {
	host := &fakeRunnerHost{}
	h := NewHandler(executor.NewService(executor.Deps{WorkspaceRoot: t.TempDir()}), Options{Token: testToken, GitHubRunner: host})

	if code, body := ghPost(t, h, PathGitHubRunnerPrepare, `{"repo":"acme/app"}`); code != http.StatusOK || !strings.Contains(body, `"supported":true`) {
		t.Fatalf("prepare: %d %s", code, body)
	}
	if code, body := ghPost(t, h, PathGitHubRunnerStart, `{"repo":"acme/app","jit_config":"TOP-SECRET-JIT"}`); code != http.StatusOK || strings.Contains(body, "TOP-SECRET-JIT") {
		t.Fatalf("start: %d %s", code, body)
	}
	if len(host.prepared) != 1 || len(host.started) != 1 || host.started[0] != "acme/app=TOP-SECRET-JIT" {
		t.Fatalf("the host was driven wrongly: %v %v", host.prepared, host.started)
	}
	if code, _ := ghPost(t, h, PathGitHubRunnerStatus, `{}`); code != http.StatusOK {
		t.Fatalf("status: %d", code)
	}
	if code, _ := ghPost(t, h, PathGitHubRunnerStop, `{"repo":"acme/app","remove":true}`); code != http.StatusOK {
		t.Fatalf("stop: %d", code)
	}
}

func TestGitHubRunnerRoutesRefuseBadRepositoriesAndRedactARefusedConfig(t *testing.T) {
	host := &fakeRunnerHost{err: ghrunner.ErrHostBusy}
	h := NewHandler(executor.NewService(executor.Deps{WorkspaceRoot: t.TempDir()}), Options{Token: testToken, GitHubRunner: host})

	for _, repo := range []string{"", "acme", "../x/y", "a/b/c", "a b/c"} {
		if code, _ := ghPost(t, h, PathGitHubRunnerPrepare, `{"repo":"`+repo+`"}`); code != http.StatusBadRequest {
			t.Errorf("repo %q: status %d, want 400", repo, code)
		}
	}
	code, body := ghPost(t, h, PathGitHubRunnerStart, `{"repo":"acme/app","jit_config":"TOP-SECRET-JIT"}`)
	if code != http.StatusConflict || strings.Contains(body, "TOP-SECRET-JIT") {
		t.Fatalf("a second start: %d %s, want 409 and no config", code, body)
	}
}

func TestGitHubRunnerRoutesAreNotReadyWithoutAHost(t *testing.T) {
	h := NewHandler(executor.NewService(executor.Deps{WorkspaceRoot: t.TempDir()}), Options{Token: testToken})
	if code, body := ghPost(t, h, PathGitHubRunnerStatus, `{}`); code != http.StatusServiceUnavailable || !strings.Contains(body, "not_ready") {
		t.Fatalf("%d %s, want 503 not_ready", code, body)
	}
}
