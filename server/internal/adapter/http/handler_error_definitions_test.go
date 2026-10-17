package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsai"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// fakeOpsAI records what the routes hand the AI service and answers what a
// test sets.
type fakeOpsAI struct {
	suggestErrors opsai.SuggestErrorsRequest
	suggestEnv    domain.DeployEnvironment
	enable        opsai.EnableRequest
	job           domain.AIJob
	task          domain.BoardTask
	readiness     domain.ObservabilityReadiness
	err           error
}

func (f *fakeOpsAI) SuggestErrors(_ context.Context, req opsai.SuggestErrorsRequest) (domain.AIJob, error) {
	f.suggestErrors = req
	return f.job, f.err
}

func (f *fakeOpsAI) SuggestSignals(_ context.Context, _ uuid.UUID, env domain.DeployEnvironment) (domain.AIJob, error) {
	f.suggestEnv = env
	return f.job, f.err
}

func (f *fakeOpsAI) Job(_ context.Context, id uuid.UUID) (domain.AIJob, error) {
	if id != f.job.ID {
		return domain.AIJob{}, port.ErrNotFound
	}
	return f.job, nil
}

func (f *fakeOpsAI) Enable(_ context.Context, _ uuid.UUID, req opsai.EnableRequest) (domain.BoardTask, error) {
	f.enable = req
	return f.task, f.err
}

func (f *fakeOpsAI) Readiness(_ context.Context, _ uuid.UUID, _ domain.DeployEnvironment) (domain.ObservabilityReadiness, error) {
	return f.readiness, f.err
}

type ErrorDefinitionsHandlerSuite struct {
	suite.Suite
	store   *mocks.ErrorDefinitionStore
	comps   *mocks.ComponentStore
	envs    *mocks.EnvironmentStore
	runtime *mocks.RuntimeReader
	ai      *fakeOpsAI
	app     *fiber.App
	repo    uuid.UUID
	comp    domain.Component
}

func TestErrorDefinitionsHandlerSuite(t *testing.T) { suite.Run(t, new(ErrorDefinitionsHandlerSuite)) }

func (s *ErrorDefinitionsHandlerSuite) SetupTest() {
	t := s.T()
	s.store = mocks.NewErrorDefinitionStore(t)
	s.comps = mocks.NewComponentStore(t)
	s.envs = mocks.NewEnvironmentStore(t)
	s.runtime = mocks.NewRuntimeReader(t)
	s.ai = &fakeOpsAI{}
	s.repo = uuid.New()
	s.comp = domain.Component{ID: uuid.New(), RepositoryID: s.repo, Path: "api", Status: domain.ComponentStatusActive}
	defs, err := errordef.NewService(errordef.Deps{Store: s.store, Components: s.comps})
	s.Require().NoError(err)
	h := &Handler{
		errorDefsSvc: defs,
		opsAISvc:     s.ai,
		opsQuerySvc:  opsquery.NewService(opsquery.Deps{Environments: s.envs, Runtime: s.runtime}),
	}
	s.app = fiber.New()
	h.registerOpsQueryRoutes(s.app)
	h.registerErrorDefinitionRoutes(s.app)
}

func (s *ErrorDefinitionsHandlerSuite) do(method, path, body string) (int, map[string]any) {
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

func (s *ErrorDefinitionsHandlerSuite) definition(status domain.ErrorDefinitionStatus, action domain.ErrorAction, match domain.ErrorMatch) domain.ErrorDefinition {
	return domain.ErrorDefinition{
		ID: uuid.New(), RepositoryID: s.repo, Name: "Timeout", Match: match,
		Category: domain.ErrorCategoryDependency, Severity: domain.IncidentSeverityHigh, Action: action,
		Source: domain.ErrorDefinitionAI, Status: status, Evidence: []string{},
	}
}

var errorDefinitionKeys = []string{
	"id", "repository_id", "component_id", "name", "match", "category", "severity", "action", "alert_threshold",
	"source", "status", "rationale", "evidence", "task_id", "firing_since", "created_at", "updated_at",
}

func (s *ErrorDefinitionsHandlerSuite) TestCreateStoresAnActiveHumanDefinition() {
	s.comps.On("GetComponent", mock.Anything, s.comp.ID).Return(s.comp, nil).Once()
	s.store.On("ListErrorDefinitions", mock.Anything, mock.Anything).Return([]domain.ErrorDefinition{}, nil).Once()
	s.store.On("CreateErrorDefinition", mock.Anything, mock.MatchedBy(func(d domain.ErrorDefinition) bool {
		return d.Source == domain.ErrorDefinitionHuman && d.Status == domain.ErrorDefinitionActive &&
			d.Match.MessageRegex == "timeout" && *d.ComponentID == s.comp.ID && d.AlertThreshold.Count == 5
	})).Return(func(_ context.Context, d domain.ErrorDefinition) (domain.ErrorDefinition, error) {
		d.ID = uuid.New()
		return d, nil
	}).Once()

	status, body := s.do("POST", "/v1/error-definitions", `{"repository_id":"`+s.repo.String()+`","component_id":"`+s.comp.ID.String()+
		`","name":"Timeout","match":{"message_regex":"timeout"},"category":"dependency","action":"alert","alert_threshold":{"count":5,"window_seconds":600}}`)
	s.Equal(fiber.StatusCreated, status, body)
	s.ElementsMatch(errorDefinitionKeys, keysOf(body))
	s.Equal("human", body["source"])
}

func (s *ErrorDefinitionsHandlerSuite) TestCreateAnswersFieldErrors() {
	status, body := s.do("POST", "/v1/error-definitions", `{"name":"","match":{"message_regex":"("}}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.ElementsMatch([]string{"error", "fields"}, keysOf(body))
	fields := body["fields"].(map[string]any)
	s.Equal("required", fields["repository_id"])
	s.Equal("required", fields["name"])
	s.Equal("invalid", fields["match.message_regex"])
	s.Equal("required", fields["category"])
}

func (s *ErrorDefinitionsHandlerSuite) TestListPassesTheFilters() {
	d := s.definition(domain.ErrorDefinitionSuggested, domain.ErrorActionNone, domain.ErrorMatch{Status: 504})
	s.store.On("ListErrorDefinitions", mock.Anything, domain.ErrorDefinitionFilter{
		RepositoryIDs: []uuid.UUID{s.repo}, ComponentID: &s.comp.ID, Statuses: []domain.ErrorDefinitionStatus{domain.ErrorDefinitionSuggested},
	}).Return([]domain.ErrorDefinition{d}, nil).Once()

	status, body := s.do("GET", "/v1/error-definitions?repository_id="+s.repo.String()+"&component_id="+s.comp.ID.String()+"&status=suggested", "")
	s.Equal(fiber.StatusOK, status)
	s.Len(body["definitions"], 1)

	status, _ = s.do("GET", "/v1/error-definitions?repository_id=nope", "")
	s.Equal(fiber.StatusBadRequest, status)
	status, _ = s.do("GET", "/v1/error-definitions?status=maybe", "")
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *ErrorDefinitionsHandlerSuite) TestPatchDeleteAcceptDismiss() {
	d := s.definition(domain.ErrorDefinitionSuggested, domain.ErrorActionNone, domain.ErrorMatch{Status: 504})
	s.store.On("GetErrorDefinition", mock.Anything, d.ID).Return(d, nil)
	s.store.On("UpdateErrorDefinition", mock.Anything, mock.MatchedBy(func(u domain.ErrorDefinition) bool {
		return u.Name == "Provider timeout" && u.Action == domain.ErrorActionIgnore
	})).Return(func(_ context.Context, u domain.ErrorDefinition) (domain.ErrorDefinition, error) { return u, nil }).Once()
	status, body := s.do("PATCH", "/v1/error-definitions/"+d.ID.String(), `{"name":"Provider timeout","action":"ignore"}`)
	s.Equal(fiber.StatusOK, status, body)
	s.Equal("Provider timeout", body["name"])

	accepted := d
	accepted.Status = domain.ErrorDefinitionActive
	s.store.On("SetErrorDefinitionStatus", mock.Anything, d.ID, domain.ErrorDefinitionActive).Return(accepted, nil).Once()
	status, body = s.do("POST", "/v1/error-definitions/"+d.ID.String()+"/accept", "")
	s.Equal(fiber.StatusOK, status)
	s.Equal("active", body["status"])

	human := s.definition(domain.ErrorDefinitionActive, domain.ErrorActionNone, domain.ErrorMatch{Status: 500})
	s.store.On("GetErrorDefinition", mock.Anything, human.ID).Return(human, nil)
	status, body = s.do("POST", "/v1/error-definitions/"+human.ID.String()+"/dismiss", "")
	s.Equal(fiber.StatusConflict, status)
	s.Equal("not_suggested", body["code"])

	s.store.On("DeleteErrorDefinition", mock.Anything, d.ID).Return(nil).Once()
	status, _ = s.do("DELETE", "/v1/error-definitions/"+d.ID.String(), "")
	s.Equal(fiber.StatusNoContent, status)

	missing := uuid.New()
	s.store.On("GetErrorDefinition", mock.Anything, missing).Return(domain.ErrorDefinition{}, port.ErrNotFound)
	status, _ = s.do("POST", "/v1/error-definitions/"+missing.String()+"/accept", "")
	s.Equal(fiber.StatusNotFound, status)
	status, _ = s.do("PATCH", "/v1/error-definitions/not-a-uuid", `{}`)
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *ErrorDefinitionsHandlerSuite) TestTheErrorsInboxCarriesDefinitionsAndHidesIgnoredGroups() {
	env := opsEnv(domain.EnvironmentProduction)
	env.RepositoryID, env.ComponentID = s.repo, s.comp.ID
	s.envs.On("ListAllEnvironments", mock.Anything).Return([]domain.ComponentEnvironment{env}, nil)
	s.runtime.On("ErrorsSupported", mock.Anything).Return(true)
	seen := time.Date(2026, 10, 16, 11, 0, 0, 0, time.UTC)
	s.runtime.On("Errors", mock.Anything, env.ID, mock.Anything).Return([]domain.RuntimeErrorGroup{
		{Fingerprint: "a", Message: "upstream timeout", Count: 3, FirstSeen: seen, LastSeen: seen},
		{Fingerprint: "b", Message: "health check 404", Count: 9, FirstSeen: seen, LastSeen: seen},
		{Fingerprint: "c", Message: "nil pointer", Count: 1, FirstSeen: seen, LastSeen: seen},
	}, nil)
	alert := s.definition(domain.ErrorDefinitionActive, domain.ErrorActionAlert, domain.ErrorMatch{MessageRegex: "timeout"})
	ignore := s.definition(domain.ErrorDefinitionActive, domain.ErrorActionIgnore, domain.ErrorMatch{Fingerprint: "b"})
	s.store.On("ListErrorDefinitions", mock.Anything, domain.ErrorDefinitionFilter{
		RepositoryIDs: []uuid.UUID{s.repo}, Statuses: []domain.ErrorDefinitionStatus{domain.ErrorDefinitionActive},
	}).Return([]domain.ErrorDefinition{alert, ignore}, nil).Once()

	status, body := s.do("GET", "/v1/operations/errors", "")
	s.Equal(fiber.StatusOK, status)
	s.Equal(float64(1), body["ignored_count"])
	groups := body["groups"].([]any)
	s.Require().Len(groups, 2)
	byFP := map[string]map[string]any{}
	for _, g := range groups {
		m := g.(map[string]any)
		byFP[m["fingerprint"].(string)] = m
	}
	def := byFP["a"]["definition"].(map[string]any)
	s.ElementsMatch([]string{"id", "name", "category", "severity", "action"}, keysOf(def))
	s.Equal(alert.ID.String(), def["id"])
	s.Equal("alert", def["action"])
	s.Nil(byFP["c"]["definition"])

	status, body = s.do("GET", "/v1/operations/errors?include_ignored=1", "")
	s.Equal(fiber.StatusOK, status)
	s.NotContains(keysOf(body), "ignored_count")
	s.Len(body["groups"], 3)
}

func (s *ErrorDefinitionsHandlerSuite) TestSuggestJobsAnswerTheirJobID() {
	s.ai.job = domain.AIJob{ID: uuid.New(), Kind: domain.AIJobDefineErrors, Status: domain.AIJobRunning, Created: []domain.AIJobItem{}}
	status, body := s.do("POST", "/v1/error-definitions/suggest", `{"repository_id":"`+s.repo.String()+`","environment":"staging","mode":"code"}`)
	s.Equal(fiber.StatusAccepted, status)
	s.Equal(s.ai.job.ID.String(), body["job_id"])
	s.Equal(opsai.SuggestErrorsRequest{RepositoryID: s.repo.String(), Environment: "staging", Mode: "code"}, s.ai.suggestErrors)

	status, body = s.do("POST", "/v1/components/"+s.comp.ID.String()+"/signals/ai-suggest?env=staging", "")
	s.Equal(fiber.StatusAccepted, status)
	s.Equal(s.ai.job.ID.String(), body["job_id"])
	s.Equal(domain.EnvironmentStaging, s.ai.suggestEnv)

	status, body = s.do("GET", "/v1/ai-jobs/"+s.ai.job.ID.String(), "")
	s.Equal(fiber.StatusOK, status)
	s.ElementsMatch([]string{"id", "kind", "repository_id", "component_id", "environment", "status", "session_id",
		"result_summary", "error", "created", "created_at", "started_at", "updated_at", "finished_at"}, keysOf(body))
	status, _ = s.do("GET", "/v1/ai-jobs/"+uuid.NewString(), "")
	s.Equal(fiber.StatusNotFound, status)

	s.ai.err = opsai.ErrNoReleaseEngineer
	status, body = s.do("POST", "/v1/error-definitions/suggest", `{"repository_id":"`+s.repo.String()+`"}`)
	s.Equal(fiber.StatusConflict, status)
	s.Equal("no_release_engineer", body["code"])
	s.ai.err = opsai.FieldErrors{"mode": "invalid_option"}
	status, body = s.do("POST", "/v1/error-definitions/suggest", `{"repository_id":"`+s.repo.String()+`","mode":"x"}`)
	s.Equal(fiber.StatusBadRequest, status)
	s.Equal("invalid_option", body["fields"].(map[string]any)["mode"])
}

func (s *ErrorDefinitionsHandlerSuite) TestEnableAndReadiness() {
	s.ai.task = domain.BoardTask{ID: uuid.New(), Key: "TC-3"}
	status, body := s.do("POST", "/v1/components/"+s.comp.ID.String()+"/observability/enable",
		`{"environment":"production","goals":["traces","logs"],"target":"otlp_endpoint","otlp_endpoint":"https://otel.example.com"}`)
	s.Equal(fiber.StatusCreated, status)
	s.Equal(map[string]any{"task_id": s.ai.task.ID.String(), "task_key": "TC-3"}, body)
	s.Equal(opsai.EnableRequest{Environment: "production", Goals: []string{"traces", "logs"}, Target: "otlp_endpoint", OTLPEndpoint: "https://otel.example.com"}, s.ai.enable)

	s.ai.readiness = domain.ObservabilityReadiness{ComponentID: s.comp.ID, Environment: domain.EnvironmentProduction,
		Signals: []domain.ReadinessSignal{}, Instrumentation: map[string]bool{"otel": false}, Suggestions: []domain.ReadinessSuggestion{{Goal: domain.GoalTraces, Code: "no_tracing_sdk"}}}
	status, body = s.do("GET", "/v1/components/"+s.comp.ID.String()+"/observability", "")
	s.Equal(fiber.StatusOK, status)
	s.ElementsMatch([]string{"component_id", "environment", "role", "provider", "signals", "instrumentation", "suggestions"}, keysOf(body))

	status, _ = s.do("GET", "/v1/components/nope/observability", "")
	s.Equal(fiber.StatusBadRequest, status)
}
