package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type markerComponents map[uuid.UUID]domain.Component

func (m markerComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	return m[id], nil
}

type markerRepos map[uuid.UUID]domain.Repository

func (m markerRepos) Get(_ context.Context, id uuid.UUID) (domain.Repository, error) {
	return m[id], nil
}

func markerFixture(services string) (*DeployMarker, *fakeClient, domain.Release) {
	svc, client := configured()
	client.respond = func(c call) (port.TelemetryResponse, error) {
		if c.method == "GET" && c.path == "services" {
			return port.TelemetryResponse{Status: 200, Body: []byte(services)}, nil
		}
		return port.TelemetryResponse{Status: 201, Body: []byte(`[]`)}, nil
	}
	repoID, compID := uuid.New(), uuid.New()
	comp := domain.Component{ID: compID, RepositoryID: repoID, Path: "services/orders", Name: domain.Detected("Orders API", "")}
	deployed := time.UnixMilli(1_700_000_000_000)
	r := domain.Release{
		ID: uuid.New(), RepositoryID: repoID, ComponentID: &compID, Version: "v1.4.0", CommitSHA: "abc123",
		DeployedAt: &deployed, Tasks: []domain.ReleaseTaskRef{{Key: "TT-7"}},
	}
	m := NewDeployMarker(svc, markerComponents{compID: comp}, markerRepos{repoID: {ID: repoID, Name: "shop"}})
	return m, client, r
}

func sentMarker(t *testing.T, client *fakeClient) DeployInput {
	t.Helper()
	for _, c := range client.calls {
		if c.method == http.MethodPost && c.path == "deploys" {
			var in DeployInput
			require.NoError(t, json.Unmarshal([]byte(c.body), &in))
			return in
		}
	}
	t.Fatal("no deploy marker was posted")
	return DeployInput{}
}

func TestDeployMarkerNamesTheServiceTelemetryKnows(t *testing.T) {
	m, client, r := markerFixture(`[{"name":"orders-api"},{"name":"shop"}]`)
	m.MarkDeployed(context.Background(), r)
	in := sentMarker(t, client)
	assert.Equal(t, "orders-api", in.Service)
	assert.Equal(t, "production", in.Environment)
	assert.Equal(t, "abc123", in.Version)
	assert.Equal(t, "abc123", in.CommitSHA)
	assert.Equal(t, int64(1_700_000_000_000), in.TS)
	assert.Equal(t, "TaskTrooper release v1.4.0: TT-7", in.Message)
}

func TestDeployMarkerFallsBackToTheRepositoryThenTheComponentName(t *testing.T) {
	m, client, r := markerFixture(`[{"name":"shop"}]`)
	m.MarkDeployed(context.Background(), r)
	assert.Equal(t, "shop", sentMarker(t, client).Service)

	m, client, r = markerFixture(`[]`)
	m.MarkDeployed(context.Background(), r)
	assert.Equal(t, "Orders API", sentMarker(t, client).Service)
}

func TestDeployMarkerIsSilentWithoutTelemetryOrAVersion(t *testing.T) {
	svc, _, client := newTestService()
	NewDeployMarker(svc, nil, nil).MarkDeployed(context.Background(), domain.Release{CommitSHA: "abc"})
	assert.Empty(t, client.calls)

	m, client2, r := markerFixture(`[]`)
	r.CommitSHA, r.Version = "", ""
	m.MarkDeployed(context.Background(), r)
	assert.Empty(t, client2.calls)
}

func TestDeployMarkerSwallowsAFailure(t *testing.T) {
	m, client, r := markerFixture(`[]`)
	client.respond = func(call) (port.TelemetryResponse, error) {
		return port.TelemetryResponse{}, port.ErrTelemetryUnreachable
	}
	assert.NotPanics(t, func() { m.MarkDeployed(context.Background(), r) })
}
