package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTagPostsGitRef(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ref":"refs/tags/rollback/prod/1"}`))
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	if err := api.CreateTag(context.Background(), "o", "r", "rollback/prod/1", "abc123"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if gotPath != "/repos/o/r/git/refs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["ref"] != "refs/tags/rollback/prod/1" || gotBody["sha"] != "abc123" {
		t.Fatalf("body = %v", gotBody)
	}
}

// An empty branch must list runs across every branch — the monitor has no
// single branch to filter by.
func TestListWorkflowRunsOmitsBranchWhenEmpty(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":7,"run_number":3,"head_sha":"s","head_branch":"main","status":"completed","conclusion":"success","html_url":"u","event":"workflow_dispatch"}]}`))
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	runs, err := api.ListWorkflowRuns(context.Background(), "o", "r", "prod.yml", "")
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if strings.Contains(gotQuery, "branch=") {
		t.Fatalf("query carried a branch filter: %q", gotQuery)
	}
	if len(runs) != 1 || runs[0].ID != 7 || runs[0].Event != "workflow_dispatch" {
		t.Fatalf("runs = %+v", runs)
	}
}

// A red codecov/ci context must never read as a failed deploy: only statuses
// that look like a deploy provider (or a context literally saying "deploy")
// count, and GitHub's own combined state was computed over all of them.
func TestCommitDeployStatusIgnoresCIContextsAndKeepsDeployOnesOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"state": "failure",
			"total_count": 3,
			"statuses": [
				{"context": "codecov/project", "state": "failure"},
				{"context": "ci/lint", "state": "success"},
				{"context": "vercel", "state": "success", "description": "Deployed", "target_url": "https://vercel.example/x"}
			]
		}`))
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	signal, err := api.CommitDeployStatus(context.Background(), "o", "r", "sha")
	if err != nil {
		t.Fatalf("CommitDeployStatus: %v", err)
	}
	if signal.Kind == "" || signal.State != "success" {
		t.Fatalf("signal = %+v, want a commit_status success — the only deploy-provider context (vercel) succeeded, the combined state's failure came from codecov", signal)
	}
	if len(signal.Contexts) != 1 || signal.Contexts[0] != "vercel" {
		t.Fatalf("contexts = %v, want only the vercel context, codecov/ci filtered out", signal.Contexts)
	}
}

// When NOTHING looks like a deploy provider, CommitDeployStatus must fall
// through to the deployments API rather than reporting a CI-only combined
// status as a deploy signal.
func TestCommitDeployStatusFallsThroughToDeploymentsWhenOnlyCIContextsExist(t *testing.T) {
	var sawDeploymentsCall bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{
				"state": "failure",
				"total_count": 2,
				"statuses": [
					{"context": "codecov/project", "state": "failure"},
					{"context": "build", "state": "success"}
				]
			}`))
		case strings.Contains(r.URL.Path, "/deployments"):
			sawDeploymentsCall = true
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	api := NewActionsAPI("tok")
	api.SetBaseURL(srv.URL)
	signal, err := api.CommitDeployStatus(context.Background(), "o", "r", "sha")
	if err != nil {
		t.Fatalf("CommitDeployStatus: %v", err)
	}
	if signal.Kind != "" {
		t.Fatalf("kind = %q, want empty — nothing here is a deploy signal", signal.Kind)
	}
	if !sawDeploymentsCall {
		t.Fatal("CI-only statuses must fall through to the deployments API, not be treated as the answer")
	}
}

func TestLooksLikeDeployContextRecognisesProvidersNotCI(t *testing.T) {
	cases := map[string]bool{
		"vercel":                true,
		"netlify":               true,
		"my-deploy-hook":        true,
		"deployment/production": true,
		"codecov/project":       false,
		"ci/lint":               false,
		"build":                 false,
		"test":                  false,
	}
	for ctx, want := range cases {
		if got := looksLikeDeployContext(ctx); got != want {
			t.Errorf("looksLikeDeployContext(%q) = %v, want %v", ctx, got, want)
		}
	}
}
