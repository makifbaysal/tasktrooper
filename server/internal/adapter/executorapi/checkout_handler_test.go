package executorapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	gitadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/vcs/git"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const checkoutWorkspace = "repos/app/task-7"

func (s *HandlerSuite) git(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	s.Require().NoError(err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// withCheckout serves a workspace that is a clone of a bare origin with one
// commit on main, and returns the checkout and the origin.
func (s *HandlerSuite) withCheckout() (string, string) {
	if runtime.GOOS == "windows" {
		s.T().Skip("the fixtures are sh scripts")
	}
	root := s.T().TempDir()
	origin := filepath.Join(s.T().TempDir(), "origin.git")
	s.git(filepath.Dir(origin), "init", "--bare", "--initial-branch=main", origin)
	seed := filepath.Join(s.T().TempDir(), "seed")
	s.git(filepath.Dir(seed), "clone", origin, seed)
	s.Require().NoError(os.WriteFile(filepath.Join(seed, "main.go"), []byte("package main\n"), 0o644))
	s.git(seed, "add", ".")
	s.git(seed, "commit", "-m", "first")
	s.git(seed, "push", "origin", "main")
	dir := filepath.Join(root, checkoutWorkspace)
	s.Require().NoError(os.MkdirAll(filepath.Dir(dir), 0o755))
	s.git(root, "clone", origin, dir)

	s.server.Close()
	s.svc = executor.NewService(executor.Deps{
		LLM:           s.llm,
		Providers:     map[string]executor.Provider{"main": {Type: domain.LLMProviderOpenAI, DefaultModel: "gpt-4o"}},
		WorkspaceRoot: root,
		Git:           gitadapter.NewCheckout(),
		Limits:        executor.Limits{MaxIterations: 5, TaskMaxIterations: 5, History: appcontext.Budget{KeepRecentMessages: 10}},
	})
	s.server = httptest.NewServer(NewHandler(s.svc, Options{Token: testToken, Version: "test", Secrets: []string{testKey}}))
	return dir, origin
}

func (s *HandlerSuite) TestVerifyStreamsStagesAndOutputThenTheVerdict() {
	s.withCheckout()

	frames := s.frames(s.request(context.Background(), http.MethodPost, PathVerify, testToken, map[string]any{
		"id":        "c-verify-1",
		"workspace": checkoutWorkspace,
		"commands":  []map[string]any{{"argv": []string{"sh", "-c", "echo running " + testKey + "; echo 'FAIL: TestLogin'; exit 1"}}},
	}))

	s.Require().GreaterOrEqual(len(frames), 4)
	s.Equal("started", frames[0].Event)
	for _, f := range frames {
		s.Equal("c-verify-1", f.ID)
	}
	var kinds []string
	var output strings.Builder
	for _, f := range frames[1 : len(frames)-1] {
		s.Equal("event", f.Event)
		kind, _ := f.Payload["kind"].(string)
		kinds = append(kinds, kind)
		if kind == executor.EventVerifyOutput {
			data, _ := f.Payload["data"].(string)
			output.WriteString(data)
		}
	}
	s.Equal(executor.EventVerifyStage, kinds[0])
	s.Equal(executor.EventVerifyStage, kinds[len(kinds)-1])
	s.Contains(kinds, executor.EventVerifyOutput)
	s.Contains(output.String(), "FAIL: TestLogin")
	s.NotContains(output.String(), testKey, "a held secret is scrubbed from the streamed output")

	done := frames[len(frames)-1]
	s.Equal("done", done.Event)
	s.Require().NotNil(done.OK)
	s.True(*done.OK, "a failing build is a verdict, not a failed call")
	s.Equal(false, done.Result["passed"])
	s.Contains(done.Result["report"], "FAIL: TestLogin")
	stages, _ := done.Result["stages"].([]any)
	s.Len(stages, 1)
}

func (s *HandlerSuite) TestVerifyCancelledByItsStreamID() {
	s.withCheckout()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := s.request(ctx, http.MethodPost, PathVerify, testToken, map[string]any{
		"id": "c-verify-2", "workspace": checkoutWorkspace,
		"commands": []map[string]any{{"argv": []string{"sh", "-c", "sleep 30"}}},
	})
	cancelResp := s.request(context.Background(), http.MethodPost, PathCancel, testToken, map[string]any{"id": "c-verify-2"})
	var cancelled cancelResponse
	s.Require().NoError(json.NewDecoder(cancelResp.Body).Decode(&cancelled))
	cancelResp.Body.Close()
	s.True(cancelled.Cancelled)

	frames := s.frames(resp)
	done := frames[len(frames)-1]
	s.Require().NotNil(done.OK)
	s.False(*done.OK)
	s.Equal(executor.CodeCancelled, done.Error.Code)
}

func (s *HandlerSuite) TestGitDiffAndStatusAnswerForTheCheckout() {
	dir, _ := s.withCheckout()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))

	resp := s.request(context.Background(), http.MethodPost, PathGitDiff, testToken, map[string]any{"workspace": checkoutWorkspace})
	s.Equal(http.StatusOK, resp.StatusCode)
	var diff struct {
		Base  string   `json:"base"`
		Files []string `json:"files"`
		Stat  string   `json:"stat"`
		Patch string   `json:"patch"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&diff))
	resp.Body.Close()
	s.Len(diff.Base, 40)
	s.Equal([]string{"main.go"}, diff.Files)
	s.Contains(diff.Patch, "+func main() {}")

	resp = s.request(context.Background(), http.MethodPost, PathGitStatus, testToken, map[string]any{"workspace": checkoutWorkspace})
	s.Equal(http.StatusOK, resp.StatusCode)
	var status struct {
		Branch string `json:"branch"`
		Clean  bool   `json:"clean"`
		Files  []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&status))
	resp.Body.Close()
	s.Equal("main", status.Branch)
	s.False(status.Clean)
	s.Require().Len(status.Files, 1)
	s.Equal("main.go", status.Files[0].Path)

	resp = s.request(context.Background(), http.MethodPost, PathGitDiff, testToken, map[string]any{"workspace": "../escape"})
	s.Equal(http.StatusBadRequest, resp.StatusCode)
	s.Equal(executor.CodeBadRequest, s.errorOf(resp).Code)
}

// TestCommitPushKeepsTheTokenOffEveryArgv runs the real git behind a wrapper
// that records what each git child was started with.
func (s *HandlerSuite) TestCommitPushKeepsTheTokenOffEveryArgv() {
	dir, origin := s.withCheckout()
	realGit, err := exec.LookPath("git")
	s.Require().NoError(err)
	bin := s.T().TempDir()
	argvLog := filepath.Join(s.T().TempDir(), "argv.log")
	wrapper := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TT_GIT_ARGV_LOG\"\n" +
		"if [ -n \"$GIT_CONFIG_KEY_0\" ]; then printf 'env:%s\\n' \"$GIT_CONFIG_KEY_0\" >> \"$TT_GIT_ARGV_LOG\"; fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	s.Require().NoError(os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o755))
	s.T().Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	s.T().Setenv("TT_GIT_ARGV_LOG", argvLog)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package main\n"), 0o644))

	const token = "ghs_installation-token-0123456789"
	resp := s.request(context.Background(), http.MethodPost, PathCommitPush, testToken, map[string]any{
		"workspace": checkoutWorkspace, "message": "feat: the feature", "branch": "feature/tt-7", "github_token": token,
	})
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	var result struct {
		Branch        string   `json:"branch"`
		BranchCreated bool     `json:"branch_created"`
		Head          string   `json:"head"`
		Committed     bool     `json:"committed"`
		Files         []string `json:"files"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&result))
	resp.Body.Close()
	s.Equal("feature/tt-7", result.Branch)
	s.True(result.BranchCreated)
	s.True(result.Committed)
	s.Equal([]string{"feature.go"}, result.Files)
	s.Equal(result.Head, s.git(origin, "rev-parse", "refs/heads/feature/tt-7"))

	logged, err := os.ReadFile(argvLog)
	s.Require().NoError(err)
	s.Contains(string(logged), "push -u origin HEAD")
	s.Contains(string(logged), "env:http.https://github.com/.extraheader", "the token rides the push's environment")
	s.NotContains(string(logged), token)
	s.NotContains(string(logged), "x-access-token")
}

func (s *HandlerSuite) TestCommitPushRefusesTheDefaultBranch() {
	s.withCheckout()
	resp := s.request(context.Background(), http.MethodPost, PathCommitPush, testToken, map[string]any{
		"workspace": checkoutWorkspace, "message": "m", "branch": "main",
	})
	s.Equal(http.StatusConflict, resp.StatusCode)
	s.Equal(executor.CodeConflict, s.errorOf(resp).Code)
}
