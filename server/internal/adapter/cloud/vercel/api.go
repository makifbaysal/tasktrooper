// Package vercel is the port.CloudProvider adapter for Vercel: who a token
// is, which teams it can act in, its projects and their deployments, logs and
// runtime errors.
//
// Token source: an access token the operator creates at
// vercel.com/account/tokens and saves as a cloud account. It is stored
// encrypted and never leaves the server.
package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Client is the low-level Vercel REST client Provider is built on.
type Client struct {
	// BaseURL is a field (not a const) so tests can point it at an httptest
	// server. Empty means the public API.
	BaseURL string
	HTTP    *http.Client
}

// New returns a client against api.vercel.com with a 15s per-call timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}}
}

const defaultBase = "https://api.vercel.com"

// projectPageLimit is Vercel's maximum page size for /v9/projects; maxPages
// bounds a runaway pagination on a scope with thousands of projects.
const (
	projectPageLimit = 100
	maxPages         = 10
)

// APIError is a non-2xx answer from Vercel, with the message it carried.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vercel api: %d %s", e.Status, e.Message)
}

// IsUnauthorized reports whether err is Vercel refusing the token itself.
func IsUnauthorized(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden
	}
	return false
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBase
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) getJSON(ctx context.Context, token, path string, query url.Values, out any) error {
	u := c.base() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &payload)
		msg := payload.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("vercel api: decode %s: %w", path, err)
		}
	}
	return nil
}

// withTeam adds teamId to a query when the scope is a team.
func withTeam(q url.Values, teamID string) url.Values {
	if q == nil {
		q = url.Values{}
	}
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	return q
}

// User — GET /v2/user.
func (c *Client) User(ctx context.Context, token string) (domain.VercelUser, error) {
	var out struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
			Name     string `json:"name"`
		} `json:"user"`
	}
	if err := c.getJSON(ctx, token, "/v2/user", nil, &out); err != nil {
		return domain.VercelUser{}, err
	}
	if out.User.ID == "" && out.User.Username == "" {
		return domain.VercelUser{}, fmt.Errorf("vercel api: /v2/user returned no user")
	}
	return domain.VercelUser{ID: out.User.ID, Username: out.User.Username, Email: out.User.Email, Name: out.User.Name}, nil
}

// Teams — GET /v2/teams. One page of 100 is every team a human token
// realistically belongs to.
func (c *Client) Teams(ctx context.Context, token string) ([]domain.VercelTeam, error) {
	var out struct {
		Teams []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	q := url.Values{}
	q.Set("limit", "100")
	if err := c.getJSON(ctx, token, "/v2/teams", q, &out); err != nil {
		return nil, err
	}
	teams := make([]domain.VercelTeam, 0, len(out.Teams))
	for _, t := range out.Teams {
		teams = append(teams, domain.VercelTeam{ID: t.ID, Slug: t.Slug, Name: t.Name})
	}
	return teams, nil
}

// rawProject is the subset of Vercel's project object this package reads.
type rawProject struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Framework     *string `json:"framework"`
	RootDirectory *string `json:"rootDirectory"`
	UpdatedAt     int64   `json:"updatedAt"`
	Link          *struct {
		Type             string `json:"type"`
		Org              string `json:"org"`
		Repo             string `json:"repo"`
		ProductionBranch string `json:"productionBranch"`
		// GitLab / Bitbucket spell the same two facts differently.
		ProjectNamespace string `json:"projectNamespace"`
		ProjectName      string `json:"projectName"`
		Owner            string `json:"owner"`
		Slug             string `json:"slug"`
	} `json:"link"`
	Targets struct {
		Production *struct {
			URL   string   `json:"url"`
			Alias []string `json:"alias"`
		} `json:"production"`
	} `json:"targets"`
}

func (r rawProject) toDomain(teamID string) domain.VercelProject {
	p := domain.VercelProject{ID: r.ID, Name: r.Name, TeamID: teamID}
	if r.Framework != nil {
		p.Framework = *r.Framework
	}
	if r.RootDirectory != nil {
		p.RootDirectory = strings.Trim(*r.RootDirectory, "/")
	}
	if r.UpdatedAt > 0 {
		p.UpdatedAt = time.UnixMilli(r.UpdatedAt).UTC()
	}
	if r.Link != nil && r.Link.Type != "" {
		link := &domain.VercelGitLink{Type: r.Link.Type, ProductionBranch: r.Link.ProductionBranch}
		switch {
		case r.Link.Org != "" || r.Link.Repo != "":
			link.Org, link.Repo = r.Link.Org, r.Link.Repo
		case r.Link.ProjectNamespace != "" || r.Link.ProjectName != "":
			link.Org, link.Repo = r.Link.ProjectNamespace, r.Link.ProjectName
		case r.Link.Owner != "" || r.Link.Slug != "":
			link.Org, link.Repo = r.Link.Owner, r.Link.Slug
		}
		p.Link = link
	}
	p.ProductionURL = productionURL(r)
	return p
}

func httpsOf(host string) string {
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	return "https://" + strings.TrimRight(host, "/")
}

// productionURL picks the address production answers on: a custom domain
// first (an alias that is not *.vercel.app), then any production alias, then
// the production target's own URL, and finally the <name>.vercel.app default
// every project gets.
func productionURL(r rawProject) string {
	var aliases []string
	if r.Targets.Production != nil {
		aliases = append(aliases, r.Targets.Production.Alias...)
	}
	for _, a := range aliases {
		if a != "" && !strings.HasSuffix(strings.ToLower(a), ".vercel.app") {
			return httpsOf(a)
		}
	}
	for _, a := range aliases {
		if a != "" {
			return httpsOf(a)
		}
	}
	if r.Targets.Production != nil && r.Targets.Production.URL != "" {
		return httpsOf(r.Targets.Production.URL)
	}
	if r.Name != "" {
		return "https://" + r.Name + ".vercel.app"
	}
	return ""
}

// Projects — GET /v9/projects, paginated with the `until` cursor Vercel
// returns in pagination.next. Bounded by maxPages.
func (c *Client) Projects(ctx context.Context, token, teamID string) ([]domain.VercelProject, error) {
	var all []domain.VercelProject
	var until int64
	for page := 0; page < maxPages; page++ {
		q := withTeam(url.Values{}, teamID)
		q.Set("limit", strconv.Itoa(projectPageLimit))
		if until > 0 {
			q.Set("until", strconv.FormatInt(until, 10))
		}
		var out struct {
			Projects   []rawProject `json:"projects"`
			Pagination struct {
				Next int64 `json:"next"`
			} `json:"pagination"`
		}
		if err := c.getJSON(ctx, token, "/v9/projects", q, &out); err != nil {
			return nil, err
		}
		for _, r := range out.Projects {
			all = append(all, r.toDomain(teamID))
		}
		if out.Pagination.Next == 0 || len(out.Projects) < projectPageLimit || out.Pagination.Next == until {
			break
		}
		until = out.Pagination.Next
	}
	return all, nil
}

// Project — GET /v9/projects/{idOrName}.
func (c *Client) Project(ctx context.Context, token, teamID, idOrName string) (domain.VercelProject, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return domain.VercelProject{}, fmt.Errorf("vercel api: project id is required")
	}
	var r rawProject
	if err := c.getJSON(ctx, token, "/v9/projects/"+url.PathEscape(idOrName), withTeam(nil, teamID), &r); err != nil {
		return domain.VercelProject{}, err
	}
	return r.toDomain(teamID), nil
}
