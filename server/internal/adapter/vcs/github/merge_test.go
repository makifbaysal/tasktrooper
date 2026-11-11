package github

// The wire contract of the four calls that open and land a task's pull request.
// They are asserted against an httptest server rather than mocked, because what
// matters here IS the request: the method, the path, and the two fields
// (`draft`, `sha`) whose absence is invisible until a PR turns out to be
// unmergeable or a merge lands a commit nobody reviewed.
//
// apiBase is a package-level var for exactly this (see secrets_test.go).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withAPIBase(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	original := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = original })
}

// Task PRs open READY FOR REVIEW. They used to open as drafts, and nothing ever
// took them out of draft — GitHub refuses to merge a draft, so every task PR
// was unmergeable by construction.
func TestCreatePullRequestOpensItReadyForReview(t *testing.T) {
	var body map[string]any
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/widget/pulls" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/acme/widget/pull/42"})
	})

	url, err := CreatePullRequest(context.Background(), "tok", "acme", "widget", "feature/t-7", "main", "T-7: add the store link")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/acme/widget/pull/42" {
		t.Errorf("url = %q", url)
	}
	if draft, ok := body["draft"].(bool); !ok || draft {
		t.Errorf("draft = %v, want an explicit false", body["draft"])
	}
}

// The merge is a squash and carries the head-SHA precondition. Without the
// precondition GitHub merges whatever the branch points at when the request
// lands, which is how a push made after sign-off gets merged unseen.
func TestMergePullRequestSquashesWithTheExpectedHeadSHA(t *testing.T) {
	var body map[string]any
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/repos/acme/widget/pulls/42/merge" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "deadbeef", "merged": true})
	})

	out, err := MergePullRequest(context.Background(), "tok", "acme", "widget", 42, "1111111", "T-7: add the store link (#42)", "Task: T-7")
	if err != nil {
		t.Fatal(err)
	}
	if out.SHA != "deadbeef" || !out.Merged {
		t.Errorf("result = %+v", out)
	}
	if body["merge_method"] != "squash" {
		t.Errorf("merge_method = %v, want squash", body["merge_method"])
	}
	if body["sha"] != "1111111" {
		t.Errorf("sha = %v, want the verified head", body["sha"])
	}
	if body["commit_title"] != "T-7: add the store link (#42)" {
		t.Errorf("commit_title = %v", body["commit_title"])
	}
}

// No SHA, no merge — enforced here rather than only at the caller, because this
// is the last place before the irreversible request goes out.
func TestMergePullRequestRefusesWithoutTheExpectedHeadSHA(t *testing.T) {
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing may reach GitHub: %s %s", r.Method, r.URL.Path)
	})

	if _, err := MergePullRequest(context.Background(), "tok", "acme", "widget", 42, "  ", "", ""); err == nil {
		t.Fatal("a merge without the verified head commit must be refused")
	}
}

// GitHub's two meaningful refusals are named, because the agent reading the
// tool result decides what to do next from that sentence: 409 means the head
// moved, 405 means the PR is not in a mergeable state.
func TestMergePullRequestNamesGitHubsRefusals(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusConflict, "no longer at"},
		{http.StatusMethodNotAllowed, "not in a mergeable state"},
	} {
		withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "nope"})
		})

		_, err := MergePullRequest(context.Background(), "tok", "acme", "widget", 42, "1111111", "", "")
		if err == nil {
			t.Fatalf("status %d produced no error", tc.status)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d error = %q, want it to mention %q", tc.status, err, tc.want)
		}
	}
}

// Un-drafting goes through GraphQL. REST's "Update a pull request" has no
// `draft` field at all (only title/body/state/base/maintainer_can_modify), so a
// PATCH would report success and change nothing — and the merge behind it would
// fail with "Draft pull requests cannot be merged" for no visible reason.
func TestMarkPullRequestReadyUsesTheGraphQLMutation(t *testing.T) {
	var query struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	graphqlCalled := false
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/pulls/42":
			_ = json.NewEncoder(w).Encode(map[string]any{"node_id": "PR_node_42", "draft": true})
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			graphqlCalled = true
			_ = json.NewDecoder(r.Body).Decode(&query)
			_, _ = w.Write([]byte(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"number":42,"isDraft":false}}}}`))
		case r.Method == http.MethodPatch:
			t.Errorf("REST PATCH cannot un-draft a pull request and must not be attempted")
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})

	if err := MarkPullRequestReady(context.Background(), "tok", "acme", "widget", 42); err != nil {
		t.Fatal(err)
	}
	if !graphqlCalled {
		t.Fatal("the GraphQL mutation was never called")
	}
	if !strings.Contains(query.Query, "markPullRequestReadyForReview") {
		t.Errorf("query = %q", query.Query)
	}
	if query.Variables["id"] != "PR_node_42" {
		t.Errorf("mutation id = %v, want the PR's node id", query.Variables["id"])
	}
}

// A PR that is already ready costs one read and nothing else.
func TestMarkPullRequestReadyIsANoopForAReadyPR(t *testing.T) {
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			t.Error("a ready PR must not be un-drafted")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": "PR_node_42", "draft": false})
	})

	if err := MarkPullRequestReady(context.Background(), "tok", "acme", "widget", 42); err != nil {
		t.Fatal(err)
	}
}

// GraphQL answers 200 with an `errors` array for a failed mutation, so the
// transport-level check is not enough: a silently-still-draft PR would hit the
// merge as an unexplained failure.
func TestMarkPullRequestReadySurfacesGraphQLErrors(t *testing.T) {
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			_, _ = w.Write([]byte(`{"errors":[{"message":"Resource not accessible by integration"}]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": "PR_node_42", "draft": true})
	})

	err := MarkPullRequestReady(context.Background(), "tok", "acme", "widget", 42)
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("err = %v, want the GraphQL message", err)
	}
}

// The branch delete addresses the ref namespace, with every segment escaped
// individually: escaping the whole name would turn a `feature/x` branch's slash
// into %2F and address a ref that does not exist.
func TestDeleteBranchEscapesSegmentsWithoutBreakingTheRefPath(t *testing.T) {
	var gotPath string
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := DeleteBranch(context.Background(), "tok", "acme", "widget", "feature/t-7"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/acme/widget/git/refs/heads/feature/t-7" {
		t.Errorf("path = %q", gotPath)
	}
}

// An owner or repo carrying path syntax must not be able to steer the request
// out of the repository it names.
func TestDeleteBranchEscapesTheRepositoryCoordinates(t *testing.T) {
	var gotPath string
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	})

	if err := DeleteBranch(context.Background(), "tok", "acme/../evil", "widget", "main"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotPath, "/../") {
		t.Errorf("path = %q, want the owner escaped", gotPath)
	}
}

func prServer(t *testing.T, open []map[string]any, listStatus int, creates *int) {
	withAPIBase(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if listStatus != 0 {
				w.WriteHeader(listStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(open)
		case http.MethodPost:
			*creates++
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/acme/widget/pull/99"})
		}
	})
}

// A question answered mid-review re-enters the PR step; the branch already has
// a PR, so it must be reused rather than a second one opened.
func TestEnsureOpenPullRequestReusesTheOpenPRForTheBranch(t *testing.T) {
	creates := 0
	prServer(t, []map[string]any{{"html_url": "https://github.com/acme/widget/pull/42"}}, 0, &creates)

	url, err := EnsureOpenPullRequest(context.Background(), "tok", "acme", "widget", "acme", "feature/t-7", "main", "T-7")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/acme/widget/pull/42" || creates != 0 {
		t.Errorf("url = %q creates = %d, want the existing PR and no create", url, creates)
	}
}

func TestEnsureOpenPullRequestOpensOneWhenNoneExists(t *testing.T) {
	creates := 0
	prServer(t, []map[string]any{}, 0, &creates)

	url, err := EnsureOpenPullRequest(context.Background(), "tok", "acme", "widget", "acme", "feature/t-7", "main", "T-7")
	if err != nil || url != "https://github.com/acme/widget/pull/99" || creates != 1 {
		t.Errorf("url = %q err = %v creates = %d", url, err, creates)
	}
}

// A failed lookup used to read as "no PR yet" and opened a duplicate.
func TestEnsureOpenPullRequestDoesNotCreateWhenTheLookupFails(t *testing.T) {
	creates := 0
	prServer(t, nil, http.StatusBadGateway, &creates)

	if _, err := EnsureOpenPullRequest(context.Background(), "tok", "acme", "widget", "acme", "feature/t-7", "main", "T-7"); err == nil {
		t.Fatal("want an error when the open-PR lookup fails")
	}
	if creates != 0 {
		t.Errorf("creates = %d, want 0", creates)
	}
}
