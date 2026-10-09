package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	cloudHostSuffix = ".atlassian.net"
	maxBodyBytes    = 4 << 20
	defaultTimeout  = 20 * time.Second
	searchMaxIssues = 100
	projectPageSize = 50
	maxProjects     = 500
	maxAPIMessages  = 5
	maxAPIMsgLen    = 300
)

var (
	// ErrInvalidSite is the sentinel for every rejected site URL.
	ErrInvalidSite = errors.New("jira: site must be https://<name>.atlassian.net")

	IssueKeyPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}-[1-9][0-9]{0,9}$`)
	ProjectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,19}$`)

	blankLineBlock = regexp.MustCompile(`\n[ \t]*\n`)
)

// ValidateSiteURL accepts only a Jira Cloud site and returns its normalized
// base URL. A missing scheme is read as https.
func ValidateSiteURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", fmt.Errorf("%w: empty", ErrInvalidSite)
	}
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSite, err)
	}
	switch {
	case parsed.Scheme != "https" || parsed.Opaque != "":
		return "", fmt.Errorf("%w: only https is allowed", ErrInvalidSite)
	case parsed.User != nil:
		return "", fmt.Errorf("%w: credentials in the URL are not allowed", ErrInvalidSite)
	case parsed.Port() != "":
		return "", fmt.Errorf("%w: an explicit port is not allowed", ErrInvalidSite)
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return "", fmt.Errorf("%w: a query string is not allowed", ErrInvalidSite)
	case parsed.Fragment != "" || parsed.RawFragment != "":
		return "", fmt.Errorf("%w: a fragment is not allowed", ErrInvalidSite)
	case parsed.Path != "" && parsed.Path != "/":
		return "", fmt.Errorf("%w: only the site root is allowed", ErrInvalidSite)
	}
	host := strings.ToLower(parsed.Hostname())
	if !isCloudHost(host) {
		return "", fmt.Errorf("%w: %q is not a Jira Cloud host", ErrInvalidSite, host)
	}
	return "https://" + host, nil
}

func isCloudHost(host string) bool {
	if !strings.HasSuffix(host, cloudHostSuffix) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
			if !isAllowed {
				return false
			}
		}
	}
	return true
}

// Client talks to one Jira Cloud site's REST v3 API. It holds the API token in
// its Authorization header and never writes it to an error or a log.
type Client struct {
	baseURL   string
	authValue string
	http      *http.Client
}

// New builds a client for siteURL. A nil httpClient means a client with a 20s
// timeout.
func New(siteURL, email, apiToken string, httpClient *http.Client) (*Client, error) {
	base, err := ValidateSiteURL(siteURL)
	if err != nil {
		return nil, err
	}
	return newAtBase(base, email, apiToken, httpClient), nil
}

// newAtBase skips site validation so tests can point a client at a loopback
// httptest server, which is not a Jira Cloud host.
func newAtBase(baseURL, email, apiToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		authValue: base64.StdEncoding.EncodeToString([]byte(email + ":" + apiToken)),
		http:      httpClient,
	}
}

// APIError is returned for any non-2xx answer.
type APIError struct {
	Status   int
	Messages []string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jira: HTTP %d: %s", e.Status, strings.Join(e.Messages, "; "))
}

func newAPIError(status int, body []byte) *APIError {
	var payload struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	candidates := make([]string, 0, 4)
	if err := json.Unmarshal(body, &payload); err == nil {
		candidates = append(candidates, payload.ErrorMessages...)
		for _, field := range sortedFields(payload.Errors) {
			candidates = append(candidates, payload.Errors[field])
		}
	}
	if len(candidates) == 0 {
		candidates = []string{string(body)}
	}
	messages := make([]string, 0, maxAPIMessages)
	for _, candidate := range candidates {
		message := strings.TrimSpace(collapseSpaces(candidate))
		if message == "" {
			continue
		}
		runes := []rune(message)
		if len(runes) > maxAPIMsgLen {
			message = string(runes[:maxAPIMsgLen])
		}
		messages = append(messages, message)
		if len(messages) == maxAPIMessages {
			break
		}
	}
	return &APIError{Status: status, Messages: messages}
}

func sortedFields(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+c.authValue)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TaskTrooper")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp.StatusCode, data)
	}
	if readErr != nil {
		return readErr
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("jira: decode %s %s: %w", method, path, err)
	}
	return nil
}

type Myself struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

func (c *Client) Myself(ctx context.Context) (Myself, error) {
	var out Myself
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, &out); err != nil {
		return Myself{}, err
	}
	return out, nil
}

type Project struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	projects := make([]Project, 0, projectPageSize)
	startAt := 0
	for {
		query := url.Values{}
		query.Set("startAt", fmt.Sprintf("%d", startAt))
		query.Set("maxResults", fmt.Sprintf("%d", projectPageSize))
		var page struct {
			Values   []Project `json:"values"`
			Projects []Project `json:"projects"`
			IsLast   bool      `json:"isLast"`
		}
		if err := c.do(ctx, http.MethodGet, "/rest/api/3/project/search?"+query.Encode(), nil, &page); err != nil {
			return nil, err
		}
		items := page.Values
		if len(items) == 0 {
			items = page.Projects
		}
		projects = append(projects, items...)
		if len(projects) >= maxProjects {
			return projects[:maxProjects], nil
		}
		if page.IsLast || len(items) == 0 {
			return projects, nil
		}
		startAt += len(items)
	}
}

type Issue struct {
	ID                  string
	Key                 string
	Summary             string
	DescriptionMarkdown string
	Status              string
	StatusCategory      string
	IssueType           string
	Priority            string
	Labels              []string
	URL                 string
	Updated             time.Time
}

type issuePayload struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Status      struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		Labels  []string `json:"labels"`
		Updated string   `json:"updated"`
	} `json:"fields"`
}

const jiraTimeLayout = "2006-01-02T15:04:05.000-0700"

func issueFieldNames() []string {
	return []string{"summary", "description", "status", "issuetype", "priority", "labels", "updated"}
}

func (c *Client) toIssue(raw issuePayload) Issue {
	priority := ""
	if raw.Fields.Priority != nil {
		priority = raw.Fields.Priority.Name
	}
	updated, _ := time.Parse(jiraTimeLayout, raw.Fields.Updated)
	issue := Issue{
		ID:                  raw.ID,
		Key:                 raw.Key,
		Summary:             raw.Fields.Summary,
		DescriptionMarkdown: ADFToMarkdown(raw.Fields.Description),
		Status:              raw.Fields.Status.Name,
		StatusCategory:      raw.Fields.Status.StatusCategory.Key,
		IssueType:           raw.Fields.IssueType.Name,
		Priority:            priority,
		Labels:              raw.Fields.Labels,
		Updated:             updated,
	}
	if issue.Key != "" {
		issue.URL = c.baseURL + "/browse/" + issue.Key
	}
	return issue
}

func (c *Client) toIssues(raws []issuePayload) []Issue {
	issues := make([]Issue, 0, len(raws))
	for _, raw := range raws {
		issues = append(issues, c.toIssue(raw))
	}
	return issues
}

// Search runs jql and returns the first page only, with max clamped to 1..100.
func (c *Client) Search(ctx context.Context, jql string, max int) ([]Issue, error) {
	if max < 1 {
		max = 1
	}
	if max > searchMaxIssues {
		max = searchMaxIssues
	}
	body := map[string]any{
		"jql":        jql,
		"maxResults": max,
		"fields":     issueFieldNames(),
	}
	var page struct {
		Issues []issuePayload `json:"issues"`
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", body, &page); err != nil {
		return nil, err
	}
	return c.toIssues(page.Issues), nil
}

func (c *Client) GetIssue(ctx context.Context, key string) (Issue, error) {
	if !IssueKeyPattern.MatchString(key) {
		return Issue{}, fmt.Errorf("jira: %q is not a valid issue key", key)
	}
	query := url.Values{}
	query.Set("fields", strings.Join(issueFieldNames(), ","))
	var raw issuePayload
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+key+"?"+query.Encode(), nil, &raw); err != nil {
		return Issue{}, err
	}
	return c.toIssue(raw), nil
}

// AddComment posts text as plain paragraphs. Empty text is a no-op: Jira has
// nothing to show for a comment with no block.
func (c *Client) AddComment(ctx context.Context, key, text string) error {
	if !IssueKeyPattern.MatchString(key) {
		return fmt.Errorf("jira: %q is not a valid issue key", key)
	}
	blocks := paragraphBlocks(text)
	if len(blocks) == 0 {
		return nil
	}
	body := map[string]any{
		"body": map[string]any{
			"type":    "doc",
			"version": 1,
			"content": blocks,
		},
	}
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+key+"/comment", body, nil); err != nil {
		return err
	}
	return nil
}

func paragraphBlocks(text string) []map[string]any {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	blocks := make([]map[string]any, 0, 4)
	for _, block := range blankLineBlock.Split(text, -1) {
		lines := strings.Split(block, "\n")
		content := make([]map[string]any, 0, len(lines)*2)
		kept := false
		for i, line := range lines {
			line = strings.Trim(line, " \t")
			if i > 0 {
				content = append(content, map[string]any{"type": "hardBreak"})
			}
			if line == "" {
				continue
			}
			content = append(content, map[string]any{"type": "text", "text": line})
			kept = true
		}
		if kept {
			blocks = append(blocks, map[string]any{"type": "paragraph", "content": content})
		}
	}
	return blocks
}

// TransitionToDone moves the issue to the first transition whose target status
// category is done, and reports whether such a transition existed.
func (c *Client) TransitionToDone(ctx context.Context, key string) (bool, error) {
	if !IssueKeyPattern.MatchString(key) {
		return false, fmt.Errorf("jira: %q is not a valid issue key", key)
	}
	var available struct {
		Transitions []struct {
			ID string `json:"id"`
			To struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
		} `json:"transitions"`
	}
	path := "/rest/api/3/issue/" + key + "/transitions"
	if err := c.do(ctx, http.MethodGet, path, nil, &available); err != nil {
		return false, err
	}
	for _, transition := range available.Transitions {
		if transition.To.StatusCategory.Key != "done" {
			continue
		}
		body := map[string]any{"transition": map[string]any{"id": transition.ID}}
		if err := c.do(ctx, http.MethodPost, path, body, nil); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// QuoteJQL renders s as a JQL string literal, so an issue summary can be
// searched on without letting it change the query's meaning.
func QuoteJQL(s string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			quoted.WriteByte('\\')
			quoted.WriteRune(r)
		case unicode.IsControl(r):
		default:
			quoted.WriteRune(r)
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
}
