package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const redirectURI = "http://127.0.0.1:43123/oauth/mcp/callback"

type fakeOAuthStore struct {
	mu      sync.Mutex
	records map[string]port.MCPOAuthRecord
}

func (f *fakeOAuthStore) GetMCPOAuth(_ context.Context, serverID string) (port.MCPOAuthRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[serverID]
	return rec, ok, nil
}

func (f *fakeOAuthStore) SaveMCPOAuth(_ context.Context, rec port.MCPOAuthRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[rec.ServerID] = rec
	return nil
}

func (f *fakeOAuthStore) DeleteMCPOAuth(_ context.Context, serverID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, serverID)
	return nil
}

func (f *fakeOAuthStore) get(serverID string) (port.MCPOAuthRecord, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[serverID]
	return rec, ok
}

type OAuthSuite struct {
	suite.Suite
	store    *fakeMCPStore
	oauth    *fakeOAuthStore
	provider *mocks.MCPOAuthProvider
	cipher   *secrets.Cipher
	svc      *mcp.Service
	now      time.Time
	reloads  chan struct{}
	meta     domain.MCPOAuthMetadata
}

func (s *OAuthSuite) SetupTest() {
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.cipher = cipher
	s.store = newFakeMCPStore()
	s.store.servers["figma"] = domain.MCPServer{ID: "figma", Enabled: true, Transport: "http", URL: "https://mcp.figma.example/mcp", Access: domain.MCPAccessListed}
	s.store.servers["local"] = domain.MCPServer{ID: "local", Enabled: true, Transport: "stdio", Command: "npx"}
	s.oauth = &fakeOAuthStore{records: map[string]port.MCPOAuthRecord{}}
	s.provider = mocks.NewMCPOAuthProvider(s.T())
	s.now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	s.reloads = make(chan struct{}, 4)
	s.svc = mcp.NewService(s.store, s.cipher, func([]domain.MCPServerConfig) error {
		s.reloads <- struct{}{}
		return nil
	})
	s.svc.EnableOAuth(s.oauth, s.provider, func() string { return redirectURI }, mcp.OAuthClock(func() time.Time { return s.now }))
	s.meta = domain.MCPOAuthMetadata{
		Resource:              "https://mcp.figma.example/mcp",
		Issuer:                "https://auth.figma.example",
		AuthorizationEndpoint: "https://auth.figma.example/authorize?prompt=consent",
		TokenEndpoint:         "https://auth.figma.example/token",
		RegistrationEndpoint:  "https://auth.figma.example/register",
		Scopes:                []string{"files:read"},
	}
}

func pkceChallengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (s *OAuthSuite) start() url.Values {
	resp, err := s.svc.StartOAuth(context.Background(), "figma", domain.MCPOAuthStartRequest{})
	s.Require().NoError(err)
	u, err := url.Parse(resp.AuthorizationURL)
	s.Require().NoError(err)
	return u.Query()
}

func (s *OAuthSuite) expectDiscoveryAndRegistration() {
	s.provider.On("Discover", mock.Anything, "https://mcp.figma.example/mcp").Return(s.meta, nil)
	s.provider.On("Register", mock.Anything, s.meta, redirectURI).
		Return(domain.MCPOAuthClient{ClientID: "dyn-1", AuthMethod: "none", Dynamic: true}, nil)
}

func (s *OAuthSuite) TestSignInRegistersAClientAndRedeemsTheCodeWithTheMatchingVerifier() {
	s.expectDiscoveryAndRegistration()
	q := s.start()

	s.Equal("code", q.Get("response_type"))
	s.Equal("dyn-1", q.Get("client_id"))
	s.Equal(redirectURI, q.Get("redirect_uri"))
	s.Equal("S256", q.Get("code_challenge_method"))
	s.Equal("https://mcp.figma.example/mcp", q.Get("resource"))
	s.Equal("files:read", q.Get("scope"))
	s.Equal("consent", q.Get("prompt"), "the endpoint's own query survives")
	s.Len(q.Get("state"), 43)
	challenge := q.Get("code_challenge")

	s.provider.On("Exchange", mock.Anything, s.meta,
		domain.MCPOAuthClient{ClientID: "dyn-1", AuthMethod: "none", Dynamic: true, RedirectURI: redirectURI},
		"the-code", mock.MatchedBy(func(verifier string) bool { return pkceChallengeOf(verifier) == challenge }), redirectURI).
		Return(domain.MCPOAuthToken{AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: s.now.Add(time.Hour)}, nil)

	serverID, err := s.svc.CompleteOAuth(context.Background(), domain.MCPOAuthCallback{
		State: q.Get("state"), Code: "the-code", Issuer: "https://auth.figma.example",
	})
	s.Require().NoError(err)
	s.Equal("figma", serverID)

	rec, ok := s.oauth.get("figma")
	s.Require().True(ok)
	s.NotContains(string(rec.AccessToken), "at-1", "tokens are stored encrypted")
	s.NotContains(string(rec.RefreshToken), "rt-1")
	s.Equal("dyn-1", rec.ClientID)
	s.True(rec.ClientDynamic)

	select {
	case <-s.reloads:
	case <-time.After(2 * time.Second):
		s.Fail("a completed sign-in reconnects the servers")
	}

	views, err := s.svc.ListViews(context.Background(), nil)
	s.Require().NoError(err)
	s.Equal(domain.MCPAuthOAuthConnected, viewOf(views, "figma").Auth)
	s.Equal(domain.MCPAuthNone, viewOf(views, "local").Auth)

	token, err := s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.Require().NoError(err)
	s.Equal("at-1", token)
}

func (s *OAuthSuite) TestTheCallbackStateIsSingleUseShortLivedAndBound() {
	tests := []struct {
		name     string
		callback func(state string) domain.MCPOAuthCallback
		advance  time.Duration
	}{
		{name: "unknown state", callback: func(string) domain.MCPOAuthCallback {
			return domain.MCPOAuthCallback{State: "forged", Code: "c"}
		}},
		{name: "empty state", callback: func(string) domain.MCPOAuthCallback {
			return domain.MCPOAuthCallback{Code: "c"}
		}},
		{name: "expired state", advance: 11 * time.Minute, callback: func(state string) domain.MCPOAuthCallback {
			return domain.MCPOAuthCallback{State: state, Code: "c"}
		}},
		{name: "a different issuer", callback: func(state string) domain.MCPOAuthCallback {
			return domain.MCPOAuthCallback{State: state, Code: "c", Issuer: "https://evil.example"}
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.expectDiscoveryAndRegistration()
			state := s.start().Get("state")
			s.now = s.now.Add(tt.advance)

			_, err := s.svc.CompleteOAuth(context.Background(), tt.callback(state))
			s.ErrorIs(err, domain.ErrMCPOAuthState)
			s.provider.AssertNotCalled(s.T(), "Exchange", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func (s *OAuthSuite) TestAStateCannotBeReplayedEvenAfterAFailure() {
	s.expectDiscoveryAndRegistration()
	state := s.start().Get("state")

	_, err := s.svc.CompleteOAuth(context.Background(), domain.MCPOAuthCallback{State: state, Error: "access_denied"})
	s.ErrorIs(err, domain.ErrMCPOAuthDenied)

	_, err = s.svc.CompleteOAuth(context.Background(), domain.MCPOAuthCallback{State: state, Code: "late-code"})
	s.ErrorIs(err, domain.ErrMCPOAuthState)
}

func (s *OAuthSuite) TestStartingAgainInvalidatesTheEarlierState() {
	s.expectDiscoveryAndRegistration()
	first := s.start().Get("state")
	second := s.start().Get("state")
	s.NotEqual(first, second)

	_, err := s.svc.CompleteOAuth(context.Background(), domain.MCPOAuthCallback{State: first, Code: "c"})
	s.ErrorIs(err, domain.ErrMCPOAuthState)
}

func (s *OAuthSuite) TestAnEnteredClientIDIsUsedInsteadOfRegistering() {
	meta := s.meta
	meta.RegistrationEndpoint = ""
	s.provider.On("Discover", mock.Anything, mock.Anything).Return(meta, nil)

	resp, err := s.svc.StartOAuth(context.Background(), "figma", domain.MCPOAuthStartRequest{ClientID: " manual-id ", ClientSecret: "sec"})
	s.Require().NoError(err)
	u, _ := url.Parse(resp.AuthorizationURL)
	s.Equal("manual-id", u.Query().Get("client_id"))
	s.provider.AssertNotCalled(s.T(), "Register", mock.Anything, mock.Anything, mock.Anything)
}

func (s *OAuthSuite) TestWithoutRegistrationEndpointOrClientIDTheCallerIsAskedForOne() {
	meta := s.meta
	meta.RegistrationEndpoint = ""
	s.provider.On("Discover", mock.Anything, mock.Anything).Return(meta, nil)

	_, err := s.svc.StartOAuth(context.Background(), "figma", domain.MCPOAuthStartRequest{})
	s.ErrorIs(err, domain.ErrMCPOAuthClientRequired)
	s.Equal(redirectURI, s.svc.OAuthRedirectURI())
}

func (s *OAuthSuite) TestOAuthIsRefusedForStdioServersAndUnsafeAuthorizationURLs() {
	_, err := s.svc.StartOAuth(context.Background(), "local", domain.MCPOAuthStartRequest{})
	s.ErrorIs(err, domain.ErrMCPOAuthNotHTTP)

	meta := s.meta
	meta.AuthorizationEndpoint = "javascript:alert(1)"
	s.provider.On("Discover", mock.Anything, mock.Anything).Return(meta, nil)
	_, err = s.svc.StartOAuth(context.Background(), "figma", domain.MCPOAuthStartRequest{})
	s.ErrorIs(err, domain.ErrMCPOAuthDiscovery)
}

func (s *OAuthSuite) storeTokens(access, refresh string, expiresAt time.Time) {
	rec := port.MCPOAuthRecord{ServerID: "figma", Metadata: s.meta, ClientID: "dyn-1", ClientAuthMethod: "none", ClientDynamic: true}
	var err error
	rec.AccessToken, err = s.cipher.Encrypt(access)
	s.Require().NoError(err)
	if refresh != "" {
		rec.RefreshToken, err = s.cipher.Encrypt(refresh)
		s.Require().NoError(err)
	}
	rec.ExpiresAt = &expiresAt
	s.Require().NoError(s.oauth.SaveMCPOAuth(context.Background(), rec))
}

func (s *OAuthSuite) TestTokenIsRefreshedShortlyBeforeItExpires() {
	s.storeTokens("at-old", "rt-1", s.now.Add(30*time.Second))
	s.provider.On("Refresh", mock.Anything, s.meta, mock.Anything, "rt-1").
		Return(domain.MCPOAuthToken{AccessToken: "at-new", ExpiresAt: s.now.Add(time.Hour)}, nil).Once()

	token, err := s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.Require().NoError(err)
	s.Equal("at-new", token)

	rec, _ := s.oauth.get("figma")
	refresh, err := s.cipher.Decrypt(rec.RefreshToken)
	s.Require().NoError(err)
	s.Equal("rt-1", refresh, "a refresh that returns no new refresh token keeps the old one")

	token, err = s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.Require().NoError(err)
	s.Equal("at-new", token, "a fresh token is served without another refresh")
}

func (s *OAuthSuite) TestARejectedTokenIsRefreshedUnlessANewerOneIsAlreadyStored() {
	s.storeTokens("at-current", "rt-1", s.now.Add(time.Hour))

	token, err := s.svc.MCPAccessToken(context.Background(), "figma", "at-older")
	s.Require().NoError(err)
	s.Equal("at-current", token)
	s.provider.AssertNotCalled(s.T(), "Refresh", mock.Anything, mock.Anything, mock.Anything, mock.Anything)

	s.provider.On("Refresh", mock.Anything, s.meta, mock.Anything, "rt-1").
		Return(domain.MCPOAuthToken{AccessToken: "at-next", RefreshToken: "rt-2"}, nil).Once()
	token, err = s.svc.MCPAccessToken(context.Background(), "figma", "at-current")
	s.Require().NoError(err)
	s.Equal("at-next", token)
}

func (s *OAuthSuite) TestARefusedRefreshMarksTheSignInExpired() {
	s.storeTokens("at-old", "rt-dead", s.now.Add(-time.Minute))
	s.provider.On("Refresh", mock.Anything, mock.Anything, mock.Anything, "rt-dead").
		Return(domain.MCPOAuthToken{}, domain.ErrMCPOAuthGrantRejected).Once()

	_, err := s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.ErrorIs(err, domain.ErrMCPOAuthExpired)

	_, err = s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.ErrorIs(err, domain.ErrMCPOAuthExpired, "no further refresh is attempted")

	views, err := s.svc.ListViews(context.Background(), nil)
	s.Require().NoError(err)
	s.Equal(domain.MCPAuthOAuthExpired, viewOf(views, "figma").Auth)
}

func (s *OAuthSuite) TestAServerThatAnswered401WithoutASignInNeedsOne() {
	views, err := s.svc.ListViews(context.Background(), []map[string]interface{}{
		{"id": "figma", "connected": false, "auth_required": true, "last_error": "Unauthorized"},
	})
	s.Require().NoError(err)
	s.Equal(domain.MCPAuthOAuthNeeded, viewOf(views, "figma").Auth)
}

func (s *OAuthSuite) TestDisconnectDropsADynamicClientButKeepsAnEnteredOne() {
	s.storeTokens("at", "rt", s.now.Add(time.Hour))
	s.Require().NoError(s.svc.DisconnectOAuth(context.Background(), "figma"))
	_, ok := s.oauth.get("figma")
	s.False(ok)

	secret, err := s.cipher.Encrypt("sec")
	s.Require().NoError(err)
	access, err := s.cipher.Encrypt("at")
	s.Require().NoError(err)
	s.Require().NoError(s.oauth.SaveMCPOAuth(context.Background(), port.MCPOAuthRecord{
		ServerID: "figma", ClientID: "manual", ClientSecret: secret, AccessToken: access,
	}))
	s.Require().NoError(s.svc.DisconnectOAuth(context.Background(), "figma"))
	rec, ok := s.oauth.get("figma")
	s.Require().True(ok)
	s.Equal("manual", rec.ClientID)
	s.Equal(secret, rec.ClientSecret)
	s.False(rec.HasTokens())

	token, err := s.svc.MCPAccessToken(context.Background(), "figma", "")
	s.NoError(err)
	s.Empty(token)
}

func (s *OAuthSuite) TestPointingTheServerElsewhereForgetsItsSignIn() {
	s.storeTokens("at", "rt", s.now.Add(time.Hour))

	_, err := s.svc.Update(context.Background(), "figma", domain.UpdateMCPServerRequest{
		Enabled: true, Transport: "http", URL: "https://other.example/mcp",
	})
	s.Require().NoError(err)
	_, ok := s.oauth.get("figma")
	s.False(ok)
}

func viewOf(views []domain.MCPServerView, id string) domain.MCPServerView {
	for _, v := range views {
		if v.ID == id {
			return v
		}
	}
	return domain.MCPServerView{}
}

func TestOAuthSuite(t *testing.T) {
	suite.Run(t, new(OAuthSuite))
}
