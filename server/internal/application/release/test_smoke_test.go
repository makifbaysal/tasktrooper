package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestTestSmokeResolvesTheConfirmedProdEnvironmentAndRunsRelativeAndAbsoluteChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	components := newFakeComponents()
	envs := newFakeEnvironments()
	repositoryID := uuid.New()
	comp := components.add(repositoryID, domain.Component{ID: uuid.New(), Path: "web"})
	envs.envs[repositoryID] = append(envs.envs[repositoryID], domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: comp.ID,
		Environment: domain.EnvironmentProduction, Status: domain.LinkConfirmed, URL: server.URL,
	})

	svc := New(Deps{Store: newFakeReleaseStore(), Components: components, Environments: envs})
	svc.SetURLPolicy(urlguard.Policy{Schemes: []string{"http", "https"}, AllowLoopback: true, MaxRedirects: 3})

	checks := []domain.SmokeCheck{
		{Method: "GET", Path: "/relative"},
		{Method: "GET", Path: server.URL + "/absolute"},
	}
	baseURL, results, err := svc.TestSmoke(context.Background(), comp.ID, checks)
	require.NoError(t, err)
	assert.Equal(t, server.URL, baseURL)
	require.Len(t, results, 2)
	assert.True(t, results[0].OK)
	assert.True(t, results[1].OK)
}

func TestTestSmokeReportsNoBaseURLForARelativeCheckWhenNothingIsBound(t *testing.T) {
	components := newFakeComponents()
	repositoryID := uuid.New()
	comp := components.add(repositoryID, domain.Component{ID: uuid.New(), Path: "web"})

	svc := New(Deps{Store: newFakeReleaseStore(), Components: components})

	baseURL, results, err := svc.TestSmoke(context.Background(), comp.ID, []domain.SmokeCheck{{Method: "GET", Path: "/relative"}})
	require.NoError(t, err)
	assert.Empty(t, baseURL)
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Error, "no base URL")
}

func TestTestSmokeReturnsNotFoundForAnUnknownComponent(t *testing.T) {
	svc := New(Deps{Store: newFakeReleaseStore(), Components: newFakeComponents()})

	_, _, err := svc.TestSmoke(context.Background(), uuid.New(), []domain.SmokeCheck{{Method: "GET", Path: "/x"}})
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestTestSmokeWithoutComponentsDependencyIsNotFound(t *testing.T) {
	svc := New(Deps{Store: newFakeReleaseStore()})

	_, _, err := svc.TestSmoke(context.Background(), uuid.New(), []domain.SmokeCheck{{Method: "GET", Path: "/x"}})
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestTestSmokeRejectsAnEmptyChecksList(t *testing.T) {
	svc := New(Deps{Store: newFakeReleaseStore(), Components: newFakeComponents()})

	_, _, err := svc.TestSmoke(context.Background(), uuid.New(), nil)
	assert.ErrorIs(t, err, domain.ErrInvalidDelivery)
}

func TestProductionBaseURLResolvesTheConfirmedProdEnvironment(t *testing.T) {
	components := newFakeComponents()
	envs := newFakeEnvironments()
	repositoryID := uuid.New()
	comp := components.add(repositoryID, domain.Component{ID: uuid.New(), Path: "web"})
	envs.envs[repositoryID] = append(envs.envs[repositoryID], domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: repositoryID, ComponentID: comp.ID,
		Environment: domain.EnvironmentProduction, Status: domain.LinkConfirmed, URL: "https://example.com",
	})

	svc := New(Deps{Store: newFakeReleaseStore(), Components: components, Environments: envs})

	baseURL, err := svc.ProductionBaseURL(context.Background(), comp.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", baseURL)
}

func TestProductionBaseURLIsEmptyWithNothingBound(t *testing.T) {
	components := newFakeComponents()
	repositoryID := uuid.New()
	comp := components.add(repositoryID, domain.Component{ID: uuid.New(), Path: "web"})

	svc := New(Deps{Store: newFakeReleaseStore(), Components: components})

	baseURL, err := svc.ProductionBaseURL(context.Background(), comp.ID)
	require.NoError(t, err)
	assert.Empty(t, baseURL)
}

func TestProductionBaseURLIsNotFoundForAnUnknownComponent(t *testing.T) {
	svc := New(Deps{Store: newFakeReleaseStore(), Components: newFakeComponents()})

	_, err := svc.ProductionBaseURL(context.Background(), uuid.New())
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestProductionBaseURLWithoutComponentsDependencyIsNotFound(t *testing.T) {
	svc := New(Deps{Store: newFakeReleaseStore()})

	_, err := svc.ProductionBaseURL(context.Background(), uuid.New())
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestTestSmokeRejectsAnInvalidCheck(t *testing.T) {
	components := newFakeComponents()
	repositoryID := uuid.New()
	comp := components.add(repositoryID, domain.Component{ID: uuid.New(), Path: "web"})
	svc := New(Deps{Store: newFakeReleaseStore(), Components: components})

	_, _, err := svc.TestSmoke(context.Background(), comp.ID, []domain.SmokeCheck{{Method: "POST", Path: "/x"}})
	assert.ErrorIs(t, err, domain.ErrInvalidDelivery)
}
