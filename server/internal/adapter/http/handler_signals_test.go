package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type signalsPromKind struct {
	*mocks.DataSourceKindAdapter
	*mocks.MetricsQuerier
	*mocks.Discoverer
	*mocks.TableQuerier
}

type SignalsHandlerSuite struct {
	suite.Suite
	dsStore    *mocks.DataSourceStore
	querier    *mocks.MetricsQuerier
	discoverer *mocks.Discoverer
	table      *mocks.TableQuerier
	bindings   *mocks.SignalBindingStore
	comps      *mocks.ComponentStore
	envs       *mocks.EnvironmentStore
	source     domain.DataSource
	component  domain.Component
	now        time.Time
	app        *fiber.App
}

func TestSignalsHandlerSuite(t *testing.T) { suite.Run(t, new(SignalsHandlerSuite)) }

func (s *SignalsHandlerSuite) SetupTest() {
	t := s.T()
	s.now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.dsStore = mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	kind := mocks.NewDataSourceKindAdapter(t)
	s.querier = mocks.NewMetricsQuerier(t)
	s.discoverer = mocks.NewDiscoverer(t)
	s.table = mocks.NewTableQuerier(t)
	s.bindings = mocks.NewSignalBindingStore(t)
	s.comps = mocks.NewComponentStore(t)
	s.envs = mocks.NewEnvironmentStore(t)

	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Prometheus"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	recipes.On("ListUserRecipes", mock.Anything).Return([]domain.Recipe{}, nil).Maybe()

	s.source = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Name: "local prom"}
	s.dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.source}, nil).Maybe()
	s.dsStore.On("GetDataSource", mock.Anything, s.source.ID).Return(s.source, nil).Maybe()
	s.dsStore.On("DataSourceSecrets", mock.Anything, s.source.ID).Return(map[string]string{}, nil).Maybe()

	s.component = domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: "api",
		Name: domain.Detected("api", domain.ConfidenceHigh), Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	s.comps.On("GetComponent", mock.Anything, s.component.ID).Return(s.component, nil).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: s.dsStore, Recipes: recipes,
		Kinds:     []port.DataSourceKindAdapter{signalsPromKind{kind, s.querier, s.discoverer, s.table}},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	svc, err := signals.NewService(signals.Deps{Bindings: s.bindings, Sources: sources, Components: s.comps, Environments: s.envs},
		signals.WithClock(func() time.Time { return s.now }))
	s.Require().NoError(err)
	s.app = fiber.New(fiber.Config{JSONEncoder: goccyjson.Marshal, JSONDecoder: goccyjson.Unmarshal})
	(&Handler{signalsSvc: svc}).registerSignalRoutes(s.app)
}

func (s *SignalsHandlerSuite) do(method, path, body string) (int, map[string]any) {
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.app.Test(req, 10000)
	s.Require().NoError(err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		s.Require().NoError(json.Unmarshal(raw, &out), string(raw))
	}
	return resp.StatusCode, out
}

func (s *SignalsHandlerSuite) binding(signal string, status domain.SignalBindingStatus) domain.SignalBinding {
	return domain.SignalBinding{
		ID: uuid.New(), ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: signal,
		DataSourceID: s.source.ID, RecipeID: "otel-http", Params: map[string]string{"job": "api"},
		Status: status, Source: domain.SignalBindingDetected, CreatedAt: s.now, UpdatedAt: s.now,
	}
}

var bindingKeys = []string{
	"id", "component_id", "environment", "signal", "data_source_id", "data_source_name", "data_source_kind",
	"recipe_id", "params", "query", "status", "source", "rendered_query", "needs_params", "created_at", "updated_at",
}

func (s *SignalsHandlerSuite) componentPath(rest string) string {
	return "/v1/components/" + s.component.ID.String() + rest
}

func (s *SignalsHandlerSuite) TestListAnswersTheContractShape() {
	bound := s.binding("http.requests", domain.SignalBindingConfirmed)
	pending := s.binding("http.latency", domain.SignalBindingSuggested)
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentStaging).
		Return([]domain.SignalBinding{bound, pending}, nil).Once()

	status, body := s.do("GET", s.componentPath("/signals?env=staging"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"component_id", "role", "environment", "signals", "source_offers"}, keysOf(body))
	s.Equal("backend", body["role"])
	s.Equal("staging", body["environment"])
	list := body["signals"].([]any)
	s.Require().NotEmpty(list)
	first := list[0].(map[string]any)
	s.ElementsMatch([]string{"signal", "kind", "unit", "golden", "status", "binding", "suggestions"}, keysOf(first))
	s.Equal("http.requests", first["signal"])
	s.Equal("bound", first["status"])
	s.Equal(true, first["golden"])
	b := first["binding"].(map[string]any)
	s.ElementsMatch(bindingKeys, keysOf(b))
	s.Equal("local prom", b["data_source_name"])
	s.Contains(b["rendered_query"], `job="api"`)
	s.Equal([]any{}, b["needs_params"])
	s.Equal([]any{}, first["suggestions"])

	for _, raw := range list {
		sig := raw.(map[string]any)
		if sig["signal"] == "http.error_ratio" {
			s.Nil(sig["binding"])
			s.Equal("unbound", sig["status"])
		}
		if sig["signal"] == "http.latency" {
			s.Equal("suggested", sig["status"])
			s.Len(sig["suggestions"], 1)
		}
	}
}

func (s *SignalsHandlerSuite) TestListRefusesABadEnvironmentAndAnUnknownComponent() {
	status, body := s.do("GET", s.componentPath("/signals?env=prod"), "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Contains(body["error"], "unknown environment")

	missing := uuid.New()
	s.comps.On("GetComponent", mock.Anything, missing).Return(domain.Component{}, port.ErrNotFound).Once()
	status, _ = s.do("GET", "/v1/components/"+missing.String()+"/signals", "")
	s.Equal(fiber.StatusNotFound, status)

	status, _ = s.do("GET", "/v1/components/nope/signals", "")
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *SignalsHandlerSuite) TestSuggestAnswersTheListShape() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{}, nil).Twice()
	s.envs.On("ListEnvironments", mock.Anything, s.component.RepositoryID).Return([]domain.ComponentEnvironment{}, nil).Once()
	s.discoverer.On("Discover", mock.Anything, mock.Anything, mock.Anything).Return(domain.Inventory{Metrics: []string{"up"}}, nil).Once()
	s.bindings.On("ReplaceDetectedSuggestions", mock.Anything, s.component.ID, domain.EnvironmentProduction, []uuid.UUID{s.source.ID}, []domain.SignalBinding{}).
		Return([]domain.SignalBinding{}, nil).Once()

	status, body := s.do("POST", s.componentPath("/signals/suggest"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"component_id", "role", "environment", "signals", "source_offers"}, keysOf(body))
	s.Equal("production", body["environment"])
}

func (s *SignalsHandlerSuite) TestPutBindsADottedSignalAndRefusesFieldsInTheDataSourceStyle() {
	s.bindings.On("PutConfirmedSignalBinding", mock.Anything, mock.MatchedBy(func(b domain.SignalBinding) bool {
		return b.Signal == "http.requests" && b.Query == "sum(rate(http_requests_total[$__rate]))" && b.Source == domain.SignalBindingHuman
	})).Return(func(_ context.Context, b domain.SignalBinding) (domain.SignalBinding, error) {
		b.ID, b.Status = uuid.New(), domain.SignalBindingConfirmed
		return b, nil
	}).Once()

	status, body := s.do("PUT", s.componentPath("/signals/http.requests?env=production"),
		`{"data_source_id":"`+s.source.ID.String()+`","query":"sum(rate(http_requests_total[$__rate]))"}`)
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch(bindingKeys, keysOf(body))
	s.Equal("confirmed", body["status"])
	s.Equal("human", body["source"])
	s.Equal("sum(rate(http_requests_total[120s]))", body["rendered_query"])

	status, body = s.do("PUT", s.componentPath("/signals/http.requests"), `{"recipe_id":"otel-http"}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"data_source_id": "required", "params.job": "required"}, body["fields"])
	s.NotEmpty(body["error"])
}

func (s *SignalsHandlerSuite) TestConfirmDismissAndDelete() {
	pending := s.binding("http.requests", domain.SignalBindingSuggested)
	confirmed := pending
	confirmed.Status = domain.SignalBindingConfirmed
	dismissed := pending
	dismissed.Status = domain.SignalBindingDismissed
	s.bindings.On("GetSignalBinding", mock.Anything, pending.ID).Return(pending, nil).Twice()
	s.bindings.On("ConfirmSignalBinding", mock.Anything, pending.ID).Return(confirmed, nil).Once()
	s.bindings.On("ConfirmSignalBinding", mock.Anything, pending.ID).
		Return(domain.SignalBinding{}, fmt.Errorf("confirm signal binding: %w", port.ErrSignalBindingConflict)).Once()
	s.bindings.On("DismissSignalBinding", mock.Anything, pending.ID).Return(dismissed, nil).Once()
	other := s.binding("http.latency", domain.SignalBindingConfirmed)
	s.bindings.On("DismissSignalBinding", mock.Anything, other.ID).Return(other, nil).Once()
	s.bindings.On("DeleteSignalBinding", mock.Anything, other.ID).Return(nil).Once()
	unknown := uuid.New()
	s.bindings.On("DeleteSignalBinding", mock.Anything, unknown).Return(port.ErrNotFound).Once()

	status, body := s.do("POST", "/v1/signal-bindings/"+pending.ID.String()+"/confirm", "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch(bindingKeys, keysOf(body))
	s.Equal("confirmed", body["status"])
	status, body = s.do("POST", "/v1/signal-bindings/"+pending.ID.String()+"/confirm", "")
	s.Equal(fiber.StatusConflict, status)
	s.Equal("conflict", body["code"])

	status, _ = s.do("POST", "/v1/signal-bindings/"+pending.ID.String()+"/dismiss", "")
	s.Equal(fiber.StatusNoContent, status)
	status, body = s.do("POST", "/v1/signal-bindings/"+other.ID.String()+"/dismiss", "")
	s.Equal(fiber.StatusConflict, status)
	s.Equal("confirmed", body["code"])

	status, _ = s.do("DELETE", "/v1/signal-bindings/"+other.ID.String(), "")
	s.Equal(fiber.StatusNoContent, status)
	status, _ = s.do("DELETE", "/v1/signal-bindings/"+unknown.String(), "")
	s.Equal(fiber.StatusNotFound, status)
}

func (s *SignalsHandlerSuite) TestPreviewAnswersRenderedQuerySeriesAndWarnings() {
	from := s.now.Add(-time.Hour)
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool {
		return q.Query == `sum(rate(http_requests_total{job="api"}[120s]))` && q.From.Equal(from) && q.Step == 30*time.Second
	})).Return(domain.SeriesResult{Series: []domain.Series{{Name: "x", Labels: map[string]string{}, Points: []domain.SeriesPoint{domain.FinitePoint(from, 2)}}}}, nil).Once()

	status, body := s.do("POST", s.componentPath("/signals/preview"),
		`{"signal":"http.requests","data_source_id":"`+s.source.ID.String()+`","query":"sum(rate(http_requests_total{job=\"$job\"}[$__rate]))","params":{"job":"api"},"from":"`+from.Format(time.RFC3339)+`","to":"2026-10-16T12:00:00.000Z"}`)
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"rendered_query", "series", "warnings"}, keysOf(body))
	series := body["series"].([]any)
	s.Require().Len(series, 1)
	s.ElementsMatch([]string{"name", "labels", "points"}, keysOf(series[0].(map[string]any)))
	s.Equal([]any{float64(from.UnixMilli()), float64(2)}, series[0].(map[string]any)["points"].([]any)[0])

	status, body = s.do("POST", s.componentPath("/signals/preview"), `{"signal":"http.requests","from":"yesterday"}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"from": "invalid"}, body["fields"])
}

func (s *SignalsHandlerSuite) TestSeriesAnswersPerSignalEntries() {
	bound := s.binding("http.requests", domain.SignalBindingConfirmed)
	bound.RecipeID, bound.Query = "", "sum(up)"
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{bound}, nil).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.SeriesQuery) bool { return q.Step == time.Minute })).
		Return(func(context.Context, domain.DataSource, map[string]string, domain.SeriesQuery) (domain.SeriesResult, error) {
			return domain.SeriesResult{Series: []domain.Series{{Name: "up", Labels: map[string]string{}}}}, nil
		}).Twice()

	status, body := s.do("GET", s.componentPath("/series?env=production&signals=http.requests,http.latency&from=2026-10-16T11:00:00Z&to=2026-10-16T12:00:00Z&step_seconds=60&compare=1"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"from", "to", "step_seconds", "signals"}, keysOf(body))
	s.Equal(float64(60), body["step_seconds"])
	list := body["signals"].([]any)
	s.Require().Len(list, 2)
	req := list[0].(map[string]any)
	s.ElementsMatch([]string{"signal", "unit", "bound", "series", "previous", "error", "previous_error"}, keysOf(req))
	s.Equal(true, req["bound"])
	s.Nil(req["error"])
	s.Nil(req["previous_error"])
	s.Len(req["series"], 1)
	s.Len(req["previous"], 1)
	lat := list[1].(map[string]any)
	s.Equal(false, lat["bound"])
	s.Equal([]any{}, lat["series"])

	status, body = s.do("GET", s.componentPath("/series?step_seconds=-1"), "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"step_seconds": "invalid"}, body["fields"])
}

func (s *SignalsHandlerSuite) TestGoldenAnswersTheExplorerShape() {
	bound := s.binding("http.requests", domain.SignalBindingConfirmed)
	s.bindings.On("ListConfirmedSignalBindings", mock.Anything, domain.EnvironmentProduction).Return([]domain.SignalBinding{bound}, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return([]domain.Component{s.component}, nil).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(domain.SeriesResult{Series: []domain.Series{{Points: []domain.SeriesPoint{domain.FinitePoint(s.now, 4)}}}}, nil).Once()

	status, body := s.do("GET", "/v1/operations/golden?env=production&component_id="+s.component.ID.String(), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"generated_at", "components"}, keysOf(body))
	comps := body["components"].([]any)
	s.Require().Len(comps, 1)
	c := comps[0].(map[string]any)
	s.ElementsMatch([]string{"component_id", "signals"}, keysOf(c))
	sigs := c["signals"].([]any)
	s.Require().Len(sigs, 3)
	req := sigs[0].(map[string]any)
	s.ElementsMatch([]string{"signal", "unit", "bound", "value", "previous", "spark"}, keysOf(req))
	s.Equal(true, req["bound"])
	s.Equal(float64(4), req["value"])
	s.Nil(req["previous"])
	s.Equal([]any{[]any{float64(s.now.UnixMilli()), float64(4)}}, req["spark"])
	s.Equal(false, sigs[1].(map[string]any)["bound"])
	s.Nil(sigs[1].(map[string]any)["value"])

	status, _ = s.do("GET", "/v1/operations/golden?component_id=nope", "")
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *SignalsHandlerSuite) TestQueuesAnswersTheContractShape() {
	backlog := s.binding("queue.backlog", domain.SignalBindingConfirmed)
	backlog.RecipeID, backlog.Params = "rabbitmq-prometheus", map[string]string{"queue": "orders", "vhost": "/"}
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentStaging).Return([]domain.SignalBinding{backlog}, nil).Once()
	s.table.On("Table", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.TableQuery) bool {
		return q.At.Equal(s.now)
	})).Return(domain.TableResult{Rows: []map[string]any{{"vhost": "/", "queue": "orders", "value": 3.0}}}, nil).Times(3)

	status, body := s.do("GET", s.componentPath("/queues?env=staging"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"component_id", "environment", "read_at", "queues"}, keysOf(body))
	s.Equal("staging", body["environment"])
	s.Equal(s.now.Format(time.RFC3339), body["read_at"])
	queues := body["queues"].([]any)
	s.Require().Len(queues, 1)
	q := queues[0].(map[string]any)
	s.ElementsMatch([]string{
		"signal_binding_id", "signal", "data_source_id", "data_source_name", "recipe_id", "messaging_system",
		"params", "columns", "key_columns", "rows", "truncated", "error",
	}, keysOf(q))
	s.Equal(backlog.ID.String(), q["signal_binding_id"])
	s.Equal("queue.backlog", q["signal"])
	s.Equal([]any{"vhost", "queue"}, q["key_columns"])
	s.Equal("rabbitmq", q["messaging_system"])
	s.Equal(map[string]any{"queue": "orders", "vhost": "/"}, q["params"])
	s.Nil(q["error"])
	first := q["columns"].([]any)[0].(map[string]any)
	s.ElementsMatch([]string{"key", "label", "unit"}, keysOf(first))
	s.Equal([]any{map[string]any{"vhost": "/", "queue": "orders", "ready": 3.0, "unacked": 3.0, "consumers": 3.0}}, q["rows"])
}

func (s *SignalsHandlerSuite) TestQueuesRefusesABadEnvironment() {
	status, body := s.do("GET", s.componentPath("/queues?env=prod"), "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Contains(body["error"], "unknown environment")
}
