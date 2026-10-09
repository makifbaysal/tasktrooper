package github

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// IssuesAPI is the GitHub issues slice of the REST API: search, list, read,
// comment, close. Separate from PRAPI because the issue-import feature is its
// only caller and pulls in port.GitHubIssuesClient's domain.ExternalIssue
// shape rather than PRAPI's port.PullRequest one.
type IssuesAPI struct {
	baseURL string
}

func NewIssuesAPI() *IssuesAPI { return &IssuesAPI{} }

func (a *IssuesAPI) SetBaseURL(u string) { a.baseURL = u }

type issuePayload struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url"`
	Labels  []struct {
		Name string `json:"name"`
	} `json:"labels"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest *map[string]any `json:"pull_request"`
}

func (p issuePayload) isPullRequest() bool {
	return p.PullRequest != nil
}

func (p issuePayload) toDomain(owner, repo string) domain.ExternalIssue {
	labels := make([]string, 0, len(p.Labels))
	for _, l := range p.Labels {
		if l.Name != "" {
			labels = append(labels, l.Name)
		}
	}
	return domain.ExternalIssue{
		Provider:  domain.IssueProviderGitHub,
		Key:       owner + "/" + repo + "#" + strconv.Itoa(p.Number),
		Title:     p.Title,
		Body:      p.Body,
		URL:       p.HTMLURL,
		State:     p.State,
		Labels:    labels,
		UpdatedAt: p.UpdatedAt,
	}
}

func issuesPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues"
}

func (a *IssuesAPI) ListOpen(ctx context.Context, token, owner, repo string, perPage int) ([]domain.ExternalIssue, error) {
	if perPage <= 0 || perPage > 100 {
		perPage = 50
	}
	q := url.Values{}
	q.Set("state", "open")
	q.Set("sort", "created")
	q.Set("direction", "desc")
	q.Set("per_page", strconv.Itoa(perPage))
	var out []issuePayload
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, issuesPath(owner, repo)+"?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return filterIssues(out, owner, repo), nil
}

func (a *IssuesAPI) ListLabelled(ctx context.Context, token, owner, repo, label string, perPage int) ([]domain.ExternalIssue, error) {
	if perPage <= 0 || perPage > 100 {
		perPage = 50
	}
	q := url.Values{}
	q.Set("state", "open")
	q.Set("sort", "created")
	q.Set("direction", "desc")
	q.Set("per_page", strconv.Itoa(perPage))
	if label != "" {
		q.Set("labels", label)
	}
	var out []issuePayload
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, issuesPath(owner, repo)+"?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return filterIssues(out, owner, repo), nil
}

// Search runs GET /search/issues?q=repo:o/r is:issue is:open <q>. q may be
// empty, which is still a valid (and useful) search — "every open issue".
func (a *IssuesAPI) Search(ctx context.Context, token, owner, repo, q string) ([]domain.ExternalIssue, error) {
	query := "repo:" + owner + "/" + repo + " is:issue is:open"
	if strings.TrimSpace(q) != "" {
		query += " " + q
	}
	vals := url.Values{}
	vals.Set("q", query)
	vals.Set("per_page", "50")
	var out struct {
		Items []issuePayload `json:"items"`
	}
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, "/search/issues?"+vals.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return filterIssues(out.Items, owner, repo), nil
}

func (a *IssuesAPI) Get(ctx context.Context, token, owner, repo string, number int) (domain.ExternalIssue, error) {
	var out issuePayload
	path := issuesPath(owner, repo) + "/" + strconv.Itoa(number)
	if err := doJSONAt(ctx, a.baseURL, token, http.MethodGet, path, nil, &out); err != nil {
		return domain.ExternalIssue{}, err
	}
	return out.toDomain(owner, repo), nil
}

func (a *IssuesAPI) AddComment(ctx context.Context, token, owner, repo string, number int, body string) error {
	path := issuesPath(owner, repo) + "/" + strconv.Itoa(number) + "/comments"
	return doJSONAt(ctx, a.baseURL, token, http.MethodPost, path, map[string]any{"body": body}, nil)
}

func (a *IssuesAPI) Close(ctx context.Context, token, owner, repo string, number int) error {
	path := issuesPath(owner, repo) + "/" + strconv.Itoa(number)
	return doJSONAt(ctx, a.baseURL, token, http.MethodPatch, path, map[string]any{
		"state":        "closed",
		"state_reason": "completed",
	}, nil)
}

func filterIssues(in []issuePayload, owner, repo string) []domain.ExternalIssue {
	out := make([]domain.ExternalIssue, 0, len(in))
	for _, p := range in {
		if p.isPullRequest() {
			continue
		}
		out = append(out, p.toDomain(owner, repo))
	}
	return out
}
