package mcpoauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func loopbackPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	return p
}

type recordedRequest struct {
	Method string
	Path   string
	Form   url.Values
	Header http.Header
	Body   []byte
}

// authServer is a scripted authorization server: metadata, registration and
// a token endpoint, recording every request it gets.
type authServer struct {
	srv       *httptest.Server
	mu        sync.Mutex
	requests  []recordedRequest
	methods   []string
	tokenCode int
	tokenBody string
}

func newAuthServer(t *testing.T, issuerPath string) *authServer {
	as := &authServer{methods: []string{"S256"}, tokenCode: http.StatusOK}
	mux := http.NewServeMux()
	as.srv = httptest.NewServer(mux)
	t.Cleanup(as.srv.Close)
	issuer := as.srv.URL + issuerPath

	mux.HandleFunc("/.well-known/oauth-authorization-server"+issuerPath, func(w http.ResponseWriter, r *http.Request) {
		as.record(r, nil)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                as.srv.URL + "/authorize",
			"token_endpoint":                        as.srv.URL + "/token",
			"registration_endpoint":                 as.srv.URL + "/register",
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      as.methods,
			"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		as.record(r, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"dyn-client","token_endpoint_auth_method":"none"}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		as.record(r, nil)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(as.tokenCode)
		_, _ = w.Write([]byte(as.tokenBody))
	})
	return as
}

func (as *authServer) record(r *http.Request, body []byte) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.requests = append(as.requests, recordedRequest{
		Method: r.Method, Path: r.URL.Path, Form: r.PostForm, Header: r.Header.Clone(), Body: body,
	})
}

func (as *authServer) last(path string) recordedRequest {
	as.mu.Lock()
	defer as.mu.Unlock()
	for i := len(as.requests) - 1; i >= 0; i-- {
		if as.requests[i].Path == path {
			return as.requests[i]
		}
	}
	return recordedRequest{}
}

type ClientSuite struct {
	suite.Suite
	client *Client
	now    time.Time
}

func (s *ClientSuite) SetupTest() {
	s.now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s.client = New(loopbackPolicy())
	s.client.now = func() time.Time { return s.now }
}

func (s *ClientSuite) TestDiscoveryFollowsTheChallengeToResourceAndServerMetadata() {
	as := newAuthServer(s.T(), "/tenant")
	mux := http.NewServeMux()
	mcp := httptest.NewServer(mux)
	s.T().Cleanup(mcp.Close)
	resource := mcp.URL + "/mcp"
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+mcp.URL+`/meta/prm", scope="files:read design:read"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/meta/prm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              resource,
			"authorization_servers": []string{as.srv.URL + "/tenant"},
			"scopes_supported":      []string{"everything"},
		})
	})

	meta, err := s.client.Discover(context.Background(), resource)
	s.Require().NoError(err)
	s.Equal(domain.MCPOAuthMetadata{
		Resource:              resource,
		Issuer:                as.srv.URL + "/tenant",
		AuthorizationEndpoint: as.srv.URL + "/authorize",
		TokenEndpoint:         as.srv.URL + "/token",
		RegistrationEndpoint:  as.srv.URL + "/register",
		Scopes:                []string{"files:read", "design:read"},
		TokenAuthMethods:      []string{"none", "client_secret_basic"},
	}, meta)
}

func (s *ClientSuite) TestDiscoveryFindsTheWellKnownResourceDocumentWithoutAChallenge() {
	as := newAuthServer(s.T(), "")
	mux := http.NewServeMux()
	mcp := httptest.NewServer(mux)
	s.T().Cleanup(mcp.Close)
	resource := mcp.URL + "/v1/mcp"
	mux.HandleFunc("/v1/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/v1/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              resource,
			"authorization_servers": []string{as.srv.URL},
			"scopes_supported":      []string{"read"},
		})
	})

	meta, err := s.client.Discover(context.Background(), resource)
	s.Require().NoError(err)
	s.Equal(resource, meta.Resource)
	s.Equal(as.srv.URL, meta.Issuer)
	s.Equal([]string{"read"}, meta.Scopes)
	s.Equal(as.srv.URL+"/token", meta.TokenEndpoint)
}

func (s *ClientSuite) TestDiscoveryFallsBackToTheServerOriginAsAuthorizationServer() {
	as := newAuthServer(s.T(), "")
	resource := as.srv.URL + "/mcp"

	meta, err := s.client.Discover(context.Background(), resource)
	s.Require().NoError(err)
	s.Equal(resource, meta.Resource)
	s.Equal(as.srv.URL, meta.Issuer)
	s.Equal(as.srv.URL+"/authorize", meta.AuthorizationEndpoint)
}

func (s *ClientSuite) TestDiscoveryRefusesAnAuthorizationServerWithoutS256() {
	as := newAuthServer(s.T(), "")
	as.methods = []string{"plain"}

	_, err := s.client.Discover(context.Background(), as.srv.URL+"/mcp")
	s.ErrorContains(err, "S256")
}

func (s *ClientSuite) TestDiscoveryNeverDialsWhatTheGuardRefuses() {
	guarded := New(urlguard.PublicOnly())
	_, err := guarded.Discover(context.Background(), "http://127.0.0.1:9/mcp")
	s.Error(err)
}

func (s *ClientSuite) TestEndpointsMustBeHTTPSUnlessLoopback() {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https", url: "https://auth.example.com/token"},
		{name: "loopback http", url: "http://127.0.0.1:8080/token"},
		{name: "remote http", url: "http://auth.example.com/token", wantErr: true},
		{name: "javascript", url: "javascript:alert(1)", wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.client.checkEndpoint(tt.url)
			if tt.wantErr {
				s.Error(err)
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *ClientSuite) TestDynamicRegistrationAsksForAPublicLoopbackClient() {
	as := newAuthServer(s.T(), "")
	meta := domain.MCPOAuthMetadata{RegistrationEndpoint: as.srv.URL + "/register", Scopes: []string{"read"}}

	client, err := s.client.Register(context.Background(), meta, "http://127.0.0.1:5555/oauth/mcp/callback")
	s.Require().NoError(err)
	s.Equal(domain.MCPOAuthClient{
		ClientID: "dyn-client", AuthMethod: "none",
		RedirectURI: "http://127.0.0.1:5555/oauth/mcp/callback", Dynamic: true,
	}, client)

	var sent map[string]any
	s.Require().NoError(json.Unmarshal(as.last("/register").Body, &sent))
	s.Equal([]any{"http://127.0.0.1:5555/oauth/mcp/callback"}, sent["redirect_uris"])
	s.Equal("none", sent["token_endpoint_auth_method"])
	s.Equal([]any{"authorization_code", "refresh_token"}, sent["grant_types"])
	s.Equal("read", sent["scope"])
}

func (s *ClientSuite) TestExchangeSendsTheVerifierAndResource() {
	as := newAuthServer(s.T(), "")
	as.tokenBody = `{"access_token":"at-1","token_type":"bearer","expires_in":3600,"refresh_token":"rt-1","scope":"read"}`
	meta := domain.MCPOAuthMetadata{Resource: "https://mcp.example.com/mcp", TokenEndpoint: as.srv.URL + "/token"}

	tok, err := s.client.Exchange(context.Background(), meta, domain.MCPOAuthClient{ClientID: "dyn-client", AuthMethod: "none"},
		"code-1", "verifier-1", "http://127.0.0.1:5555/oauth/mcp/callback")
	s.Require().NoError(err)
	s.Equal(domain.MCPOAuthToken{
		AccessToken: "at-1", RefreshToken: "rt-1", TokenType: "Bearer", Scope: "read",
		ExpiresAt: s.now.Add(time.Hour),
	}, tok)

	form := as.last("/token").Form
	s.Equal("authorization_code", form.Get("grant_type"))
	s.Equal("code-1", form.Get("code"))
	s.Equal("verifier-1", form.Get("code_verifier"))
	s.Equal("http://127.0.0.1:5555/oauth/mcp/callback", form.Get("redirect_uri"))
	s.Equal("https://mcp.example.com/mcp", form.Get("resource"))
	s.Equal("dyn-client", form.Get("client_id"))
}

func (s *ClientSuite) TestAClientSecretGoesInTheBasicHeaderNotTheForm() {
	as := newAuthServer(s.T(), "")
	as.tokenBody = `{"access_token":"at-2"}`
	meta := domain.MCPOAuthMetadata{TokenEndpoint: as.srv.URL + "/token"}

	_, err := s.client.Refresh(context.Background(), meta,
		domain.MCPOAuthClient{ClientID: "manual", ClientSecret: "s3cret", AuthMethod: "client_secret_basic"}, "rt-1")
	s.Require().NoError(err)

	req := as.last("/token")
	user, pass, ok := (&http.Request{Header: req.Header}).BasicAuth()
	s.True(ok)
	s.Equal("manual", user)
	s.Equal("s3cret", pass)
	s.Empty(req.Form.Get("client_secret"))
	s.Equal("refresh_token", req.Form.Get("grant_type"))
	s.Equal("rt-1", req.Form.Get("refresh_token"))
}

func (s *ClientSuite) TestRefreshTellsARejectedGrantFromAnyOtherFailure() {
	as := newAuthServer(s.T(), "")
	meta := domain.MCPOAuthMetadata{TokenEndpoint: as.srv.URL + "/token"}
	client := domain.MCPOAuthClient{ClientID: "dyn-client", AuthMethod: "none"}

	as.tokenCode, as.tokenBody = http.StatusBadRequest, `{"error":"invalid_grant","error_description":"<script>"}`
	_, err := s.client.Refresh(context.Background(), meta, client, "rt-dead")
	s.True(errors.Is(err, domain.ErrMCPOAuthGrantRejected))
	s.NotContains(err.Error(), "<script>")

	as.tokenCode, as.tokenBody = http.StatusServiceUnavailable, `upstream down`
	_, err = s.client.Refresh(context.Background(), meta, client, "rt-1")
	s.Error(err)
	s.False(errors.Is(err, domain.ErrMCPOAuthGrantRejected))
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}
