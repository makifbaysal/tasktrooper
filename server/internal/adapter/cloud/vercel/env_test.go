package vercel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var envTestCred = domain.CloudCredential{Fields: map[string]string{"token": "tok", "team_id": "team_1"}}

var envTestRef = domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: "prj_1", Name: "site"}

func TestListEnvVarsMergesTargetsAndSkipsBranchScopedOnes(t *testing.T) {
	var gotPath, gotTeam string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotTeam = r.URL.Path, r.URL.Query().Get("teamId")
		_, _ = w.Write([]byte(`{"envs":[
			{"key":"SESSION_SECRET","target":["production","preview"],"type":"sensitive","value":""},
			{"key":"GITHUB_REPO","target":"production","type":"encrypted","value":"x"},
			{"key":"GITHUB_REPO","target":["preview"],"type":"encrypted","value":"x"},
			{"key":"FEATURE","target":["preview"],"gitBranch":"feat-1","type":"plain","value":"on"}
		]}`))
	}))

	vars, err := NewProvider(c).ListEnvVars(context.Background(), envTestCred, envTestRef)

	require.NoError(t, err)
	assert.Equal(t, "/v10/projects/prj_1/env", gotPath)
	assert.Equal(t, "team_1", gotTeam)
	require.Len(t, vars, 2)
	assert.Equal(t, "GITHUB_REPO", vars[0].Key)
	assert.True(t, vars[0].SetFor(domain.EnvironmentProduction))
	assert.True(t, vars[0].SetFor(domain.EnvironmentPreview))
	assert.Equal(t, "SESSION_SECRET", vars[1].Key)
}

func TestListEnvVarsRefusesWhenProductionVariablesAreHidden(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"envs":[],"hiddenProductionEnvCount":2}`))
	}))

	_, err := NewProvider(c).ListEnvVars(context.Background(), envTestCred, envTestRef)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot see 2")
	assert.NotErrorIs(t, err, port.ErrCloudAuth)
}

func TestUpsertEnvVarsSendsOneUpsertWithTypesAndTargets(t *testing.T) {
	var gotQuery string
	var body []map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v10/projects/prj_1/env", r.URL.Path)
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":[],"failed":[]}`))
	}))

	err := NewProvider(c).UpsertEnvVars(context.Background(), envTestCred, envTestRef, []domain.CloudEnvWrite{
		{Key: "SESSION_SECRET", Value: "s3cret", Sensitive: true, Targets: domain.EnvTargets},
		{Key: "GITHUB_REPO", Value: "o/r", Targets: []domain.DeployEnvironment{domain.EnvironmentPreview}},
	})

	require.NoError(t, err)
	assert.Contains(t, gotQuery, "upsert=true")
	assert.Contains(t, gotQuery, "teamId=team_1")
	require.Len(t, body, 2)
	assert.Equal(t, "sensitive", body[0]["type"])
	assert.Equal(t, []any{"production", "preview"}, body[0]["target"])
	assert.Equal(t, "encrypted", body[1]["type"])
	assert.Equal(t, []any{"preview"}, body[1]["target"])
}

func TestUpsertEnvVarsReportsFailuresWithoutTheirValues(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"failed":[{"error":{"code":"bad","message":"invalid value","key":"GITHUB_TOKEN","value":"ghp_secret"}}]}`))
	}))

	err := NewProvider(c).UpsertEnvVars(context.Background(), envTestCred, envTestRef, []domain.CloudEnvWrite{
		{Key: "GITHUB_TOKEN", Value: "ghp_secret", Sensitive: true, Targets: domain.EnvTargets},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "GITHUB_TOKEN: invalid value")
	assert.NotContains(t, err.Error(), "ghp_secret")
}

func TestUpsertEnvVarsMapsForbiddenToWriteDenied(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))

	err := NewProvider(c).UpsertEnvVars(context.Background(), envTestCred, envTestRef, []domain.CloudEnvWrite{
		{Key: "A", Value: "b", Targets: domain.EnvTargets},
	})

	assert.ErrorIs(t, err, port.ErrCloudWriteDenied)
}

func TestRedeployRebuildsTheLiveProductionDeploymentFromTheLatestCommit(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v6/deployments":
			_, _ = w.Write([]byte(`{"deployments":[{"uid":"dpl_live","readySubstate":"PROMOTED","readyState":"READY","target":"production"}]}`))
		case "/v13/deployments":
			raw, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(raw, &body))
			_, _ = w.Write([]byte(`{"id":"dpl_new","url":"site-abc.vercel.app","readyState":"QUEUED"}`))
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
	}))

	d, err := NewProvider(c).Redeploy(context.Background(), envTestCred, envTestRef)

	require.NoError(t, err)
	assert.Equal(t, "dpl_new", d.ID)
	assert.Equal(t, "https://site-abc.vercel.app", d.URL)
	assert.Equal(t, domain.CloudDeployBuilding, d.Status)
	assert.Equal(t, "dpl_live", body["deploymentId"])
	assert.Equal(t, "production", body["target"])
	assert.Equal(t, true, body["withLatestCommit"])
	assert.Equal(t, "prj_1", body["project"])
}
