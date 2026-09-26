package release

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestRunSmokeAgainstTheStoredTargetAppendsResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	svc.SetURLPolicy(urlguard.Policy{Schemes: []string{"http", "https"}, AllowLoopback: true, MaxRedirects: 3})

	repositoryID := uuid.New()
	r := domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying,
		Profile: domain.ComponentDelivery{Verify: domain.DeliveryVerify{Smoke: []domain.SmokeCheck{{Method: "GET", Path: "/ok"}}}},
		Checks:  domain.ReleaseChecks{BaseURL: server.URL},
	}
	created, err := store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	results, err := svc.RunSmoke(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].OK)

	// an on-demand run must not write the release — that would race the
	// sweeper's own Update of the same release.
	got, err := store.Get(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Checks.Smoke)
}

func TestRunSmokeWithoutABaseURLReportsEachRelativeCheckSkipped(t *testing.T) {
	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})

	repositoryID := uuid.New()
	r := domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying,
		Profile: domain.ComponentDelivery{Verify: domain.DeliveryVerify{Smoke: []domain.SmokeCheck{{Method: "GET", Path: "/ok"}}}},
	}
	created, err := store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	results, err := svc.RunSmoke(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Error, "no base URL")
}

func TestRunSmokeFailsAResponseSlowerThanItsLatencyBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	svc.SetURLPolicy(urlguard.Policy{Schemes: []string{"http", "https"}, AllowLoopback: true, MaxRedirects: 3})

	repositoryID := uuid.New()
	r := domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying,
		Profile: domain.ComponentDelivery{Verify: domain.DeliveryVerify{Smoke: []domain.SmokeCheck{{Method: "GET", Path: "/slow", MaxLatencyMS: 5}}}},
		Checks:  domain.ReleaseChecks{BaseURL: server.URL},
	}
	created, err := store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	results, err := svc.RunSmoke(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Error, "slower than 5 ms")
}

func TestRunSmokeKeepsTheContainsFailureReasonOverALatencyBudgetMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	store := newFakeReleaseStore()
	svc := New(Deps{Store: store})
	svc.SetURLPolicy(urlguard.Policy{Schemes: []string{"http", "https"}, AllowLoopback: true, MaxRedirects: 3})

	repositoryID := uuid.New()
	r := domain.Release{
		RepositoryID: repositoryID, Status: domain.ReleaseVerifying,
		Profile: domain.ComponentDelivery{Verify: domain.DeliveryVerify{Smoke: []domain.SmokeCheck{
			{Method: "GET", Path: "/slow", Contains: "expected", MaxLatencyMS: 5},
		}}},
		Checks: domain.ReleaseChecks{BaseURL: server.URL},
	}
	created, err := store.Create(context.Background(), r, nil)
	require.NoError(t, err)

	results, err := svc.RunSmoke(context.Background(), created.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	assert.Equal(t, "response did not contain the expected text", results[0].Error)
}
