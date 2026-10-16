package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeGitHubRunner struct {
	got domain.GitHubRunnerConfig
	err error
}

func (f *fakeGitHubRunner) Status(context.Context) (domain.GitHubRunnerStatus, error) {
	return domain.GitHubRunnerStatus{Supported: true, Enabled: f.got.Enabled}, nil
}

func (f *fakeGitHubRunner) Configure(_ context.Context, cfg domain.GitHubRunnerConfig) (domain.GitHubRunnerStatus, error) {
	if f.err != nil {
		return domain.GitHubRunnerStatus{}, f.err
	}
	f.got = cfg
	return domain.GitHubRunnerStatus{Supported: true, Enabled: cfg.Enabled}, nil
}

func putRunner(t *testing.T, h *Handler, body string) (int, map[string]any) {
	t.Helper()
	app := fiber.New()
	app.Put("/v1/settings/github/runner", h.SetGitHubRunner)
	req := httptest.NewRequest("PUT", "/v1/settings/github/runner", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSetGitHubRunnerPassesTheSwitchAndRepositories(t *testing.T) {
	runner := &fakeGitHubRunner{}
	status, out := putRunner(t, &Handler{githubRunner: runner}, `{"enabled":true,"repos":["acme/app"]}`)
	if status != 200 || out["enabled"] != true {
		t.Fatalf("%d %v", status, out)
	}
	if !runner.got.Enabled || len(runner.got.Repos) != 1 || runner.got.Repos[0] != "acme/app" {
		t.Fatalf("configured %+v", runner.got)
	}
}

func TestSetGitHubRunnerNamesTheRefusalInTheErrorType(t *testing.T) {
	runner := &fakeGitHubRunner{err: &domain.GitHubRunnerRepoError{Repo: "acme/site", Code: domain.GitHubRunnerErrPublicRepo}}
	status, out := putRunner(t, &Handler{githubRunner: runner}, `{"enabled":true,"repos":["acme/site"]}`)
	errObj, _ := out["error"].(map[string]any)
	if status != 400 || errObj["type"] != "github_runner_public_repo" {
		t.Fatalf("%d %v", status, out)
	}
}

func TestSetGitHubRunnerWithoutARunnerIsUnsupported(t *testing.T) {
	status, out := putRunner(t, &Handler{}, `{"enabled":true,"repos":[]}`)
	errObj, _ := out["error"].(map[string]any)
	if status != 503 || errObj["type"] != "github_runner_unsupported_platform" {
		t.Fatalf("%d %v", status, out)
	}
}
