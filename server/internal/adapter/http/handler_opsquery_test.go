package http

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type opsQueryFixture struct {
	app     *fiber.App
	envs    *mocks.EnvironmentStore
	runtime *mocks.RuntimeReader
}

func newOpsQueryTestApp(t *testing.T) opsQueryFixture {
	t.Helper()
	envs := mocks.NewEnvironmentStore(t)
	runtime := mocks.NewRuntimeReader(t)
	h := &Handler{opsQuerySvc: opsquery.NewService(opsquery.Deps{Environments: envs, Runtime: runtime})}
	app := fiber.New()
	h.registerOpsQueryRoutes(app)
	return opsQueryFixture{app: app, envs: envs, runtime: runtime}
}

func opsEnv(kind domain.DeployEnvironment) domain.ComponentEnvironment {
	acct := uuid.New()
	return domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: uuid.New(), ComponentID: uuid.New(), Environment: kind,
		Provider: domain.CloudGCP, AccountID: &acct,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: uuid.NewString()},
		Status:   domain.LinkConfirmed,
	}
}

func getOps(t *testing.T, app *fiber.App, url string) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", url, nil), -1)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body), string(raw))
	return resp.StatusCode, body
}

func TestOperationsErrorsAnswersTheContractShapeAndDefaultsToProduction(t *testing.T) {
	fx := newOpsQueryTestApp(t)
	prod, staging := opsEnv(domain.EnvironmentProduction), opsEnv(domain.EnvironmentStaging)
	fx.envs.On("ListAllEnvironments", mock.Anything).Return([]domain.ComponentEnvironment{prod, staging}, nil).Once()
	fx.runtime.On("ErrorsSupported", mock.Anything).Return(true).Once()
	seen := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	fx.runtime.On("Errors", mock.Anything, prod.ID, mock.Anything).Return([]domain.RuntimeErrorGroup{{
		Fingerprint: "fp", Message: "boom", Count: 3, FirstSeen: seen, LastSeen: seen,
		Sample: "boom at x", Source: "api", New: true, ExternalURL: "https://console.cloud.google.com/errors/fp",
	}}, nil).Once()

	status, body := getOps(t, fx.app, "/v1/operations/errors")
	require.Equal(t, fiber.StatusOK, status)
	require.ElementsMatch(t, []string{"groups", "unavailable", "truncated"}, keysOf(body))
	require.Equal(t, false, body["truncated"])
	require.Equal(t, []any{}, body["unavailable"])

	groups := body["groups"].([]any)
	require.Len(t, groups, 1)
	g := groups[0].(map[string]any)
	require.ElementsMatch(t, []string{
		"component_id", "repository_id", "environment_id", "provider", "fingerprint", "message", "count",
		"first_seen", "last_seen", "sample", "source", "new", "external_url", "definition",
	}, keysOf(g))
	require.Nil(t, g["definition"])
	require.Equal(t, prod.ComponentID.String(), g["component_id"])
	require.Equal(t, prod.RepositoryID.String(), g["repository_id"])
	require.Equal(t, prod.ID.String(), g["environment_id"])
	require.Equal(t, "gcp", g["provider"])
}

func TestOperationsLogsAnswersTheContractShape(t *testing.T) {
	fx := newOpsQueryTestApp(t)
	prod := opsEnv(domain.EnvironmentProduction)
	fx.envs.On("ListAllEnvironments", mock.Anything).Return([]domain.ComponentEnvironment{prod}, nil).Once()
	fx.runtime.On("Logs", mock.Anything, prod.ID, mock.MatchedBy(func(q domain.RuntimeLogQuery) bool {
		return q.Text == "timeout" && q.MinSeverity == domain.LogError && q.Limit == 50
	})).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{{
		Timestamp: time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC), Severity: domain.LogError, Message: "upstream timeout",
		Source: "api", Method: "GET", Path: "/v1/x", StatusCode: 504, TraceID: "t1", Fields: map[string]string{"pod": "api-1"},
	}}}, nil).Once()

	status, body := getOps(t, fx.app, "/v1/operations/logs?env=production&min_severity=error&text=timeout&limit=50"+
		"&since=2026-10-16T10:00:00Z&until=2026-10-16T12:00:00Z")
	require.Equal(t, fiber.StatusOK, status)
	require.ElementsMatch(t, []string{"entries", "unavailable", "truncated"}, keysOf(body))

	entries := body["entries"].([]any)
	require.Len(t, entries, 1)
	e := entries[0].(map[string]any)
	require.ElementsMatch(t, []string{
		"timestamp", "severity", "message", "source", "method", "path", "status_code", "trace_id", "fields",
		"component_id", "environment_id",
	}, keysOf(e))
	require.Equal(t, prod.ComponentID.String(), e["component_id"])
	require.Equal(t, prod.ID.String(), e["environment_id"])
}

func TestOperationsReadsRepeatedOrCommaSeparatedComponentIDs(t *testing.T) {
	tests := []struct {
		name  string
		query func(a, b uuid.UUID) string
	}{
		{name: "repeated", query: func(a, b uuid.UUID) string { return "component_id=" + a.String() + "&component_id=" + b.String() }},
		{name: "comma-separated", query: func(a, b uuid.UUID) string { return "component_id=" + a.String() + "," + b.String() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newOpsQueryTestApp(t)
			a, b, other := opsEnv(domain.EnvironmentProduction), opsEnv(domain.EnvironmentProduction), opsEnv(domain.EnvironmentProduction)
			fx.envs.On("ListAllEnvironments", mock.Anything).Return([]domain.ComponentEnvironment{a, b, other}, nil).Once()
			fx.runtime.On("Logs", mock.Anything, a.ID, mock.Anything).Return(domain.RuntimeLogPage{}, nil).Once()
			fx.runtime.On("Logs", mock.Anything, b.ID, mock.Anything).Return(domain.RuntimeLogPage{}, nil).Once()

			status, _ := getOps(t, fx.app, "/v1/operations/logs?"+tt.query(a.ComponentID, b.ComponentID))
			require.Equal(t, fiber.StatusOK, status)
		})
	}
}

func TestOperationsRejectsBadInputWithoutReadingAnything(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "errors: unknown environment kind", url: "/v1/operations/errors?env=moonbase"},
		{name: "errors: since not RFC3339", url: "/v1/operations/errors?since=yesterday"},
		{name: "errors: component id not a uuid", url: "/v1/operations/errors?component_id=api"},
		{name: "logs: unknown environment kind", url: "/v1/operations/logs?env=qa"},
		{name: "logs: until not RFC3339", url: "/v1/operations/logs?until=now"},
		{name: "logs: unknown severity", url: "/v1/operations/logs?min_severity=catastrophic"},
		{name: "logs: limit not a number", url: "/v1/operations/logs?limit=lots"},
		{name: "logs: window ends before it starts", url: "/v1/operations/logs?since=2026-10-16T12:00:00Z&until=2026-10-16T11:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newOpsQueryTestApp(t)
			status, body := getOps(t, fx.app, tt.url)
			require.Equal(t, fiber.StatusBadRequest, status)
			require.NotEmpty(t, body["error"])
		})
	}
}

func TestOperationsLogsOverTooManyEnvironmentsAsksForComponents(t *testing.T) {
	fx := newOpsQueryTestApp(t)
	envs := make([]domain.ComponentEnvironment, opsquery.MaxLogEnvironments+1)
	for i := range envs {
		envs[i] = opsEnv(domain.EnvironmentProduction)
	}
	fx.envs.On("ListAllEnvironments", mock.Anything).Return(envs, nil).Once()

	status, body := getOps(t, fx.app, "/v1/operations/logs?env=production")
	require.Equal(t, fiber.StatusBadRequest, status)
	require.Equal(t, "too_many_environments", body["error"])
	require.Equal(t, "too_many_environments", body["code"])
	require.EqualValues(t, opsquery.MaxLogEnvironments+1, body["matched"])
	require.EqualValues(t, opsquery.MaxLogEnvironments, body["max_environments"])
}
