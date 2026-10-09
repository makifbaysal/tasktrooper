package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEmail  = "dev@example.com"
	testToken  = "atlassian-api-token"
	testIssue  = "PROJ-12"
	basicCreds = testEmail + ":" + testToken
)

func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return newAtBase(srv.URL, testEmail, testToken, srv.Client())
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func writeErrorJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestValidateSiteURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "https cloud site", input: "https://acme.atlassian.net", want: "https://acme.atlassian.net"},
		{name: "scheme omitted", input: "acme.atlassian.net", want: "https://acme.atlassian.net"},
		{name: "mixed case with root path", input: "https://Acme.Atlassian.net/", want: "https://acme.atlassian.net"},
		{name: "surrounding spaces", input: "  https://acme.atlassian.net  ", want: "https://acme.atlassian.net"},
		{name: "dashed site name", input: "acme-dev.atlassian.net", want: "https://acme-dev.atlassian.net"},
		{name: "plain http", input: "http://acme.atlassian.net"},
		{name: "other host", input: "https://evil.com"},
		{name: "bare apex domain", input: "https://atlassian.net"},
		{name: "suffix appended to the site", input: "https://acme.atlassian.net.evil.com"},
		{name: "userinfo", input: "https://user:pw@acme.atlassian.net"},
		{name: "explicit port", input: "https://acme.atlassian.net:8443"},
		{name: "path", input: "https://acme.atlassian.net/jira"},
		{name: "ip literal", input: "https://127.0.0.1"},
		{name: "query", input: "https://acme.atlassian.net?x=1"},
		{name: "fragment", input: "https://acme.atlassian.net#x"},
		{name: "underscore in host", input: "https://acme_x.atlassian.net"},
		{name: "empty", input: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateSiteURL(tt.input)
			if tt.want == "" {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidSite)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewRejectsInvalidSite(t *testing.T) {
	client, err := New("http://acme.atlassian.net", testEmail, testToken, nil)
	require.ErrorIs(t, err, ErrInvalidSite)
	assert.Nil(t, client)
}

func TestNewDefaultsTheHTTPClient(t *testing.T) {
	client, err := New("https://acme.atlassian.net", testEmail, testToken, nil)
	require.NoError(t, err)
	require.NotNil(t, client.http)
	assert.Equal(t, defaultTimeout, client.http.Timeout)
	assert.Equal(t, "https://acme.atlassian.net", client.baseURL)
}

func TestRequestCarriesCredentialsAndHeaders(t *testing.T) {
	var (
		mu     sync.Mutex
		header http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		header = r.Header.Clone()
		mu.Unlock()
		writeJSON(w, `{"accountId":"5f0","displayName":"Dev","emailAddress":"dev@example.com"}`)
	}))
	defer srv.Close()

	myself, err := newTestClient(t, srv).Myself(context.Background())
	require.NoError(t, err)
	assert.Equal(t, Myself{AccountID: "5f0", DisplayName: "Dev", EmailAddress: testEmail}, myself)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(basicCreds)), header.Get("Authorization"))
	assert.Equal(t, "application/json", header.Get("Accept"))
	assert.Equal(t, "TaskTrooper", header.Get("User-Agent"))
	assert.Empty(t, header.Get("Content-Type"))
}

func TestSearchSendsJQLAndParsesIssues(t *testing.T) {
	var received struct {
		JQL        string   `json:"jql"`
		MaxResults int      `json:"maxResults"`
		Fields     []string `json:"fields"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/rest/api/3/search/jql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &received))
		writeJSON(w, `{
			"issues": [
				{
					"id": "10001",
					"key": "PROJ-12",
					"fields": {
						"summary": "Ship the thing",
						"description": {"type":"doc","version":1,"content":[
							{"type":"paragraph","content":[{"type":"text","text":"first line"},{"type":"hardBreak"},{"type":"text","text":"second line"}]}
						]},
						"status": {"name": "In Progress", "statusCategory": {"key": "indeterminate"}},
						"issuetype": {"name": "Story"},
						"priority": {"name": "High"},
						"labels": ["backend", "runner"],
						"updated": "2026-02-03T10:11:12.345+0300"
					}
				},
				{
					"id": "10002",
					"key": "PROJ-13",
					"fields": {"summary": "No priority", "status": {"name": "Done", "statusCategory": {"key": "done"}}}
				}
			]
		}`)
	}))
	defer srv.Close()

	issues, err := newTestClient(t, srv).Search(context.Background(), "project = PROJ", 500)
	require.NoError(t, err)

	assert.Equal(t, "project = PROJ", received.JQL)
	assert.Equal(t, 100, received.MaxResults)
	assert.Equal(t, issueFieldNames(), received.Fields)

	require.Len(t, issues, 2)
	first := issues[0]
	assert.Equal(t, "2026-02-03T10:11:12.345+0300", first.Updated.Format(jiraTimeLayout))
	first.Updated = time.Time{}
	assert.Equal(t, Issue{
		ID:                  "10001",
		Key:                 "PROJ-12",
		Summary:             "Ship the thing",
		DescriptionMarkdown: "first line\nsecond line",
		Status:              "In Progress",
		StatusCategory:      "indeterminate",
		IssueType:           "Story",
		Priority:            "High",
		Labels:              []string{"backend", "runner"},
		URL:                 srv.URL + "/browse/PROJ-12",
	}, first)
	assert.Equal(t, srv.URL+"/browse/PROJ-13", issues[1].URL)
	assert.Empty(t, issues[1].Priority)
	assert.True(t, issues[1].Updated.IsZero())
}

func TestSearchClampsMaxResults(t *testing.T) {
	var maxResults int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received struct {
			MaxResults int `json:"maxResults"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		maxResults = received.MaxResults
		writeJSON(w, `{"issues":[]}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Search(context.Background(), "project = PROJ", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, maxResults)

	_, err = newTestClient(t, srv).Search(context.Background(), "project = PROJ", -5)
	require.NoError(t, err)
	assert.Equal(t, 1, maxResults)

	_, err = newTestClient(t, srv).Search(context.Background(), "project = PROJ", 25)
	require.NoError(t, err)
	assert.Equal(t, 25, maxResults)
}

func TestGetIssue(t *testing.T) {
	var path, fields string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fields = r.URL.Query().Get("fields")
		writeJSON(w, `{"id":"10001","key":"PROJ-12","fields":{
			"summary":"Ship the thing",
			"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]},
			"status":{"name":"Done","statusCategory":{"key":"done"}},
			"issuetype":{"name":"Bug"}
		}}`)
	}))
	defer srv.Close()

	issue, err := newTestClient(t, srv).GetIssue(context.Background(), "PROJ-12")
	require.NoError(t, err)

	assert.Equal(t, "/rest/api/3/issue/PROJ-12", path)
	assert.Equal(t, strings.Join(issueFieldNames(), ","), fields)
	assert.Equal(t, "PROJ-12", issue.Key)
	assert.Equal(t, "body", issue.DescriptionMarkdown)
	assert.Equal(t, "done", issue.StatusCategory)
	assert.Equal(t, srv.URL+"/browse/PROJ-12", issue.URL)
}

func TestIssueKeysAreValidatedBeforeAnyRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, `{}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	invalid := []string{"", "proj-12", "PROJ-0", "PROJ-12/../other", "PROJ-12?x=1", "PROJ 12", strings.Repeat("A", 21) + "-1", "PROJ-12345678901"}

	for _, key := range invalid {
		_, err := client.GetIssue(context.Background(), key)
		require.Error(t, err, "key %q must be rejected", key)
		require.Error(t, client.AddComment(context.Background(), key, "hi"))
		_, err = client.TransitionToDone(context.Background(), key)
		require.Error(t, err, "key %q must be rejected", key)
	}
	assert.False(t, called)
}

func TestAddCommentBuildsADF(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "blank line separates paragraphs",
			text: "a\n\nb",
			want: `{"body":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"a"}]},
				{"type":"paragraph","content":[{"type":"text","text":"b"}]}
			]}}`,
		},
		{
			name: "single newline becomes a hard break",
			text: "a\nb",
			want: `{"body":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}
			]}}`,
		},
		{
			name: "markdown stays literal",
			text: "**not bold**",
			want: `{"body":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"**not bold**"}]}
			]}}`,
		},
		{
			name: "several blank lines collapse",
			text: "a\n\n\n\nb\n\n",
			want: `{"body":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"a"}]},
				{"type":"paragraph","content":[{"type":"text","text":"b"}]}
			]}}`,
		},
		{
			name: "windows line endings",
			text: "a\r\n\r\nb",
			want: `{"body":{"type":"doc","version":1,"content":[
				{"type":"paragraph","content":[{"type":"text","text":"a"}]},
				{"type":"paragraph","content":[{"type":"text","text":"b"}]}
			]}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotBody string
				gotPath string
				gotVerb string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotVerb = r.Method
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				gotBody = string(body)
				writeJSON(w, `{"id":"1"}`)
			}))
			defer srv.Close()

			require.NoError(t, newTestClient(t, srv).AddComment(context.Background(), testIssue, tt.text))

			assert.Equal(t, http.MethodPost, gotVerb)
			assert.Equal(t, "/rest/api/3/issue/PROJ-12/comment", gotPath)
			assert.JSONEq(t, tt.want, gotBody)
		})
	}
}

func TestAddCommentWithoutTextMakesNoRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, `{}`)
	}))
	defer srv.Close()

	require.NoError(t, newTestClient(t, srv).AddComment(context.Background(), testIssue, "  \n\n \n"))
	assert.False(t, called)
}

func TestTransitionToDone(t *testing.T) {
	tests := []struct {
		name      string
		answer    string
		wantMoved bool
	}{
		{
			name: "picks the first transition into the done category",
			answer: `{"transitions":[
				{"id":"11","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},
				{"id":"21","to":{"name":"Closed","statusCategory":{"key":"done"}}},
				{"id":"31","to":{"name":"Done","statusCategory":{"key":"done"}}}
			]}`,
			wantMoved: true,
		},
		{
			name: "no transition reaches done",
			answer: `{"transitions":[
				{"id":"11","to":{"name":"To Do","statusCategory":{"key":"new"}}},
				{"id":"21","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}
			]}`,
			wantMoved: false,
		},
		{
			name:      "no transitions at all",
			answer:    `{"transitions":[]}`,
			wantMoved: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotGetPath   string
				gotPostPath  string
				gotPostBody  string
				gotPostCount int
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					gotGetPath = r.URL.Path
					writeJSON(w, tt.answer)
				case http.MethodPost:
					gotPostPath = r.URL.Path
					gotPostCount++
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					gotPostBody = string(body)
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer srv.Close()

			moved, err := newTestClient(t, srv).TransitionToDone(context.Background(), testIssue)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMoved, moved)
			assert.Equal(t, "/rest/api/3/issue/PROJ-12/transitions", gotGetPath)

			if !tt.wantMoved {
				assert.Zero(t, gotPostCount)
				return
			}
			assert.Equal(t, 1, gotPostCount)
			assert.Equal(t, "/rest/api/3/issue/PROJ-12/transitions", gotPostPath)
			assert.JSONEq(t, `{"transition":{"id":"21"}}`, gotPostBody)
		})
	}
}

func TestTransitionToDoneSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorJSON(w, http.StatusNotFound, `{"errorMessages":["Issue does not exist or you do not have permission to see it."]}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).TransitionToDone(context.Background(), testIssue)
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Status)
}

func TestUnauthorizedDoesNotLeakCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorJSON(w, http.StatusUnauthorized, `{"errorMessages":["Client must be authenticated to access this resource."],"errors":{"token":"bad token"}}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Myself(context.Background())
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.Status)
	assert.Equal(t, []string{"Client must be authenticated to access this resource.", "bad token"}, apiErr.Messages)
	assert.Equal(t, "jira: HTTP 401: Client must be authenticated to access this resource.; bad token", apiErr.Error())

	assert.NotContains(t, err.Error(), base64.StdEncoding.EncodeToString([]byte(basicCreds)))
	assert.NotContains(t, err.Error(), testToken)
	assert.NotContains(t, err.Error(), testEmail)
	assert.NotContains(t, err.Error(), "Basic ")
}

func TestAPIErrorCapsMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrorJSON(w, http.StatusBadRequest, `{"errorMessages":["1","2","3","4","5","6"],"errors":{"a":"x"}}`)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Myself(context.Background())
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Len(t, apiErr.Messages, 5)
	assert.Equal(t, "1", apiErr.Messages[0])

	long := newAPIError(http.StatusBadRequest, []byte(`{"errorMessages":["`+strings.Repeat("x", 400)+`"]}`))
	require.Len(t, long.Messages, 1)
	assert.Len(t, []rune(long.Messages[0]), maxAPIMsgLen)
}

func TestAPIErrorFallsBackToTheRawBody(t *testing.T) {
	err := newAPIError(http.StatusBadGateway, []byte("  <html>\n  gateway down\n</html>  "))
	assert.Equal(t, []string{"<html> gateway down </html>"}, err.Messages)
}

func TestProjectsFollowsPagination(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/3/project/search", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("maxResults"))
		startAt := r.URL.Query().Get("startAt")
		requested = append(requested, startAt)
		switch startAt {
		case "0":
			writeJSON(w, `{"isLast":false,"total":3,"startAt":0,"maxResults":50,"values":[
				{"key":"AB","name":"Alpha"},{"key":"CD","name":"Beta"}
			]}`)
		case "2":
			writeJSON(w, `{"isLast":true,"total":3,"startAt":2,"maxResults":50,"values":[
				{"key":"EF","name":"Gamma"}
			]}`)
		default:
			t.Errorf("unexpected startAt %q", startAt)
			writeJSON(w, `{"isLast":true,"values":[]}`)
		}
	}))
	defer srv.Close()

	projects, err := newTestClient(t, srv).Projects(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"0", "2"}, requested)
	assert.Equal(t, []Project{{Key: "AB", Name: "Alpha"}, {Key: "CD", Name: "Beta"}, {Key: "EF", Name: "Gamma"}}, projects)
}

func TestProjectsCapsAtFiveHundredAndIgnoresTheReportedCursor(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		items := make([]map[string]string, 0, 50)
		for i := 0; i < 50; i++ {
			items = append(items, map[string]string{"key": fmt.Sprintf("K%d-%d", calls, i), "name": "Project"})
		}
		payload, err := json.Marshal(map[string]any{
			"isLast":    false,
			"startAt":   0,
			"maxResult": 50,
			"values":    items,
		})
		require.NoError(t, err)
		writeJSON(w, string(payload))
	}))
	defer srv.Close()

	projects, err := newTestClient(t, srv).Projects(context.Background())
	require.NoError(t, err)
	assert.Len(t, projects, maxProjects)
	assert.Equal(t, 10, calls)
	assert.Equal(t, "K1-0", projects[0].Key)
	assert.Equal(t, "K10-49", projects[maxProjects-1].Key)
}

func TestProjectsStopsOnAnEmptyPage(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, `{"isLast":false,"startAt":0,"values":[]}`)
	}))
	defer srv.Close()

	projects, err := newTestClient(t, srv).Projects(context.Background())
	require.NoError(t, err)
	assert.Empty(t, projects)
	assert.Equal(t, 1, calls)
}

func TestQuoteJQL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "Fix the login bug", want: `"Fix the login bug"`},
		{name: "double quote", input: `say "hi"`, want: `"say \"hi\""`},
		{name: "backslash", input: `C:\Users`, want: `"C:\\Users"`},
		{name: "both", input: `a\b"c`, want: `"a\\b\"c"`},
		{name: "control characters dropped", input: "line\nbreak\ttab\x00nul", want: `"linebreaktabnul"`},
		{name: "empty", input: "", want: `""`},
		{name: "jql injection stays inert", input: `" OR project = SECRET`, want: `"\" OR project = SECRET"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, QuoteJQL(tt.input))
		})
	}
}

func TestPatterns(t *testing.T) {
	assert.True(t, IssueKeyPattern.MatchString("PROJ-12"))
	assert.True(t, IssueKeyPattern.MatchString("A_1-9"))
	assert.False(t, IssueKeyPattern.MatchString("PROJ-0"))
	assert.False(t, IssueKeyPattern.MatchString("PROJ-"))
	assert.False(t, IssueKeyPattern.MatchString("proj-1"))
	assert.False(t, IssueKeyPattern.MatchString("PROJ-1\n"))
	assert.False(t, IssueKeyPattern.MatchString("PROJ-1.2"))

	assert.True(t, ProjectKeyPattern.MatchString("PROJ"))
	assert.False(t, ProjectKeyPattern.MatchString("proj"))
	assert.False(t, ProjectKeyPattern.MatchString("PROJ-1"))
	assert.False(t, ProjectKeyPattern.MatchString(strings.Repeat("A", 21)))
}

func TestRequestsHonourContextCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		writeJSON(w, `{}`)
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newTestClient(t, srv).Myself(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
