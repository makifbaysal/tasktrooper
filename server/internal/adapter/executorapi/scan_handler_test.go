package executorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const scanWorkspace = "repos/api/default"

func (s *HandlerSuite) withScanner(scanner port.RepoScanner) string {
	root := s.T().TempDir()
	s.server.Close()
	s.svc = executor.NewService(executor.Deps{
		LLM:           s.llm,
		Providers:     map[string]executor.Provider{"main": {Type: domain.LLMProviderOpenAI, DefaultModel: "gpt-4o"}},
		WorkspaceRoot: root,
		Scanner:       scanner,
		Limits:        executor.Limits{MaxIterations: 5, TaskMaxIterations: 5, History: appcontext.Budget{KeepRecentMessages: 10}},
	})
	s.server = httptest.NewServer(NewHandler(s.svc, Options{Token: testToken, Version: "test", Secrets: []string{testKey}}))
	return root
}

// withScannedRepository is a git checkout of a small Go service with a CI
// workflow, under the executor's workspace root.
func (s *HandlerSuite) withScannedRepository() {
	if runtime.GOOS == "windows" {
		s.T().Skip("the fixture needs a POSIX git setup")
	}
	root := s.withScanner(discovery.New())
	dir := filepath.Join(root, scanWorkspace)
	s.Require().NoError(os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755))
	files := map[string]string{
		"go.mod":  "module example.com/api\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {}\n",
		".github/workflows/ci.yml": "name: ci\non:\n  push:\n    branches: [main]\njobs:\n  test:\n    runs-on: ubuntu-latest\n" +
			"    steps:\n      - uses: actions/checkout@v4\n      - run: go test ./...\n",
	}
	for name, body := range files {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	s.git(dir, "init", "--initial-branch=main")
	s.git(dir, "add", ".")
	s.git(dir, "commit", "-m", "first")
}

func (s *HandlerSuite) TestScanStreamsEachStageThenTheScanResult() {
	s.withScannedRepository()

	frames := s.frames(s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{
		"id": "c-scan-1", "workspace": scanWorkspace,
	}))

	s.Require().GreaterOrEqual(len(frames), 3)
	s.Equal("started", frames[0].Event)
	var stages []string
	lastSeq := 0.0
	for _, f := range frames {
		s.Equal("c-scan-1", f.ID)
	}
	for _, f := range frames[1 : len(frames)-1] {
		s.Equal("event", f.Event)
		s.Equal(executor.EventScanStage, f.Payload["kind"])
		seq, _ := f.Payload["seq"].(float64)
		s.Greater(seq, lastSeq, "seq is the order on the wire")
		lastSeq = seq
		stage, _ := f.Payload["stage"].(string)
		if done, _ := f.Payload["done"].(bool); done {
			stages = append(stages, stage)
		}
	}
	s.Contains(stages, string(domain.ScanStageInventory))
	s.Contains(stages, string(domain.ScanStageComponents))
	s.Contains(stages, string(domain.ScanStageChecks))

	done := frames[len(frames)-1]
	s.Equal("done", done.Event)
	s.Require().NotNil(done.OK)
	s.True(*done.OK)
	raw, err := json.Marshal(done.Result)
	s.Require().NoError(err)
	var result domain.ScanResult
	s.Require().NoError(json.Unmarshal(raw, &result))
	s.Positive(result.FileCount)
	s.Require().NotEmpty(result.Components)
	s.Equal(".", result.Components[0].Path)
	s.NotEmpty(result.Checks, "the workflow's job is a detected check")
	s.NotEmpty(result.Git.HeadSHA, "git facts are read from the checkout")
	s.Equal("main", result.Git.DefaultBranch)
}

func (s *HandlerSuite) TestScanRefusesAWorkspaceOutsideTheRoot() {
	s.withScanner(discovery.New())

	resp := s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{"workspace": "../elsewhere"})
	s.Equal(http.StatusBadRequest, resp.StatusCode)
	s.Equal(executor.CodeBadRequest, s.errorOf(resp).Code)

	resp = s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{"workspace": "repos/missing/default"})
	s.Equal(http.StatusBadRequest, resp.StatusCode)
	s.Contains(s.errorOf(resp).Message, "does not exist")
}

func (s *HandlerSuite) TestScanWithoutAScannerIsNotReady() {
	root := s.withScanner(nil)
	s.Require().NoError(os.MkdirAll(filepath.Join(root, scanWorkspace), 0o755))

	resp := s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{"workspace": scanWorkspace})
	s.Equal(http.StatusServiceUnavailable, resp.StatusCode)
	s.Equal(executor.CodeNotReady, s.errorOf(resp).Code)
}

// waitingScanner holds a scan open until its context ends.
type waitingScanner struct {
	started chan struct{}
}

func (w *waitingScanner) Scan(ctx context.Context, _ string, emit func(domain.ScanEvent)) (domain.ScanResult, error) {
	emit(domain.ScanEvent{Stage: domain.ScanStageInventory})
	close(w.started)
	<-ctx.Done()
	return domain.ScanResult{}, ctx.Err()
}

func (s *HandlerSuite) TestScanCancelledByItsStreamID() {
	scanner := &waitingScanner{started: make(chan struct{})}
	root := s.withScanner(scanner)
	s.Require().NoError(os.MkdirAll(filepath.Join(root, scanWorkspace), 0o755))

	resp := s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{"id": "c-scan-2", "workspace": scanWorkspace})
	<-scanner.started

	conflict := s.request(context.Background(), http.MethodPost, PathScan, testToken, map[string]any{"id": "c-scan-3", "workspace": scanWorkspace})
	s.Equal(http.StatusConflict, conflict.StatusCode, "one scan of a checkout at a time")
	conflict.Body.Close()

	cancelResp := s.request(context.Background(), http.MethodPost, PathCancel, testToken, map[string]any{"id": "c-scan-2"})
	var cancelled cancelResponse
	s.Require().NoError(json.NewDecoder(cancelResp.Body).Decode(&cancelled))
	cancelResp.Body.Close()
	s.True(cancelled.Cancelled)

	frames := s.frames(resp)
	done := frames[len(frames)-1]
	s.Equal("done", done.Event)
	s.Require().NotNil(done.OK)
	s.False(*done.OK)
	s.Equal(executor.CodeCancelled, done.Error.Code)
}
