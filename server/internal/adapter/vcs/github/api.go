package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var apiBase = "https://api.github.com"

var httpClient = &http.Client{Timeout: 15 * time.Second}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("github api: %d %s", e.Status, e.Message)
}

func doJSON(ctx context.Context, token, method, path string, body any, out any) error {
	return doJSONAt(ctx, "", token, method, path, body, out)
}

func resolveBase(base string) string {
	if base == "" {
		return apiBase
	}
	return base
}

func doJSONAt(ctx context.Context, base, token, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, resolveBase(base)+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &payload)
		if payload.Message == "" {
			payload.Message = strings.TrimSpace(string(data))
		}
		return &apiError{Status: resp.StatusCode, Message: payload.Message}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

type Identity struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

func (i Identity) NoReplyEmail() string {
	if i.ID <= 0 || i.Login == "" {
		return ""
	}
	return fmt.Sprintf("%d+%s@users.noreply.github.com", i.ID, i.Login)
}

func UserIdentity(ctx context.Context, token string) (Identity, error) {
	var out Identity
	if err := doJSON(ctx, token, http.MethodGet, "/user", nil, &out); err != nil {
		return Identity{}, err
	}
	return out, nil
}

func User(ctx context.Context, token string) (string, error) {
	id, err := UserIdentity(ctx, token)
	if err != nil {
		return "", err
	}
	return id.Login, nil
}

type Repo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`

	Owner struct {
		Login string `json:"login"`
		Type  string `json:"type"` // "User" | "Organization"
	} `json:"owner"`
}

func CreateUserRepo(ctx context.Context, token, name string) (Repo, error) {
	var out Repo
	err := doJSON(ctx, token, http.MethodPost, "/user/repos", map[string]any{
		"name":    name,
		"private": true,
	}, &out)
	return out, err
}

func CreateRepoIn(ctx context.Context, token, owner, name string) (Repo, error) {
	login, err := User(ctx, token)
	if err != nil {
		return Repo{}, err
	}
	if owner == "" || strings.EqualFold(owner, login) {
		return CreateUserRepo(ctx, token, name)
	}
	var out Repo
	err = doJSON(ctx, token, http.MethodPost, "/orgs/"+owner+"/repos", map[string]any{
		"name":    name,
		"private": true,
	}, &out)
	return out, err
}

type Owner struct {
	Login string `json:"login"`
	Type  string `json:"type"` // "user" | "org"
}

func ListOwners(ctx context.Context, token string) ([]Owner, error) {
	login, err := User(ctx, token)
	if err != nil {
		return nil, err
	}
	owners := []Owner{{Login: login, Type: "user"}}
	seen := map[string]bool{strings.ToLower(login): true}
	add := func(name string) {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		owners = append(owners, Owner{Login: strings.TrimSpace(name), Type: "org"})
	}

	var orgs []struct {
		Login string `json:"login"`
	}

	orgErr := doJSON(ctx, token, http.MethodGet, "/user/orgs?per_page=100", nil, &orgs)
	if orgErr == nil {
		for _, o := range orgs {
			add(o.Login)
		}
	}

	var memberRepos []Repo
	repoErr := doJSON(ctx, token, http.MethodGet,
		"/user/repos?per_page=100&sort=pushed&affiliation=organization_member", nil, &memberRepos)
	if repoErr == nil {
		for _, r := range memberRepos {
			if !strings.EqualFold(r.Owner.Login, login) {
				add(r.Owner.Login)
			}
		}
	}

	if orgErr != nil && repoErr != nil {
		return nil, orgErr
	}
	return owners, nil
}

func ListOwnerRepos(ctx context.Context, token, owner string) ([]Repo, error) {
	login, err := User(ctx, token)
	if err != nil {
		return nil, err
	}
	if owner == "" || strings.EqualFold(owner, login) {
		var out []Repo
		err := doJSON(ctx, token, http.MethodGet, "/user/repos?per_page=100&sort=pushed&affiliation=owner", nil, &out)
		return out, err
	}
	var out []Repo
	orgErr := doJSON(ctx, token, http.MethodGet, "/orgs/"+owner+"/repos?per_page=100&sort=pushed", nil, &out)
	if orgErr == nil && len(out) > 0 {
		return out, nil
	}
	var member []Repo
	if err := doJSON(ctx, token, http.MethodGet,
		"/user/repos?per_page=100&sort=pushed&affiliation=organization_member", nil, &member); err != nil {
		if orgErr != nil {
			return nil, orgErr
		}
		return out, nil
	}
	filtered := make([]Repo, 0, len(member))
	for _, r := range member {
		if strings.EqualFold(r.Owner.Login, owner) {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 && orgErr != nil {
		return nil, orgErr
	}
	return filtered, nil
}

func GetRepo(ctx context.Context, token, owner, name string) (Repo, error) {
	var out Repo
	err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+name, nil, &out)
	return out, err
}

func FindOpenPR(ctx context.Context, token, owner, name, headOwner, branch string) (string, error) {
	q := url.Values{}
	q.Set("head", headOwner+":"+branch)
	q.Set("state", "open")
	var out []struct {
		HTMLURL string `json:"html_url"`
	}
	if err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+name+"/pulls?"+q.Encode(), nil, &out); err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", nil
	}
	return out[0].HTMLURL, nil
}

func CreatePullRequest(ctx context.Context, token, owner, name, head, base, title string) (string, error) {
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	err := doJSON(ctx, token, http.MethodPost, repoPath(owner, name)+"/pulls", map[string]any{
		"title": title,
		"head":  head,
		"base":  base,
		"draft": false,
	}, &out)
	if err != nil {
		return "", err
	}
	return out.HTMLURL, nil
}

// EnsureOpenPullRequest returns the open pull request for head, opening one
// only when none exists. A lookup that fails is an error, not "none": creating
// on a failed lookup is how a second PR for the same branch got opened. A
// create that loses a race re-reads, so concurrent callers converge on one PR.
func EnsureOpenPullRequest(ctx context.Context, token, owner, name, headOwner, head, base, title string) (string, error) {
	existing, err := FindOpenPR(ctx, token, owner, name, headOwner, head)
	if err != nil {
		return "", fmt.Errorf("look up the open pull request for %s: %w", head, err)
	}
	if existing != "" {
		return existing, nil
	}
	created, createErr := CreatePullRequest(ctx, token, owner, name, head, base, title)
	if createErr == nil {
		return created, nil
	}
	if again, err := FindOpenPR(ctx, token, owner, name, headOwner, head); err == nil && again != "" {
		return again, nil
	}
	return "", createErr
}

func MarkPullRequestReady(ctx context.Context, token, owner, repo string, number int) error {
	var pr struct {
		NodeID string `json:"node_id"`
		Draft  bool   `json:"draft"`
	}
	if err := doJSON(ctx, token, http.MethodGet, pullPath(owner, repo, number), nil, &pr); err != nil {
		return fmt.Errorf("read pull request #%d before marking it ready: %w", number, err)
	}
	if !pr.Draft {
		return nil
	}
	if pr.NodeID == "" {
		return fmt.Errorf("pull request #%d reports no node id, so it cannot be marked ready for review", number)
	}
	body := map[string]any{
		"query": "mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{number isDraft}}}",
		"variables": map[string]any{
			"id": pr.NodeID,
		},
	}

	var out struct {
		Data struct {
			MarkPullRequestReadyForReview struct {
				PullRequest struct {
					Number  int  `json:"number"`
					IsDraft bool `json:"isDraft"`
				} `json:"pullRequest"`
			} `json:"markPullRequestReadyForReview"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := doJSON(ctx, token, http.MethodPost, "/graphql", body, &out); err != nil {
		return fmt.Errorf("mark pull request #%d ready for review: %w", number, err)
	}
	if len(out.Errors) > 0 {
		messages := make([]string, 0, len(out.Errors))
		for _, e := range out.Errors {
			messages = append(messages, e.Message)
		}
		return fmt.Errorf("mark pull request #%d ready for review: %s", number, strings.Join(messages, "; "))
	}
	if out.Data.MarkPullRequestReadyForReview.PullRequest.IsDraft {
		return fmt.Errorf("pull request #%d is still a draft after markPullRequestReadyForReview", number)
	}
	return nil
}

type MergeResult struct {
	SHA     string `json:"sha"`
	Merged  bool   `json:"merged"`
	Message string `json:"message"`
}

func MergePullRequest(ctx context.Context, token, owner, repo string, number int, expectedHeadSHA, commitTitle, commitBody string) (MergeResult, error) {
	expectedHeadSHA = strings.TrimSpace(expectedHeadSHA)
	if expectedHeadSHA == "" {
		return MergeResult{}, fmt.Errorf("refusing to merge pull request #%d without the head commit it was verified at", number)
	}
	payload := map[string]any{
		"merge_method": "squash",
		"sha":          expectedHeadSHA,
	}
	if strings.TrimSpace(commitTitle) != "" {
		payload["commit_title"] = commitTitle
	}
	if strings.TrimSpace(commitBody) != "" {
		payload["commit_message"] = commitBody
	}
	var out MergeResult
	if err := doJSON(ctx, token, http.MethodPut, pullPath(owner, repo, number)+"/merge", payload, &out); err != nil {
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			switch apiErr.Status {
			case http.StatusMethodNotAllowed:
				return MergeResult{}, fmt.Errorf("github refused to merge pull request #%d — it is not in a mergeable state (draft, conflicting, a required check not green, or branch protection): %s", number, apiErr.Message)
			case http.StatusConflict:
				return MergeResult{}, fmt.Errorf("github refused to merge pull request #%d — its head is no longer at %s, so something was pushed after this task was verified: %s", number, shortSHA(expectedHeadSHA), apiErr.Message)
			}
		}
		return MergeResult{}, fmt.Errorf("merge pull request #%d: %w", number, err)
	}
	if !out.Merged || strings.TrimSpace(out.SHA) == "" {
		return out, fmt.Errorf("github reported pull request #%d as not merged: %s", number, out.Message)
	}
	return out, nil
}

func DeleteBranch(ctx context.Context, token, owner, repo, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("no branch name to delete")
	}
	path := repoPath(owner, repo) + "/git/refs/heads/" + refSegments(branch)
	if err := doJSON(ctx, token, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	return nil
}

func shortSHA(sha string) string {
	if len(sha) < 12 {
		return sha
	}
	return sha[:12]
}

func ParseOwnerRepo(origin string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(origin)
	switch {
	case strings.HasPrefix(s, "git@github.com:"):
		s = strings.TrimPrefix(s, "git@github.com:")
	case strings.Contains(s, "github.com/"):
		idx := strings.Index(s, "github.com/")
		s = s[idx+len("github.com/"):]
	default:
		return "", "", false
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
