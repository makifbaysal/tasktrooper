package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	mcpapp "github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const oauthTestAPIKey = "server-api-key"

type MCPOAuthHandlerSuite struct {
	suite.Suite
	store      *mocks.MCPStore
	oauthStore *mocks.MCPOAuthStore
	provider   *mocks.MCPOAuthProvider
	app        *fiber.App
}

func (s *MCPOAuthHandlerSuite) SetupTest() {
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.store = mocks.NewMCPStore(s.T())
	s.oauthStore = mocks.NewMCPOAuthStore(s.T())
	s.provider = mocks.NewMCPOAuthProvider(s.T())

	svc := mcpapp.NewService(s.store, cipher, nil)
	svc.EnableOAuth(s.oauthStore, s.provider, func() string { return "http://127.0.0.1:43123" + MCPOAuthCallbackPath })

	h := &Handler{legacyAPIKey: oauthTestAPIKey, mcpSvc: svc}
	s.app = fiber.New()
	s.app.Use(h.authMiddleware)
	h.registerMCPRoutes(s.app)
}

func (s *MCPOAuthHandlerSuite) do(method, target, token string) (int, nethttp.Header, string) {
	req := httptest.NewRequest(method, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.app.Test(req)
	s.Require().NoError(err)
	body, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	return resp.StatusCode, resp.Header, string(body)
}

func (s *MCPOAuthHandlerSuite) TestTheCallbackIsADeliberatePublicServerPath() {
	h := &Handler{legacyAPIKey: oauthTestAPIKey}
	s.True(h.isPublicPath(MCPOAuthCallbackPath))
	s.True(isServerPath(MCPOAuthCallbackPath), "the SPA fallback must not answer it")
	s.False(h.isPublicPath("/v1/mcp/servers/figma/oauth/start"))
}

func (s *MCPOAuthHandlerSuite) TestStartNeedsTheAPITokenAndTheCallbackDoesNot() {
	status, _, _ := s.do("POST", "/v1/mcp/servers/figma/oauth/start", "")
	s.Equal(fiber.StatusUnauthorized, status)

	server := domain.MCPServer{ID: "figma", Enabled: true, Transport: "http", URL: "https://mcp.figma.example/mcp"}
	meta := domain.MCPOAuthMetadata{
		Resource:              server.URL,
		AuthorizationEndpoint: "https://auth.figma.example/authorize",
		TokenEndpoint:         "https://auth.figma.example/token",
		RegistrationEndpoint:  "https://auth.figma.example/register",
	}
	s.store.On("Get", mock.Anything, "figma").Return(server, nil)
	s.store.On("ListSecrets", mock.Anything, "figma").Return([]port.MCPSecretRecord(nil), nil)
	s.provider.On("Discover", mock.Anything, server.URL).Return(meta, nil)
	s.oauthStore.On("GetMCPOAuth", mock.Anything, "figma").Return(port.MCPOAuthRecord{}, false, nil)
	s.provider.On("Register", mock.Anything, meta, mock.Anything).Return(domain.MCPOAuthClient{ClientID: "dyn-1", AuthMethod: "none"}, nil)

	status, _, body := s.do("POST", "/v1/mcp/servers/figma/oauth/start", oauthTestAPIKey)
	s.Require().Equal(fiber.StatusOK, status, body)
	var started domain.MCPOAuthStartResponse
	s.Require().NoError(json.Unmarshal([]byte(body), &started))
	authURL, err := url.Parse(started.AuthorizationURL)
	s.Require().NoError(err)
	state := authURL.Query().Get("state")
	s.Require().NotEmpty(state)

	s.provider.On("Exchange", mock.Anything, meta, mock.Anything, "the-code", mock.Anything, mock.Anything).
		Return(domain.MCPOAuthToken{AccessToken: "at-1"}, nil)
	s.oauthStore.On("SaveMCPOAuth", mock.Anything, mock.MatchedBy(func(rec port.MCPOAuthRecord) bool {
		return rec.ServerID == "figma" && rec.HasTokens()
	})).Return(nil).Once()

	callback := MCPOAuthCallbackPath + "?state=" + url.QueryEscape(state) + "&code=the-code"
	status, headers, page := s.do("GET", callback, "")
	s.Equal(fiber.StatusOK, status)
	s.Contains(page, "Connected — you can close this tab")
	s.Contains(page, "<code>figma</code>")
	s.Contains(headers.Get("Content-Type"), "text/html")
	s.Contains(headers.Get("Content-Security-Policy"), "default-src 'none'")
	s.Equal("no-store", headers.Get("Cache-Control"))
	s.NotContains(page, "<script")
	s.NotContains(page, "src=")
	s.NotContains(page, "href=")

	status, _, page = s.do("GET", callback, "")
	s.Equal(fiber.StatusBadRequest, status, "a state is good for one callback only")
	s.Contains(page, "Could not connect")
	s.NotContains(page, "the-code")
}

func (s *MCPOAuthHandlerSuite) TestAnErrorCallbackEscapesWhatItEchoes() {
	status, _, page := s.do("GET", MCPOAuthCallbackPath+"?state=nope&error=%3Cscript%3Ealert(1)%3C%2Fscript%3E", "")
	s.Equal(fiber.StatusBadRequest, status)
	s.NotContains(page, "<script>alert(1)")
	s.True(strings.Contains(page, "unknown, expired or was already used"))
}

func (s *MCPOAuthHandlerSuite) TestAServerWithoutRegistrationAsksForAClientID() {
	server := domain.MCPServer{ID: "gh", Enabled: true, Transport: "http", URL: "https://api.example.com/mcp"}
	s.store.On("Get", mock.Anything, "gh").Return(server, nil)
	s.store.On("ListSecrets", mock.Anything, "gh").Return([]port.MCPSecretRecord(nil), nil)
	s.provider.On("Discover", mock.Anything, server.URL).Return(domain.MCPOAuthMetadata{
		AuthorizationEndpoint: "https://auth.example.com/authorize", TokenEndpoint: "https://auth.example.com/token",
	}, nil)
	s.oauthStore.On("GetMCPOAuth", mock.Anything, "gh").Return(port.MCPOAuthRecord{}, false, nil)

	status, _, body := s.do("POST", "/v1/mcp/servers/gh/oauth/start", oauthTestAPIKey)
	s.Equal(fiber.StatusBadRequest, status)
	var resp errorResponse
	s.Require().NoError(json.Unmarshal([]byte(body), &resp))
	s.Equal("oauth_client_required", resp.Error.Type)
}

func (s *MCPOAuthHandlerSuite) TestDisconnectForgetsTheSignIn() {
	s.store.On("Get", mock.Anything, "figma").Return(domain.MCPServer{ID: "figma", Transport: "http"}, nil)
	s.oauthStore.On("GetMCPOAuth", mock.Anything, "figma").Return(port.MCPOAuthRecord{ServerID: "figma", ClientID: "dyn", ClientDynamic: true}, true, nil)
	s.oauthStore.On("DeleteMCPOAuth", mock.Anything, "figma").Return(nil)

	status, _, _ := s.do("POST", "/v1/mcp/servers/figma/oauth/disconnect", oauthTestAPIKey)
	s.Equal(fiber.StatusNoContent, status)
}

func TestMCPOAuthHandlerSuite(t *testing.T) {
	suite.Run(t, new(MCPOAuthHandlerSuite))
}
