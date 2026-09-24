package vercel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestKind(t *testing.T) {
	p := NewProvider(New())
	assert.Equal(t, domain.CloudVercel, p.Kind())
}

func TestVerifyWithoutTeam(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"akif","email":"a@b.c"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	meta, err := p.Verify(context.Background(), cred)
	require.NoError(t, err)
	assert.Equal(t, "/v2/user", gotPath)
	assert.Equal(t, map[string]string{"username": "akif", "email": "a@b.c"}, meta)
}

func TestVerifyWithTeamFetchesTeamIdentity(t *testing.T) {
	var gotPaths []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/v2/user":
			_, _ = w.Write([]byte(`{"user":{"id":"u1","username":"akif","email":"a@b.c"}}`))
		case "/v2/teams/team_1":
			_, _ = w.Write([]byte(`{"id":"team_1","slug":"acme","name":"Acme Inc"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}

	meta, err := p.Verify(context.Background(), cred)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/v2/user", "/v2/teams/team_1"}, gotPaths)
	assert.Equal(t, map[string]string{
		"username":  "akif",
		"email":     "a@b.c",
		"team_id":   "team_1",
		"team_slug": "acme",
		"team_name": "Acme Inc",
	}, meta)
}

func TestVerifyUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"Not authorized"}}`))
		}))
		p := NewProvider(c)
		cred := domain.CloudCredential{Fields: map[string]string{"token": "bad"}}

		_, err := p.Verify(context.Background(), cred)
		require.Error(t, err)
		assert.ErrorIs(t, err, port.ErrCloudAuth, "status %d", status)
		assert.Contains(t, err.Error(), "Not authorized")
	}
}

func TestVerifyMissingTokenNeverCallsTheNetwork(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call the API without a token")
	}))
	p := NewProvider(c)
	_, err := p.Verify(context.Background(), domain.CloudCredential{Fields: map[string]string{}})
	require.Error(t, err)
}

func TestListResourcesPaginatesAndMapsLabelsDomainsExtra(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("teamId") != "team_1" {
			t.Errorf("teamId missing: %s", r.URL.RawQuery)
		}
		switch r.URL.Query().Get("until") {
		case "":
			var b strings.Builder
			b.WriteString(`{"projects":[`)
			for i := 0; i < 100; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{"id":"prj_%d","name":"p%d"}`, i, i)
			}
			b.WriteString(`],"pagination":{"next":1700}}`)
			_, _ = w.Write([]byte(b.String()))
		case "1700":
			_, _ = w.Write([]byte(`{"projects":[{
				"id":"prj_last","name":"web","framework":"nextjs","rootDirectory":"apps/web/","nodeVersion":"18.x",
				"link":{"type":"github","org":"Acme","repo":"Web","productionBranch":"main"},
				"targets":{"production":{"url":"web-abc.vercel.app","alias":["web.example.com","web-abc.vercel.app"]}}
			}],"pagination":{"next":0}}`))
		default:
			t.Errorf("unexpected until %s", r.URL.Query().Get("until"))
		}
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}

	resources, err := p.ListResources(context.Background(), cred)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Len(t, resources, 101)

	last := resources[100]
	assert.Equal(t, domain.CloudVercel, last.Provider)
	assert.Equal(t, domain.CloudResourceVercelProject, last.Ref.Kind)
	assert.Equal(t, "prj_last", last.Ref.ID)
	assert.Equal(t, "web", last.Ref.Name)
	assert.Equal(t, "team_1", last.Ref.Extra["team_id"])
	assert.Equal(t, "https://web.example.com", last.URL)
	assert.ElementsMatch(t, []string{"web.example.com", "web-abc.vercel.app"}, last.Domains)
	assert.Equal(t, "nextjs", last.Labels["framework"])
	assert.Equal(t, "apps/web", last.Labels["root_directory"])
	assert.Equal(t, "18.x", last.Labels["node_version"])
	assert.Equal(t, "github", last.Labels["git_provider"])
	assert.Equal(t, "acme/web", last.Labels["git_repo"])
}

func TestResourceMapsStatusFromLatestProductionDeployment(t *testing.T) {
	cases := []struct {
		readyState string
		want       domain.CloudResourceStatus
	}{
		{"READY", domain.CloudStatusHealthy},
		{"BUILDING", domain.CloudStatusDeploying},
		{"QUEUED", domain.CloudStatusDeploying},
		{"INITIALIZING", domain.CloudStatusDeploying},
		{"ERROR", domain.CloudStatusFailed},
		{"CANCELED", domain.CloudStatusUnknown},
		{"SOMETHING_ELSE", domain.CloudStatusUnknown},
	}
	for _, tt := range cases {
		t.Run(tt.readyState, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v9/projects/prj_1":
					_, _ = w.Write([]byte(`{"id":"prj_1","name":"web","framework":"nextjs","rootDirectory":"apps/web/","nodeVersion":"20.x","link":{"type":"github","org":"acme","repo":"web","productionBranch":"main"},"targets":{"production":{"url":"web-abc.vercel.app","alias":["web.example.com","web-abc.vercel.app"]}}}`))
				case "/v6/deployments":
					_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_x","readyState":"` + tt.readyState + `","target":"production"}]}`))
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			p := NewProvider(c)
			cred := domain.CloudCredential{
				Fields: map[string]string{"token": "tok"},
				Meta:   map[string]string{"team_slug": "acme-team"},
			}
			ref := domain.CloudResourceRef{ID: "prj_1"}

			detail, err := p.Resource(context.Background(), cred, ref)
			require.NoError(t, err)
			assert.Equal(t, tt.want, detail.Status)
			assert.Equal(t, "dpl_x", detail.Revision)
			assert.Equal(t, "https://vercel.com/acme-team/web", detail.ConsoleURL)
			require.NotNil(t, detail.LatestDeployment)
			assert.Equal(t, "dpl_x", detail.LatestDeployment.ID)

			wantFacts := []domain.KeyValue{
				{Label: "framework", Value: "nextjs"},
				{Label: "node_version", Value: "20.x"},
				{Label: "root_directory", Value: "apps/web"},
				{Label: "git_repo", Value: "acme/web"},
				{Label: "production_branch", Value: "main"},
			}
			assert.Equal(t, wantFacts, detail.Facts)
		})
	}
}

func TestResourceConsoleURLFallsBackToUsername(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9/projects/prj_1":
			_, _ = w.Write([]byte(`{"id":"prj_1","name":"solo"}`))
		case "/v6/deployments":
			_, _ = w.Write([]byte(`{"deployments":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}, Meta: map[string]string{"username": "akif"}}

	detail, err := p.Resource(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"})
	require.NoError(t, err)
	assert.Equal(t, "https://vercel.com/akif/solo", detail.ConsoleURL)
	assert.Equal(t, domain.CloudStatusUnknown, detail.Status)
	assert.Nil(t, detail.LatestDeployment)
}

func TestResourceNotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Project not found"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	_, err := p.Resource(context.Background(), cred, domain.CloudResourceRef{ID: "prj_missing"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestRefTeamIDOverridesCredentialTeamID(t *testing.T) {
	var gotQueries []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueries = append(gotQueries, r.URL.RawQuery)
		switch r.URL.Path {
		case "/v9/projects/prj_1":
			_, _ = w.Write([]byte(`{"id":"prj_1","name":"web"}`))
		case "/v6/deployments":
			_, _ = w.Write([]byte(`{"deployments":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_A"}}
	ref := domain.CloudResourceRef{ID: "prj_1", Extra: map[string]string{"team_id": "team_B"}}

	_, err := p.Resource(context.Background(), cred, ref)
	require.NoError(t, err)
	require.NotEmpty(t, gotQueries)
	for _, q := range gotQueries {
		assert.Contains(t, q, "teamId=team_B")
		assert.NotContains(t, q, "team_A")
	}
}

func TestDeploymentsMapsEnvironmentCommitAndCreator(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"deployments":[
			{
				"uid":"dpl_prod","url":"prod-abc.vercel.app","state":"READY","readyState":"READY",
				"target":"production","created":1000,"createdAt":1000,"ready":2000,
				"inspectorUrl":"https://vercel.com/x/y/prod",
				"meta":{"githubCommitSha":"abc123","githubCommitRef":"main","githubCommitMessage":"ship"},
				"creator":{"username":"akif","email":"a@b.c"}
			},
			{
				"uid":"dpl_preview","url":"preview-xyz.vercel.app","state":"ERROR","readyState":"ERROR",
				"target":null,"created":500,"createdAt":500,
				"meta":{"gitlabCommitSha":"def456","gitlabCommitRef":"feature","gitlabCommitMessage":"wip"},
				"creator":{"email":"only@email.com"}
			}
		]}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}
	ref := domain.CloudResourceRef{ID: "prj_1"}

	deployments, err := p.Deployments(context.Background(), cred, ref, 5)
	require.NoError(t, err)
	require.Len(t, deployments, 2)

	prod := deployments[0]
	assert.Equal(t, "dpl_prod", prod.ID)
	assert.Equal(t, domain.CloudDeployReady, prod.Status)
	assert.Equal(t, domain.EnvironmentProduction, prod.Environment)
	assert.Equal(t, "abc123", prod.CommitSHA)
	assert.Equal(t, "main", prod.Branch)
	assert.Equal(t, "ship", prod.CommitMessage)
	assert.Equal(t, "https://prod-abc.vercel.app", prod.URL)
	assert.Equal(t, "https://vercel.com/x/y/prod", prod.InspectURL)
	assert.Equal(t, "akif", prod.Creator)
	require.NotNil(t, prod.ReadyAt)
	assert.Equal(t, time.UnixMilli(2000).UTC(), *prod.ReadyAt)

	preview := deployments[1]
	assert.Equal(t, domain.CloudDeployError, preview.Status)
	assert.Equal(t, domain.EnvironmentPreview, preview.Environment)
	assert.Equal(t, "def456", preview.CommitSHA)
	assert.Equal(t, "feature", preview.Branch)
	assert.Equal(t, "only@email.com", preview.Creator)
	assert.Nil(t, preview.ReadyAt)
	assert.Empty(t, preview.InspectURL)

	assert.Contains(t, gotQuery, "teamId=team_1")
	assert.Contains(t, gotQuery, "projectId=prj_1")
	assert.Contains(t, gotQuery, "limit=5")
	assert.NotContains(t, gotQuery, "target=")
}

func TestDeploymentsRateLimited(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"Too many requests"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	_, err := p.Deployments(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, 5)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "rate limit")
	assert.False(t, errors.Is(err, port.ErrCloudAuth))
}

func TestLogsFiltersAndSortsNewestFirst(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"rowId":"r1","timestampInMs":1000,"level":"info","message":"startup"}`,
		`{"rowId":"r2","timestampInMs":2000,"level":"warning","message":"slow request","requestMethod":"GET","requestPath":"/api","responseStatusCode":200,"domain":"example.com"}`,
		`{"rowId":"r3","timestampInMs":3000,"level":"error","message":"boom failed"}`,
		`{"rowId":"r4","timestampInMs":500,"level":"debug","message":"trace"}`,
	}, "\n")

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_1","readyState":"READY","target":"production"}]}`))
		case strings.Contains(r.URL.Path, "runtime-logs"):
			_, _ = w.Write([]byte(ndjson))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}
	ref := domain.CloudResourceRef{ID: "prj_1"}

	page, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{})
	require.NoError(t, err)
	require.Len(t, page.Entries, 4)
	assert.False(t, page.Truncated)
	assert.Equal(t, []string{"r3", "r2", "r1", "r4"}, rowIDs(page.Entries))
	assert.Equal(t, domain.LogWarning, page.Entries[1].Severity)
	assert.Equal(t, "GET", page.Entries[1].Method)
	assert.Equal(t, "/api", page.Entries[1].Path)
	assert.Equal(t, 200, page.Entries[1].StatusCode)
	assert.Equal(t, "example.com", page.Entries[1].Fields["domain"])
	assert.Equal(t, "r2", page.Entries[1].Fields["rowId"])

	warnPage, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{MinSeverity: domain.LogWarning})
	require.NoError(t, err)
	assert.Equal(t, []string{"r3", "r2"}, rowIDs(warnPage.Entries))

	textPage, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{Text: "BOOM"})
	require.NoError(t, err)
	assert.Equal(t, []string{"r3"}, rowIDs(textPage.Entries))

	sincePage, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{Since: time.UnixMilli(1500)})
	require.NoError(t, err)
	assert.Equal(t, []string{"r3", "r2"}, rowIDs(sincePage.Entries))
}

func rowIDs(entries []domain.RuntimeLogEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.Fields["rowId"]
	}
	return ids
}

func TestLogsUsesExplicitDeploymentIDWithoutResolving(t *testing.T) {
	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deployments") {
			t.Fatal("must not resolve a deployment when Extra[deployment_id] is set")
		}
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"rowId":"r1","timestampInMs":1,"level":"info","message":"hi"}` + "\n"))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}
	ref := domain.CloudResourceRef{ID: "prj_1", Extra: map[string]string{"deployment_id": "dpl_specific"}}

	page, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	assert.Contains(t, gotPath, "/dpl_specific/")
}

func TestLogsNoReadyDeploymentReturnsEmptyPage(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "runtime-logs") {
			t.Fatal("must not fetch logs when no deployment resolves")
		}
		_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_building","readyState":"BUILDING","target":"production"}]}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}
	ref := domain.CloudResourceRef{ID: "prj_1"}

	page, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{})
	require.NoError(t, err)
	assert.Empty(t, page.Entries)
	assert.False(t, page.Truncated)
}

func TestLogsStreamNeverClosesDeadlineTruncates(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deployments") {
			_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_1","readyState":"READY","target":"production"}]}`))
			return
		}
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte(`{"rowId":"r","timestampInMs":1,"level":"info","message":"hi"}` + "\n"))
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}
	ref := domain.CloudResourceRef{ID: "prj_1"}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	page, err := p.Logs(ctx, cred, ref, domain.RuntimeLogQuery{})
	require.NoError(t, err)
	assert.True(t, page.Truncated)
	assert.NotEmpty(t, page.Entries)
}

func TestLogsLineCapTruncates(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&body, `{"rowId":"r%d","timestampInMs":%d,"level":"info","message":"m"}`+"\n", i, i*1000)
	}
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deployments") {
			_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_1","readyState":"READY","target":"production"}]}`))
			return
		}
		_, _ = w.Write([]byte(body.String()))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}
	ref := domain.CloudResourceRef{ID: "prj_1"}

	page, err := p.Logs(context.Background(), cred, ref, domain.RuntimeLogQuery{Limit: 3})
	require.NoError(t, err)
	require.Len(t, page.Entries, 3)
	assert.True(t, page.Truncated)
}

func TestBuildLogsMapsStdoutStderrToInfoError(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[
			{"type":"command","created":1000,"text":"npm run build"},
			{"type":"stdout","created":2000,"payload":{"text":"compiling..."}},
			{"type":"stderr","created":3000,"text":"build failed"}
		]`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}

	entries, err := p.BuildLogs(context.Background(), cred, "dpl_1", 50)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, domain.LogInfo, entries[0].Severity)
	assert.Equal(t, "npm run build", entries[0].Message)
	assert.Equal(t, domain.LogInfo, entries[1].Severity)
	assert.Equal(t, "compiling...", entries[1].Message)
	assert.Equal(t, domain.LogError, entries[2].Severity)
	assert.Equal(t, "build failed", entries[2].Message)
	assert.Contains(t, gotQuery, "teamId=team_1")
	assert.Contains(t, gotQuery, "limit=50")
}

func TestErrorsIsUnsupported(t *testing.T) {
	p := NewProvider(New())
	_, err := p.Errors(context.Background(), domain.CloudCredential{}, domain.CloudResourceRef{}, time.Time{})
	assert.ErrorIs(t, err, port.ErrUnsupported)
}
