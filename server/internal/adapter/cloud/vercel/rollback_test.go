// Endpoints verified live against Vercel's REST API reference on 2026-09-25:
//   - https://vercel.com/docs/rest-api/projects/point-production-traffic-to-a-previous-production-deployment-by-id
//     (POST /v1/projects/{projectId}/rollback/{deploymentId})
//   - https://vercel.com/docs/rest-api/projects/point-production-traffic-to-a-given-deployment
//     (POST /v10/projects/{projectId}/promote/{deploymentId})
//   - https://vercel.com/docs/instant-rollback#undo-a-rollback (automatic
//     production-domain assignment is turned off by a rollback and only a
//     promote turns it back on)
package vercel

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestRollbackToCallsTheDocumentedRoute(t *testing.T) {
	var gotMethod, gotPath, gotTeam string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotTeam = r.URL.Query().Get("teamId")
		w.WriteHeader(http.StatusCreated)
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1"}

	err := p.RollbackTo(context.Background(), cred, ref, "dpl_prev")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/projects/prj_1/rollback/dpl_prev", gotPath)
	assert.Equal(t, "team_1", gotTeam)
}

func TestRollbackToRefExtraTeamOverridesCredential(t *testing.T) {
	var gotTeam string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTeam = r.URL.Query().Get("teamId")
		w.WriteHeader(http.StatusCreated)
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "account_team"}}
	ref := domain.CloudResourceRef{ID: "prj_1", Extra: map[string]string{"team_id": "listed_team"}}

	require.NoError(t, p.RollbackTo(context.Background(), cred, ref, "dpl_prev"))
	assert.Equal(t, "listed_team", gotTeam)
}

func TestRollbackToRequiresADeploymentID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not call the API without a deployment id")
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	err := p.RollbackTo(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "")
	require.Error(t, err)
}

func TestRollbackToUnauthorizedIsCloudAuth(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid token"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "bad"}}

	err := p.RollbackTo(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "dpl_prev")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrCloudAuth)
}

func TestRollbackToForbiddenIsWriteDenied(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"read-only token"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "readonly"}}

	err := p.RollbackTo(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "dpl_prev")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrCloudWriteDenied)
	assert.Contains(t, err.Error(), "write scope")
}

func TestPromoteCallsTheDocumentedRoute(t *testing.T) {
	var gotMethod, gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	err := p.Promote(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "dpl_new")
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v10/projects/prj_1/promote/dpl_new", gotPath)
}

func TestPromoteForbiddenIsWriteDenied(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"read-only token"}}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "readonly"}}

	err := p.Promote(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"}, "dpl_new")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrCloudWriteDenied)
}

func TestCurrentReturnsTheNewestProductionDeployment(t *testing.T) {
	var gotTarget string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.URL.Query().Get("target")
		_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_current","readyState":"READY","target":"production","createdAt":1700000000000}]}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	d, err := p.Current(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1"})
	require.NoError(t, err)
	assert.Equal(t, "production", gotTarget)
	assert.Equal(t, "dpl_current", d.ID)
	assert.Equal(t, domain.CloudDeployReady, d.Status)
}

func TestCurrentNoProductionDeploymentIsNotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"deployments":[]}`))
	}))
	p := NewProvider(c)
	cred := domain.CloudCredential{Fields: map[string]string{"token": "tok"}}

	_, err := p.Current(context.Background(), cred, domain.CloudResourceRef{ID: "prj_1", Name: "web"})
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestProviderSatisfiesCloudRollbacker(t *testing.T) {
	var _ port.CloudRollbacker = NewProvider(New())
}
