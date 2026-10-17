package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	goccyjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

var (
	alertRuleSpecKeys = []string{
		"name", "component_id", "environment", "signal", "data_source_id", "query", "condition", "severity",
		"channels", "open_incident", "enabled",
	}
	alertRuleKeys = append([]string{"id", "state", "state_since", "last_value", "last_evaluated_at", "error_count", "last_error", "created_by", "created_at", "updated_at"},
		alertRuleSpecKeys...)
)

type AlertRulesHandlerSuite struct {
	suite.Suite
	rules     *mocks.AlertRuleStore
	bindings  *mocks.SignalBindingStore
	querier   *mocks.MetricsQuerier
	source    domain.DataSource
	component domain.Component
	now       time.Time
	app       *fiber.App
}

func TestAlertRulesHandlerSuite(t *testing.T) { suite.Run(t, new(AlertRulesHandlerSuite)) }

func (s *AlertRulesHandlerSuite) SetupTest() {
	t := s.T()
	s.now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s.rules = mocks.NewAlertRuleStore(t)
	dsStore := mocks.NewDataSourceStore(t)
	recipes := mocks.NewObservabilityRecipeStore(t)
	kind := mocks.NewDataSourceKindAdapter(t)
	s.querier = mocks.NewMetricsQuerier(t)
	s.bindings = mocks.NewSignalBindingStore(t)
	comps := mocks.NewComponentStore(t)

	kind.On("Kind").Return(domain.DataSourceKindPrometheus).Maybe()
	kind.On("Schema").Return(domain.DataSourceSchema{Label: "Prometheus"}).Maybe()
	kind.On("Capabilities").Return([]domain.Capability{domain.CapabilityMetrics}).Maybe()
	recipes.On("ListUserRecipes", mock.Anything).Return([]domain.Recipe{}, nil).Maybe()
	s.source = domain.DataSource{ID: uuid.New(), Kind: domain.DataSourceKindPrometheus, Name: "prom"}
	dsStore.On("ListDataSources", mock.Anything).Return([]domain.DataSource{s.source}, nil).Maybe()
	dsStore.On("GetDataSource", mock.Anything, s.source.ID).Return(s.source, nil).Maybe()
	dsStore.On("DataSourceSecrets", mock.Anything, s.source.ID).Return(map[string]string{}, nil).Maybe()

	s.component = domain.Component{
		ID: uuid.New(), RepositoryID: uuid.New(), Path: "api",
		Name: domain.Detected("api", domain.ConfidenceHigh), Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceHigh),
		Status: domain.ComponentStatusActive,
	}
	comps.On("GetComponent", mock.Anything, s.component.ID).Return(s.component, nil).Maybe()
	comps.On("GetComponent", mock.Anything, mock.Anything).Return(domain.Component{}, port.ErrNotFound).Maybe()

	policy := urlguard.HumanConfigured()
	sources, err := datasource.NewService(datasource.Deps{
		Store: dsStore, Recipes: recipes,
		Kinds:     []port.DataSourceKindAdapter{signalsPromKind{kind, s.querier, mocks.NewDiscoverer(t), mocks.NewTableQuerier(t)}},
		URLPolicy: &policy,
	})
	s.Require().NoError(err)
	sigs, err := signals.NewService(signals.Deps{Bindings: s.bindings, Sources: sources, Components: comps, Environments: mocks.NewEnvironmentStore(t)})
	s.Require().NoError(err)
	desktop := mocks.NewNotificationChannel(t)
	desktop.On("Kind").Return(domain.NotificationChannelDesktop).Maybe()
	notifier := notify.NewService()
	notifier.Register(desktop)
	svc, err := alerting.NewService(alerting.Deps{Rules: s.rules, Signals: sigs, Sources: sources, Components: comps, Notifier: notifier},
		alerting.WithClock(func() time.Time { return s.now }))
	s.Require().NoError(err)
	s.app = fiber.New(fiber.Config{JSONEncoder: goccyjson.Marshal, JSONDecoder: goccyjson.Unmarshal})
	(&Handler{alertingSvc: svc}).registerAlertRuleRoutes(s.app)
}

func (s *AlertRulesHandlerSuite) do(method, path, body string) (int, map[string]any) {
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

func (s *AlertRulesHandlerSuite) stored() domain.AlertRule {
	last := 5.5
	return domain.AlertRule{
		ID: uuid.New(),
		AlertRuleSpec: domain.AlertRuleSpec{
			Name: "Error ratio", ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.error_ratio",
			Condition: domain.AlertCondition{Op: domain.AlertOpAbove, Threshold: 2, ForSeconds: 300, WindowSeconds: 300},
			Severity:  domain.IncidentSeverityHigh, Channels: []domain.NotificationChannelKind{domain.NotificationChannelDesktop},
			OpenIncident: true, Enabled: true,
		},
		State: domain.AlertStateFiring, StateSince: s.now, LastValue: &last, LastEvaluatedAt: &s.now,
		CreatedAt: s.now, UpdatedAt: s.now,
	}
}

func (s *AlertRulesHandlerSuite) TestListAnswersTheContractShape() {
	rule := s.stored()
	s.rules.On("ListAlertRules", mock.Anything, domain.AlertRuleFilter{ComponentID: &s.component.ID, Environment: domain.EnvironmentStaging}).
		Return([]domain.AlertRule{rule}, nil).Once()

	status, body := s.do("GET", "/v1/alert-rules?component_id="+s.component.ID.String()+"&env=staging", "")
	s.Require().Equal(fiber.StatusOK, status, body)
	list := body["rules"].([]any)
	s.Require().Len(list, 1)
	got := list[0].(map[string]any)
	s.ElementsMatch(alertRuleKeys, keysOf(got))
	s.ElementsMatch([]string{"op", "threshold", "for_seconds", "window_seconds"}, keysOf(got["condition"].(map[string]any)))
	s.Equal("firing", got["state"])
	s.Equal(5.5, got["last_value"])
	s.Nil(got["data_source_id"])
	s.Equal([]any{"desktop"}, got["channels"])
}

func (s *AlertRulesHandlerSuite) TestListRefusesABadFilter() {
	status, body := s.do("GET", "/v1/alert-rules?component_id=nope", "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"component_id": "invalid"}, body["fields"])

	status, body = s.do("GET", "/v1/alert-rules?env=prod", "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"env": "invalid"}, body["fields"])
}

func (s *AlertRulesHandlerSuite) TestCreateAnswers201WithTheRule() {
	s.rules.On("CreateAlertRule", mock.Anything, mock.Anything).Return(func(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
		r.ID, r.CreatedAt, r.UpdatedAt = uuid.New(), s.now, s.now
		return r, nil
	}).Once()

	status, body := s.do("POST", "/v1/alert-rules", `{"name":"Errors","component_id":"`+s.component.ID.String()+
		`","signal":"http.error_ratio","condition":{"op":"gt","threshold":2,"for_seconds":300},"channels":["desktop"],"open_incident":true}`)
	s.Require().Equal(fiber.StatusCreated, status, body)
	s.ElementsMatch(alertRuleKeys, keysOf(body))
	s.Equal("production", body["environment"])
	s.Equal("medium", body["severity"])
	s.Equal("ok", body["state"])
	s.Equal(true, body["enabled"])
	s.Equal(map[string]any{"op": "gt", "threshold": 2.0, "for_seconds": 300.0, "window_seconds": 300.0}, body["condition"])
}

func (s *AlertRulesHandlerSuite) TestNotificationChannelsSayWhichCanDeliver() {
	status, body := s.do("GET", "/v1/notification-channels", "")
	s.Require().Equal(fiber.StatusOK, status, body)
	channels := body["channels"].([]any)
	s.Require().Len(channels, 6)
	available := map[string]any{}
	for _, raw := range channels {
		ch := raw.(map[string]any)
		s.ElementsMatch([]string{"kind", "available"}, keysOf(ch))
		available[ch["kind"].(string)] = ch["available"]
	}
	s.Equal(true, available["desktop"])
	s.Equal(false, available["slack"])

	status, body = s.do("POST", "/v1/alert-rules", `{"name":"Errors","component_id":"`+s.component.ID.String()+
		`","signal":"http.error_ratio","condition":{"op":"gt","threshold":2},"channels":["slack"]}`)
	s.Require().Equal(fiber.StatusBadRequest, status, body)
	s.Equal(map[string]any{"channels": "unavailable"}, body["fields"])
}

func (s *AlertRulesHandlerSuite) TestCreateAnswersFieldErrorsInTheDataSourceShape() {
	status, body := s.do("POST", "/v1/alert-rules", `{"component_id":"`+uuid.NewString()+`","condition":{"op":"between"}}`)
	s.Require().Equal(fiber.StatusBadRequest, status, body)
	s.ElementsMatch([]string{"error", "fields"}, keysOf(body))
	s.Equal(map[string]any{
		"name": "required", "component_id": "invalid", "signal": "required", "condition.op": "invalid",
	}, body["fields"])

	status, _ = s.do("POST", "/v1/alert-rules", `{"condition":{"threshold":"high"}}`)
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *AlertRulesHandlerSuite) TestPatchAndDelete() {
	rule := s.stored()
	s.rules.On("GetAlertRule", mock.Anything, rule.ID).Return(rule, nil)
	s.rules.On("UpdateAlertRule", mock.Anything, mock.Anything).Return(func(_ context.Context, r domain.AlertRule) (domain.AlertRule, error) {
		return r, nil
	}).Once()

	status, body := s.do("PATCH", "/v1/alert-rules/"+rule.ID.String(), `{"name":"Renamed"}`)
	s.Require().Equal(fiber.StatusOK, status, body)
	s.Equal("Renamed", body["name"])
	s.Equal("firing", body["state"])

	unknown := uuid.New()
	s.rules.On("GetAlertRule", mock.Anything, unknown).Return(domain.AlertRule{}, port.ErrNotFound)
	status, _ = s.do("PATCH", "/v1/alert-rules/"+unknown.String(), `{"name":"x"}`)
	s.Equal(fiber.StatusNotFound, status)
	status, _ = s.do("DELETE", "/v1/alert-rules/"+unknown.String(), "")
	s.Equal(fiber.StatusNotFound, status)
	status, _ = s.do("PATCH", "/v1/alert-rules/nope", `{}`)
	s.Equal(fiber.StatusBadRequest, status)

	disabled := s.stored()
	disabled.State, disabled.OpenIncident = domain.AlertStateOK, false
	s.rules.On("GetAlertRule", mock.Anything, disabled.ID).Return(disabled, nil)
	s.rules.On("DeleteAlertRule", mock.Anything, disabled.ID).Return(nil).Once()
	status, _ = s.do("DELETE", "/v1/alert-rules/"+disabled.ID.String(), "")
	s.Equal(fiber.StatusNoContent, status)
}

func (s *AlertRulesHandlerSuite) TestEventsAnswerTheContractShape() {
	rule := s.stored()
	incident := uuid.New()
	value := 5.5
	s.rules.On("GetAlertRule", mock.Anything, rule.ID).Return(rule, nil)
	s.rules.On("ListAlertEvents", mock.Anything, rule.ID, 20).Return([]domain.AlertEvent{{
		ID: uuid.New(), RuleID: rule.ID, At: s.now, FromState: domain.AlertStatePending, ToState: domain.AlertStateFiring,
		Value: &value, IncidentID: &incident,
	}}, nil).Once()

	status, body := s.do("GET", "/v1/alert-rules/"+rule.ID.String()+"/events?limit=20", "")
	s.Require().Equal(fiber.StatusOK, status, body)
	events := body["events"].([]any)
	s.Require().Len(events, 1)
	got := events[0].(map[string]any)
	s.ElementsMatch([]string{"at", "from_state", "to_state", "value", "incident_id"}, keysOf(got))
	s.Equal(incident.String(), got["incident_id"])

	status, body = s.do("GET", "/v1/alert-rules/"+rule.ID.String()+"/events?limit=0", "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"limit": "invalid"}, body["fields"])
}

func (s *AlertRulesHandlerSuite) TestBacktestAnswersFiresAndSeries() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{{
		ID: uuid.New(), ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.error_ratio",
		DataSourceID: s.source.ID, Query: "error_ratio", Status: domain.SignalBindingConfirmed,
	}}, nil).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
		func(_ context.Context, _ domain.DataSource, _ map[string]string, q domain.SeriesQuery) (domain.SeriesResult, error) {
			var points []domain.SeriesPoint
			for at := q.From; !at.After(q.To); at = at.Add(q.Step) {
				points = append(points, domain.FinitePoint(at, 5))
			}
			return domain.SeriesResult{Series: []domain.Series{{Points: points}}}, nil
		}).Once()

	status, body := s.do("POST", "/v1/alert-rules/backtest", `{"component_id":"`+s.component.ID.String()+
		`","signal":"http.error_ratio","condition":{"op":"gt","threshold":2},"days":1}`)
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"fires", "series", "step_seconds"}, keysOf(body))
	s.Equal(300.0, body["step_seconds"])
	fires := body["fires"].([]any)
	s.Require().Len(fires, 1)
	s.ElementsMatch([]string{"start", "end", "peak"}, keysOf(fires[0].(map[string]any)))
	s.Nil(fires[0].(map[string]any)["end"])
	series := body["series"].([]any)
	s.ElementsMatch([]string{"name", "points"}, keysOf(series[0].(map[string]any)))
}

func (s *AlertRulesHandlerSuite) TestBacktestOfAnUnboundSignalIs422() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{}, nil).Once()

	status, body := s.do("POST", "/v1/alert-rules/backtest", `{"component_id":"`+s.component.ID.String()+
		`","signal":"http.error_ratio","condition":{"op":"gt","threshold":2}}`)
	s.Equal(fiber.StatusUnprocessableEntity, status)
	s.Equal("unbound", body["code"])

	status, body = s.do("POST", "/v1/alert-rules/backtest", `{"component_id":"`+s.component.ID.String()+
		`","signal":"http.error_ratio","condition":{"op":"gt"},"days":15}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"days": "invalid"}, body["fields"])
}

func (s *AlertRulesHandlerSuite) TestSuggestAnswersRulesWithoutIDs() {
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).Return([]domain.SignalBinding{{
		ID: uuid.New(), ComponentID: s.component.ID, Environment: domain.EnvironmentProduction, Signal: "http.error_ratio",
		DataSourceID: s.source.ID, Query: "error_ratio", Status: domain.SignalBindingConfirmed,
	}}, nil)
	s.rules.On("ListAlertRules", mock.Anything, mock.Anything).Return([]domain.AlertRule{}, nil).Once()
	s.querier.On("Series", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(domain.SeriesResult{}, nil).Once()

	status, body := s.do("POST", "/v1/alert-rules/suggest", `{"component_id":"`+s.component.ID.String()+`","environment":"production"}`)
	s.Require().Equal(fiber.StatusOK, status, body)
	rules := body["rules"].([]any)
	s.Require().Len(rules, 1)
	got := rules[0].(map[string]any)
	s.ElementsMatch(alertRuleSpecKeys, keysOf(got))
	s.Equal("http.error_ratio", got["signal"])

	status, body = s.do("POST", "/v1/alert-rules/suggest", `{"component_id":"nope"}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal(map[string]any{"component_id": "invalid"}, body["fields"])
}
