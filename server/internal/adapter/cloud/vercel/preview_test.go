package vercel

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const branchDeployments = `{"deployments":[
	{"uid":"dpl_prod","url":"web-prod.vercel.app","readyState":"READY","target":"production","createdAt":4000,
	 "meta":{"githubCommitSha":"ccccccc111","githubCommitRef":"feature/login"}},
	{"uid":"dpl_new","url":"web-new.vercel.app","readyState":"BUILDING","target":null,"createdAt":3000,
	 "meta":{"githubCommitSha":"bbbbbbb222","githubCommitRef":"feature/login","githubPrId":"42"}},
	{"uid":"dpl_old","url":"web-old.vercel.app","readyState":"READY","target":null,"createdAt":2000,"ready":2500,
	 "inspectorUrl":"https://vercel.com/acme/web/dpl_old",
	 "meta":{"githubCommitSha":"aaaaaaa333","githubCommitRef":"feature/login","githubPrId":"42"}}
]}`

func previewServer(t *testing.T, queries map[string]url.Values) *Client {
	return newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries[r.URL.Path] = r.URL.Query()
		switch r.URL.Path {
		case "/v7/deployments":
			_, _ = w.Write([]byte(branchDeployments))
		case "/v13/deployments/dpl_old", "/v13/deployments/dpl_new":
			_, _ = w.Write([]byte(`{"alias":["web-abc123-acme.vercel.app","web-git-feature-login-acme.vercel.app"]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
}

func TestPreviewQueriesTheBranchAndPrefersTheHeadCommit(t *testing.T) {
	queries := map[string]url.Values{}
	p := NewProvider(previewServer(t, queries))
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}

	d, ok, err := p.Preview(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "feature/login", "aaaaaaa333ffff")
	require.NoError(t, err)
	require.True(t, ok)

	q := queries["/v7/deployments"]
	assert.Equal(t, "prj_1", q.Get("projectId"))
	assert.Equal(t, "feature/login", q.Get("branch"))
	assert.Equal(t, "team_1", q.Get("teamId"))
	assert.Equal(t, "team_1", queries["/v13/deployments/dpl_old"].Get("teamId"))

	assert.Equal(t, "dpl_old", d.ID)
	assert.Equal(t, domain.CloudDeployReady, d.Status)
	assert.Equal(t, 42, d.PRNumber)
	assert.Equal(t, "https://web-old.vercel.app", d.URL)
	assert.Equal(t, "https://web-git-feature-login-acme.vercel.app", d.BranchURL)
	assert.Equal(t, "https://vercel.com/acme/web/dpl_old", d.InspectURL)
}

func TestPreviewWithoutAHeadCommitIsTheNewestPreviewNeverProduction(t *testing.T) {
	queries := map[string]url.Values{}
	p := NewProvider(previewServer(t, queries))
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	d, ok, err := p.Preview(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "feature/login", "")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "dpl_new", d.ID)
	assert.Equal(t, domain.CloudDeployBuilding, d.Status)
	assert.Equal(t, domain.EnvironmentPreview, d.Environment)
}

func TestPreviewBranchWithNoDeployment(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"deployments":[]}`))
	}))
	p := NewProvider(c)

	_, ok, err := p.Preview(context.Background(), domain.CloudCredential{Fields: map[string]string{"token": "tok"}}, domain.CloudResourceRef{ID: "prj_1"}, "feature/none", "")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPreviewAccessMapsProtectionAndAutomationBypass(t *testing.T) {
	cases := []struct {
		name    string
		project string
		want    domain.PreviewAccess
	}{
		{"unprotected", `{"id":"prj_1","ssoProtection":null,"passwordProtection":null}`,
			domain.PreviewAccess{Mode: domain.PreviewAccessNone}},
		{"vercel authentication", `{"ssoProtection":{"deploymentType":"preview"}}`,
			domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessVercelAuth}},
		{"password", `{"passwordProtection":{"deploymentType":"all"}}`,
			domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessPassword}},
		{"both, with an automation bypass", `{"ssoProtection":{"deploymentType":"all_except_custom_domains"},"passwordProtection":{"deploymentType":"preview"},
			"protectionBypass":{"shareable":{"scope":"shareable-link"},"s3cret":{"scope":"automation-bypass","createdBy":"u1"}}}`,
			domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessVercelAuthAndPassword, BypassConfigured: true, BypassSecret: "s3cret"}},
		{"a shareable link is not an automation bypass", `{"ssoProtection":{"deploymentType":"preview"},"protectionBypass":{"shareable":{"scope":"shareable-link"}}}`,
			domain.PreviewAccess{Protected: true, Mode: domain.PreviewAccessVercelAuth}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(tc.project))
			}))
			access, err := NewProvider(c).PreviewAccess(context.Background(), domain.CloudCredential{Fields: map[string]string{"token": "tok"}}, domain.CloudResourceRef{ID: "prj_1"})
			require.NoError(t, err)
			assert.Equal(t, "/v9/projects/prj_1", gotPath)
			assert.Equal(t, tc.want, access)
		})
	}
}

func TestDeploymentsAreFilteredToTheEnvironment(t *testing.T) {
	const listing = `{"deployments":[
		{"uid":"dpl_prod","readyState":"READY","target":"production","createdAt":3},
		{"uid":"dpl_preview","readyState":"READY","target":null,"createdAt":2},
		{"uid":"dpl_staging","readyState":"READY","target":"staging","createdAt":1}
	]}`
	cases := []struct {
		env        domain.DeployEnvironment
		wantTarget string
		wantIDs    []string
	}{
		{domain.EnvironmentProduction, "production", []string{"dpl_prod"}},
		{domain.EnvironmentPreview, "preview", []string{"dpl_preview"}},
		{domain.EnvironmentStaging, "", []string{"dpl_preview", "dpl_staging"}},
		{"", "", []string{"dpl_prod", "dpl_preview", "dpl_staging"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.env), func(t *testing.T) {
			var gotQuery url.Values
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query()
				_, _ = w.Write([]byte(listing))
			}))
			deployments, err := NewProvider(c).Deployments(context.Background(), domain.CloudCredential{Fields: map[string]string{"token": "tok"}}, domain.CloudResourceRef{ID: "prj_1"}, tc.env, 10)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTarget, gotQuery.Get("target"))
			ids := make([]string, 0, len(deployments))
			for _, d := range deployments {
				ids = append(ids, d.ID)
			}
			assert.Equal(t, tc.wantIDs, ids)
		})
	}
}
