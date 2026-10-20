package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func runnersAPIAt(srv *httptest.Server, token string) *RunnersAPI {
	api := NewRunnersAPI(func(context.Context) (string, error) { return token, nil })
	api.SetBaseURL(srv.URL)
	return api
}

func TestGenerateJITConfigAsksForARepoRunnerWithTheGivenLabels(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"runner":{"id":42,"name":"tasktrooper-mac-abc"},"encoded_jit_config":"ZW5jb2RlZA=="}`))
	}))
	defer srv.Close()

	jit, err := runnersAPIAt(srv, "tok").GenerateJITConfig(context.Background(), "acme", "app", "tasktrooper-mac-abc", []string{"self-hosted", "macOS"}, "_work")
	if err != nil {
		t.Fatal(err)
	}
	if jit.ID != 42 || jit.EncodedConfig != "ZW5jb2RlZA==" {
		t.Fatalf("jit = %+v", jit)
	}
	if gotPath != "/repos/acme/app/actions/runners/generate-jitconfig" || gotAuth != "Bearer tok" {
		t.Fatalf("path %q auth %q", gotPath, gotAuth)
	}
	if gotBody["name"] != "tasktrooper-mac-abc" || gotBody["runner_group_id"] != float64(1) || gotBody["work_folder"] != "_work" {
		t.Fatalf("body = %v", gotBody)
	}
	if labels, _ := gotBody["labels"].([]any); len(labels) != 2 || labels[0] != "self-hosted" {
		t.Fatalf("labels = %v", gotBody["labels"])
	}
}

func TestRepoAccessReadsVisibilityAndAdminRights(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"private":true,"permissions":{"admin":true,"push":true}}`))
	}))
	defer srv.Close()
	access, err := runnersAPIAt(srv, "tok").RepoAccess(context.Background(), "acme", "app")
	if err != nil || !access.Private || !access.Admin {
		t.Fatalf("access = %+v, err = %v", access, err)
	}
}

func TestDeleteRunnerTreatsAGoneRunnerAsDone(t *testing.T) {
	status := http.StatusNotFound
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	api := runnersAPIAt(srv, "tok")
	if err := api.DeleteRunner(context.Background(), "acme", "app", 7); err != nil {
		t.Fatalf("404: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repos/acme/app/actions/runners/7" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	status = http.StatusForbidden
	err := api.DeleteRunner(context.Background(), "acme", "app", 7)
	if HTTPStatus(err) != http.StatusForbidden || APIMessage(err) != "Not Found" {
		t.Fatalf("403: %v", err)
	}
}

func TestRunnersAPIWithoutAConnectionSaysSo(t *testing.T) {
	api := NewRunnersAPI(func(context.Context) (string, error) { return "", nil })
	if _, err := api.RepoAccess(context.Background(), "acme", "app"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
	var nilAPI *RunnersAPI
	if _, err := nilAPI.GenerateJITConfig(context.Background(), "a", "b", "n", nil, "_work"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("nil api: %v", err)
	}
}

func TestEligibleReposKeepsOnlyPrivateAdministeredRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" || r.URL.Query().Get("visibility") != "private" {
			t.Errorf("unexpected request %s", r.URL)
		}
		_, _ = w.Write([]byte(`[
			{"full_name":"me/mine","private":true,"permissions":{"admin":true}},
			{"full_name":"acme/api","private":true,"permissions":{"admin":true}},
			{"full_name":"acme/readonly","private":true,"permissions":{"admin":false}},
			{"full_name":"me/open","private":false,"permissions":{"admin":true}},
			{"full_name":"acme/old","private":true,"archived":true,"permissions":{"admin":true}}
		]`))
	}))
	defer srv.Close()

	got, err := runnersAPIAt(srv, "tok").EligibleRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "me/mine" || got[1] != "acme/api" {
		t.Fatalf("eligible = %v", got)
	}
}
