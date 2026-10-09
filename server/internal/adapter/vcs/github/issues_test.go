package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func issuesTestServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *IssuesAPI {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q, want the bearer token", got)
			}
			h(w, r)
			return
		}
		t.Errorf("unexpected call %s %s%s", r.Method, r.URL.Path, "?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	api := NewIssuesAPI()
	api.SetBaseURL(srv.URL)
	return api
}

func TestListOpenFiltersPullRequests(t *testing.T) {
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/issues": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("state"); got != "open" {
				t.Errorf("state = %q", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 1, "title": "Real issue", "html_url": "https://github.com/acme/widget/issues/1",
					"state": "open", "labels": []map[string]any{{"name": "bug"}}, "updated_at": "2024-01-02T03:04:05Z"},
				{"number": 2, "title": "A PR", "html_url": "https://github.com/acme/widget/pull/2",
					"state": "open", "pull_request": map[string]any{"url": "x"}, "updated_at": "2024-01-02T03:04:05Z"},
			})
		},
	})

	issues, err := api.ListOpen(context.Background(), "tok", "acme", "widget", 50)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (the PR must be filtered)", len(issues))
	}
	if issues[0].Key != "acme/widget#1" {
		t.Errorf("key = %q", issues[0].Key)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "bug" {
		t.Errorf("labels = %v", issues[0].Labels)
	}
}

func TestListLabelledSendsTheLabelFilter(t *testing.T) {
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/issues": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("labels"); got != "tasktrooper" {
				t.Errorf("labels query = %q", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 5, "title": "Labelled", "html_url": "u", "state": "open", "updated_at": "2024-01-02T03:04:05Z"},
			})
		},
	})
	issues, err := api.ListLabelled(context.Background(), "tok", "acme", "widget", "tasktrooper", 0)
	if err != nil {
		t.Fatalf("ListLabelled: %v", err)
	}
	if len(issues) != 1 || issues[0].Key != "acme/widget#5" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestSearchBuildsTheQualifiedQuery(t *testing.T) {
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /search/issues": func(w http.ResponseWriter, r *http.Request) {
			want := "repo:acme/widget is:issue is:open crash"
			if got := r.URL.Query().Get("q"); got != want {
				t.Errorf("q = %q, want %q", got, want)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"number": 9, "title": "Crash on load", "html_url": "u", "state": "open", "updated_at": "2024-01-02T03:04:05Z"},
				},
			})
		},
	})
	issues, err := api.Search(context.Background(), "tok", "acme", "widget", "crash")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(issues) != 1 || issues[0].Title != "Crash on load" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestSearchWithEmptyQueryOmitsTheExtraTerm(t *testing.T) {
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /search/issues": func(w http.ResponseWriter, r *http.Request) {
			want := "repo:acme/widget is:issue is:open"
			if got := r.URL.Query().Get("q"); got != want {
				t.Errorf("q = %q, want %q", got, want)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
		},
	})
	if _, err := api.Search(context.Background(), "tok", "acme", "widget", ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
}

func TestGetIssue(t *testing.T) {
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /repos/acme/widget/issues/7": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "T", "body": "B", "html_url": "u", "state": "open",
				"updated_at": "2024-01-02T03:04:05Z",
			})
		},
	})
	issue, err := api.Get(context.Background(), "tok", "acme", "widget", 7)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Key != "acme/widget#7" || issue.Body != "B" {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestCloseIssueSendsStateAndReason(t *testing.T) {
	var gotBody map[string]any
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"PATCH /repos/acme/widget/issues/3": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		},
	})
	if err := api.Close(context.Background(), "tok", "acme", "widget", 3); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if gotBody["state"] != "closed" || gotBody["state_reason"] != "completed" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestAddCommentPostsBody(t *testing.T) {
	var gotBody map[string]any
	api := issuesTestServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /repos/acme/widget/issues/3/comments": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		},
	})
	if err := api.AddComment(context.Background(), "tok", "acme", "widget", 3, "hello"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if gotBody["body"] != "hello" {
		t.Fatalf("body = %+v", gotBody)
	}
}
