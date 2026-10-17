package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type tracesKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.TracesQuerier
	*mocks.LogsQuerier
}

type TracesHandlerSuite struct {
	suite.Suite
	traces    *mocks.TracesQuerier
	logs      *mocks.LogsQuerier
	bindings  *mocks.SignalBindingStore
	comps     *mocks.ComponentStore
	envs      *mocks.EnvironmentStore
	runtime   *mocks.RuntimeReader
	source    domain.DataSource
	component domain.Component
	now       time.Time
	app       *fiber.App
}

func TestTracesHandlerSuite(t *testing.T) { suite.Run(t, new(TracesHandlerSuite)) }

func (s *TracesHandlerSuite) SetupTest() {
	t := s.T()
	s.now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dsStore := mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	recipes.On("ListUserRecipes", mock.Anything).Return(nil, nil).Maybe()
	kind := mocks.NewDataSourceKindAdapter(t)
	kind.On("Kind").Return(domain.DataSourceKindTempo).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Tempo"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityTraces, domain.CapabilityLogs}).Maybe()
	s.traces, s.logs = mocks.NewTracesQuerier(t), mocks.NewLogsQuerier(t)
	s.bindings, s.comps, s.envs, s.runtime = mocks.NewSignalBindingStore(t), mocks.NewComponentStore(t), mocks.NewEnvironmentStore(t), mocks.NewRuntimeReader(t)

	s.source = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindTempo, Name: "tempo"}
	dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.source}, nil).Maybe()
	dsStore.On("DataSourceSecrets", mock.Anything, s.source.ID).Return(map[string]string{"token": "s3cret-token"}, nil).Maybe()
	s.component = domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: "api",
		Name: domain.Detected("api", domain.ConfidenceHigh), Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	s.comps.On("GetComponent", mock.Anything, s.component.ID).Return(s.component, nil).Maybe()
	s.comps.On("GetComponent", mock.Anything, mock.Anything).Return(domain.Component{}, port.ErrNotFound).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: dsStore, Recipes: recipes, URLPolicy: &policy,
		Kinds: []port.DataSourceKindAdapter{tracesKind{kind, s.traces, s.logs}},
	})
	s.Require().NoError(err)
	svc, err := signals.NewService(signals.Deps{Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs},
		signals.WithClock(func() time.Time { return s.now }), signals.WithQueryTimeout(2*time.Second))
	s.Require().NoError(err)
	ops := opsquery.NewService(opsquery.Deps{Environments: s.envs, Runtime: s.runtime}, opsquery.WithClock(func() time.Time { return s.now }))
	ops.SetBoundLogs(svc)
	s.app = fiber.New()
	h := &Handler{signalsSvc: svc, opsQuerySvc: ops}
	h.registerTraceRoutes(s.app)
	h.registerOpsQueryRoutes(s.app)
}

func (s *TracesHandlerSuite) get(path string) (int, map[string]any) {
	resp, err := s.app.Test(httptest.NewRequest("GET", path, nil), 10000)
	s.Require().NoError(err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	s.Require().NoError(json.Unmarshal(raw, &out), string(raw))
	return resp.StatusCode, out
}

func (s *TracesHandlerSuite) binding(signal, recipe string) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: s.source.ID, RecipeID: recipe, Params: map[string]string{"service": "api"}, Status: domain.SignalBindingConfirmed,
	}
}

func (s *TracesHandlerSuite) path(rest string) string {
	return "/v1/components/" + s.component.ID.String() + rest
}

func (s *TracesHandlerSuite) TestComponentTracesAnswersTheContractShape() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{s.binding(domain.SignalTracesSpans, "tempo-service")}, nil).Once()
	s.traces.On("SearchTraces", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.TraceQuery) bool {
		return q.Query == `{ resource.service.name = "api" }` && q.MinDuration == 250*time.Millisecond && q.Limit == 5 &&
			q.Since.Equal(time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)) && q.Until.Equal(s.now)
	})).Return([]domain.TraceSummary{{ID: "abc", RootService: "api", RootName: "GET /x", Start: s.now, DurationMS: 300, SpanCount: 4, Error: true}}, nil).Once()

	status, body := s.get(s.path("/traces?since=2026-10-17T10:00:00Z&min_duration_ms=250&limit=5"))

	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"component_id", "environment", "bound", "data_source_id", "traces", "error"}, keysOf(body))
	s.Equal(true, body["bound"])
	s.Equal(s.source.ID.String(), body["data_source_id"])
	s.Nil(body["error"])
	traces := body["traces"].([]any)
	s.Require().Len(traces, 1)
	s.ElementsMatch([]string{"id", "root_service", "root_name", "start", "duration_ms", "span_count", "error"}, keysOf(traces[0].(map[string]any)))
}

func (s *TracesHandlerSuite) TestComponentTracesRefusesBadParameters() {
	for _, q := range []string{"?since=yesterday", "?min_duration_ms=-1", "?min_duration_ms=86400001", "?min_duration_ms=9223372036854775807", "?limit=0", "?limit=x", "?env=prod"} {
		status, body := s.get(s.path("/traces" + q))
		s.Equal(fiber.StatusBadRequest, status, q)
		s.NotEmpty(body["error"], q)
	}
	status, _ := s.get("/v1/components/" + uuid.NewString() + "/traces")
	s.Equal(fiber.StatusNotFound, status)
}

func (s *TracesHandlerSuite) TestComponentTraceReadsOneTraceAndSaysWhyNot() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{s.binding(domain.SignalTracesSpans, "tempo-service")}, nil).Twice()
	s.traces.On("Trace", mock.Anything, mock.Anything, mock.Anything, "4bf92f3577b34da6a3ce929d0e0e4736").Return(domain.Trace{
		ID: "4bf92f3577b34da6a3ce929d0e0e4736", Spans: []domain.Span{{ID: "01", Service: "api", Name: "GET /x", Start: s.now, DurationMS: 2, Status: domain.SpanStatusOK}},
	}, nil).Once()
	s.traces.On("Trace", mock.Anything, mock.Anything, mock.Anything, "aa").Return(domain.Trace{}, fmt.Errorf("no trace: %w", port.ErrNotFound)).Once()

	status, body := s.get(s.path("/traces/4BF92F3577B34DA6A3CE929D0E0E4736"))
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"trace", "component_id", "data_source_id"}, keysOf(body))
	trace := body["trace"].(map[string]any)
	s.ElementsMatch([]string{"id", "spans", "truncated"}, keysOf(trace))
	span := trace["spans"].([]any)[0].(map[string]any)
	s.ElementsMatch([]string{"id", "service", "name", "start", "duration_ms", "status", "attributes"}, keysOf(span))

	status, _ = s.get(s.path("/traces/aa"))
	s.Equal(fiber.StatusNotFound, status)

	status, _ = s.get(s.path("/traces/not-hex"))
	s.Equal(fiber.StatusBadRequest, status)

	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentStaging).Return(nil, nil).Once()
	status, body = s.get(s.path("/traces/aa?env=staging"))
	s.Equal(fiber.StatusUnprocessableEntity, status)
	s.Equal("unbound", body["code"])
}

func (s *TracesHandlerSuite) TestOperationsTraceResolvesTheComponentsSourceOrLooksEverywhere() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{s.binding(domain.SignalTracesSpans, "tempo-service")}, nil).Once()
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{s.binding(domain.SignalTracesSpans, "tempo-service")}, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.component}, nil).Once()
	s.traces.On("Trace", mock.Anything, mock.Anything, mock.Anything, "abc").Return(domain.Trace{ID: "abc"}, nil).Twice()

	status, body := s.get("/v1/operations/traces/abc?component_id=" + s.component.ID.String())
	s.Require().Equal(fiber.StatusOK, status, body)
	s.Equal(s.component.ID.String(), body["component_id"])

	status, body = s.get("/v1/operations/traces/abc")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.Equal(s.source.ID.String(), body["data_source_id"])

	status, _ = s.get("/v1/operations/traces/abc?component_id=nope")
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *TracesHandlerSuite) TestATraceSourceFailingIs502WithoutTheSecret() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{s.binding(domain.SignalTracesSpans, "tempo-service")}, nil).Once()
	s.traces.On("Trace", mock.Anything, mock.Anything, mock.Anything, "abc").Return(domain.Trace{}, fmt.Errorf("Tempo trace: rejected s3cret-token")).Once()

	status, body := s.get(s.path("/traces/abc"))

	s.Equal(fiber.StatusBadGateway, status)
	s.Equal("upstream", body["code"])
	s.NotContains(body["error"], "s3cret-token")
}

func (s *TracesHandlerSuite) TestOperationsLogsCarryTheDataSourceOfABindingRead() {
	s.envs.On("ListAllEnvironments", mock.Anything).Return(nil, nil).Once()
	b := s.binding(domain.SignalLogsLines, "")
	b.Params, b.Query = map[string]string{}, `{service_name="api"}`
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{b}, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.component}, nil).Once()
	s.logs.On("Logs", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.LogQuery) bool {
		return q.Query == `{service_name="api"}` && q.Text == "boom"
	})).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{{Timestamp: s.now, Severity: domain.LogError, Message: "boom", TraceID: "abc"}}}, nil).Once()

	status, body := s.get("/v1/operations/logs?text=boom")

	s.Require().Equal(fiber.StatusOK, status, body)
	entries := body["entries"].([]any)
	s.Require().Len(entries, 1)
	e := entries[0].(map[string]any)
	s.Equal(s.source.ID.String(), e["data_source_id"])
	s.Equal(uuid.Nil.String(), e["environment_id"])
	s.Equal(s.component.ID.String(), e["component_id"])
	s.Equal("abc", e["trace_id"])
}
