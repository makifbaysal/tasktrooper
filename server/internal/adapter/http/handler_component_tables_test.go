package http

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

var componentTableKeys = []string{
	"signal_binding_id", "signal", "data_source_id", "data_source_name", "recipe_id", "table",
	"params", "columns", "key_columns", "rows", "truncated", "error",
}

func (s *SignalsHandlerSuite) TestEndpointsAnswersTheContractShape() {
	requests := s.binding("http.requests", domain.SignalBindingConfirmed)
	latency := s.binding("http.latency", domain.SignalBindingConfirmed)
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentStaging).
		Return([]domain.SignalBinding{latency, requests}, nil).Once()
	answer := func(match string, rows ...map[string]any) {
		s.table.On("Table", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(q domain.TableQuery) bool {
			return q.At.Equal(s.now) && strings.HasPrefix(q.Query, match)
		})).Return(domain.TableResult{Rows: rows}, nil).Once()
	}
	answer("topk(500, 60 * sum by (http_route)",
		map[string]any{"http_route": "/health", "value": 6.0},
		map[string]any{"http_route": "/orders", "value": 120.0})
	answer("100 * (sum by (http_route)", map[string]any{"http_route": "/orders", "value": 2.5})
	answer("1000 * histogram_quantile(0.95", map[string]any{"http_route": "/orders", "value": 180.0})

	status, body := s.do("GET", s.componentPath("/endpoints?env=staging"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	s.ElementsMatch([]string{"component_id", "environment", "read_at", "tables"}, keysOf(body))
	s.Equal("staging", body["environment"])
	tables := body["tables"].([]any)
	s.Require().Len(tables, 1, "both bindings read the one table their recipe declares")
	t := tables[0].(map[string]any)
	s.ElementsMatch(componentTableKeys, keysOf(t))
	s.Equal("http.requests", t["signal"])
	s.Equal([]any{"http_route"}, t["key_columns"])
	s.Equal(requests.ID.String(), t["signal_binding_id"], "the first binding in catalog order names the table")
	s.Equal(s.source.ID.String(), t["data_source_id"])
	s.Equal("otel-http", t["recipe_id"])
	s.Equal("endpoints", t["table"])
	s.Equal(map[string]any{"job": "api"}, t["params"])
	s.Nil(t["error"])
	s.Equal(false, t["truncated"])
	first := t["columns"].([]any)[0].(map[string]any)
	s.ElementsMatch([]string{"key", "label", "unit"}, keysOf(first))
	s.Equal([]any{
		map[string]any{"http_route": "/orders", "rpm": 120.0, "error_pct": 2.5, "p95_ms": 180.0},
		map[string]any{"http_route": "/health", "rpm": 6.0, "error_pct": nil, "p95_ms": nil},
	}, t["rows"], "the busiest endpoint first")
}

func (s *SignalsHandlerSuite) TestEndpointsCarriesATableErrorAndNeverFailsTheRequest() {
	requests := s.binding("http.requests", domain.SignalBindingConfirmed)
	requests.Params = map[string]string{}
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{requests}, nil).Once()

	status, body := s.do("GET", s.componentPath("/endpoints"), "")
	s.Require().Equal(fiber.StatusOK, status, body)
	t := body["tables"].([]any)[0].(map[string]any)
	s.Equal(map[string]any{"code": "needs_params", "message": "no value for job"}, t["error"])
	s.Equal([]any{}, t["rows"])
	s.Len(t["columns"], 4)
}

func (s *SignalsHandlerSuite) TestWebAndCrashesAnswerNoTablesWithoutABindingThatDeclaresOne() {
	other := s.binding("http.requests", domain.SignalBindingConfirmed)
	s.bindings.On("ListSignalBindings", mock.Anything, s.component.ID, domain.EnvironmentProduction).
		Return([]domain.SignalBinding{other}, nil).Twice()

	for _, path := range []string{"/web?table=referrers", "/crashes"} {
		status, body := s.do("GET", s.componentPath(path), "")
		s.Require().Equal(fiber.StatusOK, status, body)
		s.ElementsMatch([]string{"component_id", "environment", "read_at", "tables"}, keysOf(body))
		s.Equal([]any{}, body["tables"], path)
	}
}

func (s *SignalsHandlerSuite) TestWebRefusesAnUnknownTableAndABadComponent() {
	status, body := s.do("GET", s.componentPath("/web?table=queues"), "")
	s.Equal(fiber.StatusBadRequest, status)
	s.Contains(body["error"], "pages, referrers, countries, devices")

	status, _ = s.do("GET", "/v1/components/nope/crashes", "")
	s.Equal(fiber.StatusBadRequest, status)

	missing := uuid.New()
	s.comps.On("GetComponent", mock.Anything, missing).Return(domain.Component{}, port.ErrNotFound).Once()
	status, _ = s.do("GET", "/v1/components/"+missing.String()+"/endpoints", "")
	s.Equal(fiber.StatusNotFound, status)
}
