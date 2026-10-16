package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const alertTestAPIKey = "server-api-key"

type AlertTokenHandlerSuite struct {
	suite.Suite
	incidents *mocks.IncidentStore
	tokens    *mocks.AlertIngestTokenStore
	app       *fiber.App
	repoID    uuid.UUID
	token     string
	stored    domain.AlertIngestToken
}

func TestAlertTokenHandlerSuite(t *testing.T) { suite.Run(t, new(AlertTokenHandlerSuite)) }

func (s *AlertTokenHandlerSuite) SetupTest() {
	s.incidents = mocks.NewIncidentStore(s.T())
	s.tokens = mocks.NewAlertIngestTokenStore(s.T())
	s.mount(prodops.Deps{Incidents: s.incidents, AlertTokens: s.tokens})

	s.repoID = uuid.New()
	s.token = "tt_alert_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	s.stored = domain.AlertIngestToken{ID: uuid.New(), RepositoryID: s.repoID, Label: "Alertmanager", DefaultEnv: domain.DeployEnvStage}
}

func (s *AlertTokenHandlerSuite) mount(deps prodops.Deps) {
	h := &Handler{legacyAPIKey: alertTestAPIKey, prodOpsSvc: prodops.NewService(deps)}
	s.app = fiber.New(fiber.Config{CaseSensitive: true})
	s.app.Use(h.authMiddleware)
	h.registerProdOpsRoutes(s.app)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AlertTokenHandlerSuite) do(method, path, apiKey string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := s.app.Test(req, -1)
	s.Require().NoError(err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	return resp.StatusCode, out
}

func (s *AlertTokenHandlerSuite) expectIngest(wantEnv string) uuid.UUID {
	incidentID := uuid.New()
	created := domain.Incident{
		ID: incidentID, RepositoryID: s.repoID, Env: wantEnv, Title: "disk full",
		Severity: domain.IncidentSeverityMedium, Status: domain.IncidentStatusOpen, Occurrences: 1,
		FirstSeenAt: time.Now(), LastSeenAt: time.Now(),
	}
	s.incidents.On("FindLive", mock.Anything, s.repoID, wantEnv, mock.Anything).
		Return(domain.Incident{}, domain.ErrIncidentNotFound).Maybe()
	s.incidents.On("Upsert", mock.Anything, mock.MatchedBy(func(in domain.IncidentInput) bool {
		return in.RepositoryID == s.repoID && in.Env == wantEnv && in.Title == "disk full" &&
			in.Source == domain.IncidentSourceWebhook
	})).Return(created, true, nil).Once()
	s.incidents.On("AppendEvent", mock.Anything, incidentID, mock.Anything, mock.Anything).Return(nil)
	s.incidents.On("History", mock.Anything, s.repoID, mock.Anything, mock.Anything).Return(nil, nil)
	s.incidents.On("UpdateRemedy", mock.Anything, incidentID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(created, nil)
	return incidentID
}

func (s *AlertTokenHandlerSuite) TestAValidTokenIngestsWithoutTheAPIKey() {
	tests := []struct {
		name    string
		query   string
		wantEnv string
	}{
		{name: "the token's default env", query: "", wantEnv: domain.DeployEnvStage},
		{name: "?env= over the default", query: "?env=preprod", wantEnv: domain.DeployEnvPreProd},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.tokens.On("FindByHash", mock.Anything, tokenHash(s.token)).Return(s.stored, nil).Once()
			s.tokens.On("MarkUsed", mock.Anything, s.stored.ID).Return(nil).Once()
			incidentID := s.expectIngest(tt.wantEnv)

			status, body := s.do("POST", alertIngestPrefix+s.token+tt.query, "",
				[]byte(`{"title":"disk full","severity":"warning"}`))

			s.Equal(fiber.StatusCreated, status, string(body))
			var got map[string]any
			s.Require().NoError(json.Unmarshal(body, &got))
			s.Equal(map[string]any{"incident_id": incidentID.String()}, got)
		})
	}
}

func (s *AlertTokenHandlerSuite) TestAnUnknownTokenIsABare404() {
	unknown := "tt_alert_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	s.tokens.On("FindByHash", mock.Anything, tokenHash(unknown)).
		Return(domain.AlertIngestToken{}, fmt.Errorf("find: %w", port.ErrNotFound)).Once()

	for _, token := range []string{unknown, "not-a-token"} {
		status, body := s.do("POST", alertIngestPrefix+token, "", []byte(`{"title":"x"}`))
		s.Equal(fiber.StatusNotFound, status)
		s.NotContains(string(body), "{")
		s.NotContains(strings.ToLower(string(body)), "token")
	}
}

func (s *AlertTokenHandlerSuite) TestADeclaredContentLengthOverTheCapIsRefusedBeforeTheLookup() {
	status, _ := s.do("POST", alertIngestPrefix+s.token, "", bytes.Repeat([]byte("a"), maxAlertIngestBody+1))

	s.Equal(fiber.StatusRequestEntityTooLarge, status)
	s.tokens.AssertNotCalled(s.T(), "FindByHash", mock.Anything, mock.Anything)
}

func (s *AlertTokenHandlerSuite) TestAPayloadThatIsNotJSONIs400AndDoesNotStampTheToken() {
	s.tokens.On("FindByHash", mock.Anything, tokenHash(s.token)).Return(s.stored, nil).Once()

	status, body := s.do("POST", alertIngestPrefix+s.token, "", []byte(`not json`))

	s.Equal(fiber.StatusBadRequest, status)
	s.Contains(string(body), "invalid JSON payload")
	s.tokens.AssertNotCalled(s.T(), "MarkUsed", mock.Anything, mock.Anything)
}

func (s *AlertTokenHandlerSuite) TestManagingTokensNeedsTheAPIKey() {
	base := "/v1/repositories/" + s.repoID.String() + "/alert-tokens"
	status, _ := s.do("POST", base, "", []byte(`{"label":"Grafana"}`))
	s.Equal(fiber.StatusUnauthorized, status)
	status, _ = s.do("GET", base, "", nil)
	s.Equal(fiber.StatusUnauthorized, status)
	status, _ = s.do("DELETE", base+"/"+uuid.NewString(), "", nil)
	s.Equal(fiber.StatusUnauthorized, status)
}

func (s *AlertTokenHandlerSuite) TestCreateShowsTheTokenOnceAndListNeverDoes() {
	base := "/v1/repositories/" + s.repoID.String() + "/alert-tokens"
	created := domain.AlertIngestToken{ID: uuid.New(), RepositoryID: s.repoID, Label: "Grafana", DefaultEnv: "prod", CreatedAt: time.Now()}
	var storedHash string
	s.tokens.On("Create", mock.Anything, s.repoID, "Grafana", mock.AnythingOfType("string"), "prod").
		Run(func(args mock.Arguments) { storedHash = args.String(3) }).Return(created, nil).Once()
	s.tokens.On("List", mock.Anything, s.repoID).Return([]domain.AlertIngestToken{created}, nil).Once()

	status, body := s.do("POST", base, alertTestAPIKey, []byte(`{"label":"Grafana"}`))
	s.Require().Equal(fiber.StatusCreated, status, string(body))
	var got struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		DefaultEnv string `json:"default_env"`
		Token      string `json:"token"`
		IngestPath string `json:"ingest_path"`
	}
	s.Require().NoError(json.Unmarshal(body, &got))
	s.Equal(created.ID.String(), got.ID)
	s.Equal("prod", got.DefaultEnv)
	s.Equal(tokenHash(got.Token), storedHash)
	s.Equal(alertIngestPrefix+got.Token, got.IngestPath)

	status, body = s.do("GET", base, alertTestAPIKey, nil)
	s.Require().Equal(fiber.StatusOK, status)
	s.NotContains(string(body), got.Token)
	s.NotContains(string(body), storedHash)
	var list struct {
		Tokens []map[string]any `json:"tokens"`
	}
	s.Require().NoError(json.Unmarshal(body, &list))
	s.Require().Len(list.Tokens, 1)
	for _, key := range []string{"id", "label", "default_env", "created_at", "last_used_at"} {
		s.Contains(list.Tokens[0], key)
	}
	s.Len(list.Tokens[0], 5)
}

func (s *AlertTokenHandlerSuite) TestCreateRejectsAMissingLabel() {
	status, _ := s.do("POST", "/v1/repositories/"+s.repoID.String()+"/alert-tokens", alertTestAPIKey, []byte(`{"label":" "}`))
	s.Equal(fiber.StatusBadRequest, status)
}

func (s *AlertTokenHandlerSuite) TestDeleteIsScopedToTheRepository() {
	base := "/v1/repositories/" + s.repoID.String() + "/alert-tokens/"
	id := uuid.New()
	s.tokens.On("Delete", mock.Anything, s.repoID, id).Return(nil).Once()
	status, _ := s.do("DELETE", base+id.String(), alertTestAPIKey, nil)
	s.Equal(fiber.StatusNoContent, status)

	otherRepo := uuid.New()
	s.tokens.On("Delete", mock.Anything, otherRepo, id).Return(fmt.Errorf("delete: %w", port.ErrNotFound)).Once()
	status, _ = s.do("DELETE", "/v1/repositories/"+otherRepo.String()+"/alert-tokens/"+id.String(), alertTestAPIKey, nil)
	s.Equal(fiber.StatusNotFound, status, "a token of another repository is not there")

	status, _ = s.do("DELETE", "/v1/alert-tokens/"+id.String(), alertTestAPIKey, nil)
	s.Equal(fiber.StatusNotFound, status, "the unscoped route is gone")
}

func (s *AlertTokenHandlerSuite) TestWithoutATokenStoreManagementIs503() {
	s.mount(prodops.Deps{Incidents: s.incidents})
	base := "/v1/repositories/" + s.repoID.String() + "/alert-tokens"

	status, _ := s.do("POST", base, alertTestAPIKey, []byte(`{"label":"Grafana"}`))
	s.Equal(fiber.StatusServiceUnavailable, status)
	status, _ = s.do("GET", base, alertTestAPIKey, nil)
	s.Equal(fiber.StatusServiceUnavailable, status)
	status, _ = s.do("DELETE", base+"/"+uuid.NewString(), alertTestAPIKey, nil)
	s.Equal(fiber.StatusServiceUnavailable, status)
}

func (s *AlertTokenHandlerSuite) TestAChunkedBodyOverTheCapIs413() {
	req := httptest.NewRequest("POST", alertIngestPrefix+s.token, bytes.NewReader(bytes.Repeat([]byte("a"), maxAlertIngestBody+1)))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	resp, err := s.app.Test(req, -1)
	s.Require().NoError(err)
	_ = resp.Body.Close()
	s.Equal(fiber.StatusRequestEntityTooLarge, resp.StatusCode)

	s.tokens.AssertNotCalled(s.T(), "FindByHash", mock.Anything, mock.Anything)
}

func (s *AlertTokenHandlerSuite) TestATokenOverItsRequestRateIs429() {
	s.mount(prodops.Deps{Incidents: s.incidents, AlertTokens: s.tokens,
		AlertLimits: domain.AlertIngestLimits{RequestsPerMinute: 1, Burst: 1}})
	s.tokens.On("FindByHash", mock.Anything, tokenHash(s.token)).Return(s.stored, nil).Twice()
	s.tokens.On("MarkUsed", mock.Anything, s.stored.ID).Return(nil).Once()
	s.expectIngest(domain.DeployEnvStage)

	status, _ := s.do("POST", alertIngestPrefix+s.token, "", []byte(`{"title":"disk full"}`))
	s.Equal(fiber.StatusCreated, status)
	status, body := s.do("POST", alertIngestPrefix+s.token, "", []byte(`{"title":"disk full"}`))
	s.Equal(fiber.StatusTooManyRequests, status)
	s.JSONEq(`{"error":"rate_limited"}`, string(body))
}

func (s *AlertTokenHandlerSuite) TestANewIncidentOverTheHourlyCapIs429() {
	s.mount(prodops.Deps{Incidents: s.incidents, AlertTokens: s.tokens,
		AlertLimits: domain.AlertIngestLimits{NewIncidentsPerHour: 1}})
	s.tokens.On("FindByHash", mock.Anything, tokenHash(s.token)).Return(s.stored, nil).Twice()
	s.tokens.On("MarkUsed", mock.Anything, s.stored.ID).Return(nil).Once()
	s.expectIngest(domain.DeployEnvStage)

	status, _ := s.do("POST", alertIngestPrefix+s.token, "", []byte(`{"title":"disk full"}`))
	s.Equal(fiber.StatusCreated, status)
	status, body := s.do("POST", alertIngestPrefix+s.token, "", []byte(`{"title":"a different alert"}`))
	s.Equal(fiber.StatusTooManyRequests, status)
	s.JSONEq(`{"error":"rate_limited"}`, string(body))
}

func (s *AlertTokenHandlerSuite) TestTheExistingIngestRouteStillNeedsTheKeyAndSharesTheSamePath() {
	path := "/v1/repositories/" + s.repoID.String() + "/incidents?env=stage"
	status, _ := s.do("POST", path, "", []byte(`{"title":"disk full"}`))
	s.Equal(fiber.StatusUnauthorized, status)

	incidentID := s.expectIngest(domain.DeployEnvStage)
	status, body := s.do("POST", path, alertTestAPIKey, []byte(`{"title":"disk full"}`))
	s.Equal(fiber.StatusCreated, status, string(body))
	s.Contains(string(body), incidentID.String())

	status, body = s.do("POST", path, alertTestAPIKey, []byte(`not json`))
	s.Equal(fiber.StatusBadRequest, status)
	s.Contains(string(body), "invalid JSON payload")
}

func TestOnlyASingleSegmentUnderTheIngestPrefixIsPublic(t *testing.T) {
	h := &Handler{}
	tests := []struct {
		path   string
		public bool
	}{
		{path: "/v1/alerts/ingest/tt_alert_abc", public: true},
		{path: "/v1/alerts/ingest/", public: false},
		{path: "/v1/alerts/ingest", public: false},
		{path: "/v1/alerts/ingest/a/b", public: false},
		{path: "/v1/alerts/other", public: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := h.isPublicPath(tt.path); got != tt.public {
				t.Errorf("isPublicPath(%q) = %v, want %v", tt.path, got, tt.public)
			}
		})
	}
}

func TestRedactPathHidesTheIngestToken(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/v1/alerts/ingest/tt_alert_secret", want: "/v1/alerts/ingest/[redacted]"},
		{in: "/v1/alerts/ingest/tt_alert_secret/extra", want: "/v1/alerts/ingest/[redacted]"},
		{in: "/V1/Alerts/Ingest/tt_alert_secret", want: "/v1/alerts/ingest/[redacted]"},
		{in: "/v1/incidents/abc", want: "/v1/incidents/abc"},
	}
	for _, tt := range tests {
		if got := RedactPath(tt.in); got != tt.want {
			t.Errorf("RedactPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestALoggedRefusalOnTheIngestPathCarriesNoToken(t *testing.T) {
	buf := captureHTTPLogs(t)
	app := fiber.New()
	app.Post(alertIngestPrefix+":token", func(c *fiber.Ctx) error {
		return internalError(c, fmt.Errorf("wrapped: %w", domain.ErrProviderUnavailable))
	})

	resp, err := app.Test(httptest.NewRequest("POST", alertIngestPrefix+"tt_alert_secret", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if strings.Contains(buf.String(), "tt_alert_secret") {
		t.Errorf("the token reached the log:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "[redacted]") {
		t.Errorf("expected the redacted path in the log:\n%s", buf.String())
	}
}
