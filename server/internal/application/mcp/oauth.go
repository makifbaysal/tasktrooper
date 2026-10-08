package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	oauthStateTTL     = 10 * time.Minute
	oauthMaxPending   = 32
	oauthRefreshSkew  = time.Minute
	oauthReloadBudget = 2 * time.Minute
)

var oauthErrorCode = regexp.MustCompile(`^[a-z_]{1,64}$`)

type pendingAuthorization struct {
	serverID    string
	verifier    string
	redirectURI string
	meta        domain.MCPOAuthMetadata
	client      domain.MCPOAuthClient
	expiresAt   time.Time
}

type oauthFlows struct {
	store    port.MCPOAuthStore
	provider port.MCPOAuthProvider
	redirect func() string
	now      func() time.Time

	mu      sync.Mutex
	pending map[string]pendingAuthorization
	locks   map[string]*sync.Mutex
}

type OAuthOption func(*oauthFlows)

func OAuthClock(now func() time.Time) OAuthOption {
	return func(f *oauthFlows) { f.now = now }
}

// EnableOAuth turns on OAuth sign-in for HTTP servers. redirectURI is read on
// every flow because the loopback port is only known once the listener is
// open, and changes with every launch.
func (s *Service) EnableOAuth(store port.MCPOAuthStore, provider port.MCPOAuthProvider, redirectURI func() string, opts ...OAuthOption) {
	f := &oauthFlows{
		store:    store,
		provider: provider,
		redirect: redirectURI,
		now:      time.Now,
		pending:  make(map[string]pendingAuthorization),
		locks:    make(map[string]*sync.Mutex),
	}
	for _, opt := range opts {
		opt(f)
	}
	s.oauth = f
}

func (s *Service) OAuthRedirectURI() string {
	if s.oauth == nil || s.oauth.redirect == nil {
		return ""
	}
	return s.oauth.redirect()
}

// StartOAuth discovers the server's authorization server, settles on a
// client (the one entered, a reusable registration, or a new dynamic one) and
// returns the URL the person opens to sign in. Nothing is stored until the
// callback brings a code back: the PKCE verifier lives only in memory, bound
// to a single-use state.
func (s *Service) StartOAuth(ctx context.Context, serverID string, req domain.MCPOAuthStartRequest) (domain.MCPOAuthStartResponse, error) {
	f := s.oauth
	if f == nil || s.cipher == nil {
		return domain.MCPOAuthStartResponse{}, domain.ErrMCPOAuthUnavailable
	}
	redirect := s.OAuthRedirectURI()
	if redirect == "" {
		return domain.MCPOAuthStartResponse{}, domain.ErrMCPOAuthUnavailable
	}
	server, err := s.store.Get(ctx, serverID)
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}
	if server.Transport != "http" {
		return domain.MCPOAuthStartResponse{}, domain.ErrMCPOAuthNotHTTP
	}
	cfg, err := s.ResolveRuntimeConfig(ctx, server)
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}

	meta, err := f.provider.Discover(ctx, cfg.URL)
	if err != nil {
		return domain.MCPOAuthStartResponse{}, fmt.Errorf("%w: %v", domain.ErrMCPOAuthDiscovery, err)
	}
	if !browserSafeURL(meta.AuthorizationEndpoint) {
		return domain.MCPOAuthStartResponse{}, fmt.Errorf("%w: the authorization endpoint is not an https URL", domain.ErrMCPOAuthDiscovery)
	}

	client, err := s.oauthClientFor(ctx, serverID, meta, redirect, req)
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}

	verifier, err := randomToken()
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}
	state, err := randomToken()
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}
	authURL, err := authorizationURL(meta, client, redirect, state, pkceChallenge(verifier))
	if err != nil {
		return domain.MCPOAuthStartResponse{}, err
	}

	expiresAt := f.now().Add(oauthStateTTL)
	f.addPending(state, pendingAuthorization{
		serverID:    serverID,
		verifier:    verifier,
		redirectURI: redirect,
		meta:        meta,
		client:      client,
		expiresAt:   expiresAt,
	})
	return domain.MCPOAuthStartResponse{AuthorizationURL: authURL, ExpiresAt: expiresAt}, nil
}

func (s *Service) oauthClientFor(ctx context.Context, serverID string, meta domain.MCPOAuthMetadata, redirect string, req domain.MCPOAuthStartRequest) (domain.MCPOAuthClient, error) {
	f := s.oauth
	if id := strings.TrimSpace(req.ClientID); id != "" {
		secret := strings.TrimSpace(req.ClientSecret)
		return domain.MCPOAuthClient{
			ClientID:     id,
			ClientSecret: secret,
			AuthMethod:   tokenAuthMethod(secret != "", meta.TokenAuthMethods),
		}, nil
	}

	rec, found, err := f.store.GetMCPOAuth(ctx, serverID)
	if err != nil {
		return domain.MCPOAuthClient{}, err
	}
	if found && rec.ClientID != "" {
		reusable := !rec.ClientDynamic ||
			(rec.ClientRedirectURI == redirect &&
				rec.Metadata.Issuer == meta.Issuer &&
				rec.Metadata.RegistrationEndpoint == meta.RegistrationEndpoint)
		if reusable {
			return s.storedClient(rec)
		}
	}

	if meta.RegistrationEndpoint == "" {
		return domain.MCPOAuthClient{}, domain.ErrMCPOAuthClientRequired
	}
	client, err := f.provider.Register(ctx, meta, redirect)
	if err != nil {
		return domain.MCPOAuthClient{}, fmt.Errorf("%w: client registration failed: %v", domain.ErrMCPOAuthDiscovery, err)
	}
	client.Dynamic = true
	client.RedirectURI = redirect
	if client.AuthMethod == "" {
		client.AuthMethod = tokenAuthMethod(client.ClientSecret != "", nil)
	}
	return client, nil
}

// CompleteOAuth redeems the callback. The state is consumed before anything
// else is checked, so a replayed or guessed one never reaches the token
// endpoint and a failed attempt cannot be retried with the same state.
func (s *Service) CompleteOAuth(ctx context.Context, cb domain.MCPOAuthCallback) (string, error) {
	f := s.oauth
	if f == nil || s.cipher == nil {
		return "", domain.ErrMCPOAuthUnavailable
	}
	p, ok := f.take(cb.State)
	if !ok {
		return "", domain.ErrMCPOAuthState
	}
	if cb.Error != "" {
		if oauthErrorCode.MatchString(cb.Error) {
			return p.serverID, fmt.Errorf("%w (%s)", domain.ErrMCPOAuthDenied, cb.Error)
		}
		return p.serverID, domain.ErrMCPOAuthDenied
	}
	if cb.Issuer != "" && p.meta.Issuer != "" && cb.Issuer != p.meta.Issuer {
		return p.serverID, fmt.Errorf("%w: the response came from a different authorization server", domain.ErrMCPOAuthState)
	}
	if strings.TrimSpace(cb.Code) == "" {
		return p.serverID, domain.ErrMCPOAuthExchange
	}

	tok, err := f.provider.Exchange(ctx, p.meta, p.client, cb.Code, p.verifier, p.redirectURI)
	if err != nil {
		return p.serverID, fmt.Errorf("%w: %v", domain.ErrMCPOAuthExchange, err)
	}
	if tok.AccessToken == "" {
		return p.serverID, domain.ErrMCPOAuthExchange
	}

	rec := port.MCPOAuthRecord{
		ServerID:          p.serverID,
		Metadata:          p.meta,
		ClientID:          p.client.ClientID,
		ClientAuthMethod:  p.client.AuthMethod,
		ClientRedirectURI: p.client.RedirectURI,
		ClientDynamic:     p.client.Dynamic,
	}
	if p.client.ClientSecret != "" {
		if rec.ClientSecret, err = s.cipher.Encrypt(p.client.ClientSecret); err != nil {
			return p.serverID, fmt.Errorf("encrypt oauth client secret: %w", err)
		}
	}
	if err := s.applyToken(&rec, tok); err != nil {
		return p.serverID, err
	}

	lock := f.lockFor(p.serverID)
	lock.Lock()
	err = f.store.SaveMCPOAuth(ctx, rec)
	lock.Unlock()
	if err != nil {
		return p.serverID, err
	}

	s.reloadInBackground()
	return p.serverID, nil
}

// DisconnectOAuth forgets the server's tokens. A hand-entered client is kept
// so reconnecting does not ask for it again; a dynamic one is dropped — the
// next sign-in registers afresh.
func (s *Service) DisconnectOAuth(ctx context.Context, serverID string) error {
	f := s.oauth
	if f == nil {
		return domain.ErrMCPOAuthUnavailable
	}
	if _, err := s.store.Get(ctx, serverID); err != nil {
		return err
	}
	f.dropPending(serverID)

	lock := f.lockFor(serverID)
	lock.Lock()
	err := func() error {
		rec, found, err := f.store.GetMCPOAuth(ctx, serverID)
		if err != nil || !found {
			return err
		}
		if rec.ClientDynamic || rec.ClientID == "" {
			return f.store.DeleteMCPOAuth(ctx, serverID)
		}
		clearTokens(&rec)
		return f.store.SaveMCPOAuth(ctx, rec)
	}()
	lock.Unlock()
	if err != nil {
		return err
	}
	return s.reloadStored(ctx)
}

// MCPAccessToken implements port.MCPTokenSource for the MCP client. The token
// is refreshed a minute before it lapses, and whenever the server rejects the
// one it was sent; a refresh the authorization server refuses marks the
// sign-in expired, which the server list reports until someone reconnects.
func (s *Service) MCPAccessToken(ctx context.Context, serverID, rejected string) (string, error) {
	f := s.oauth
	if f == nil {
		return "", nil
	}
	lock := f.lockFor(serverID)
	lock.Lock()
	defer lock.Unlock()

	rec, found, err := f.store.GetMCPOAuth(ctx, serverID)
	if err != nil {
		return "", err
	}
	if !found || !rec.HasTokens() {
		return "", nil
	}
	if rec.Expired {
		return "", domain.ErrMCPOAuthExpired
	}
	if s.cipher == nil {
		return "", domain.ErrMCPOAuthUnavailable
	}
	access, err := s.cipher.Decrypt(rec.AccessToken)
	if err != nil {
		return "", fmt.Errorf("decrypt oauth access token: %w", err)
	}

	now := f.now()
	fresh := rec.ExpiresAt == nil || now.Add(oauthRefreshSkew).Before(*rec.ExpiresAt)
	if fresh && (rejected == "" || access != rejected) {
		return access, nil
	}
	stillValid := rec.ExpiresAt == nil || now.Before(*rec.ExpiresAt)

	if len(rec.RefreshToken) == 0 {
		if rejected == "" && stillValid {
			return access, nil
		}
		return "", s.markOAuthExpired(ctx, rec)
	}
	refresh, err := s.cipher.Decrypt(rec.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("decrypt oauth refresh token: %w", err)
	}
	client, err := s.storedClient(rec)
	if err != nil {
		return "", err
	}
	tok, err := f.provider.Refresh(ctx, rec.Metadata, client, refresh)
	if err != nil {
		if errors.Is(err, domain.ErrMCPOAuthGrantRejected) {
			return "", s.markOAuthExpired(ctx, rec)
		}
		if rejected == "" && stillValid {
			return access, nil
		}
		return "", err
	}
	if tok.AccessToken == "" {
		return "", domain.ErrMCPOAuthExchange
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refresh
	}
	if err := s.applyToken(&rec, tok); err != nil {
		return "", err
	}
	if err := f.store.SaveMCPOAuth(ctx, rec); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

func (s *Service) markOAuthExpired(ctx context.Context, rec port.MCPOAuthRecord) error {
	rec.Expired = true
	if err := s.oauth.store.SaveMCPOAuth(ctx, rec); err != nil {
		return err
	}
	return domain.ErrMCPOAuthExpired
}

func (s *Service) authStatus(ctx context.Context, server domain.MCPServer, health map[string]interface{}) (domain.MCPAuthStatus, error) {
	if server.Transport != "http" {
		return domain.MCPAuthNone, nil
	}
	if f := s.oauth; f != nil {
		rec, found, err := f.store.GetMCPOAuth(ctx, server.ID)
		if err != nil {
			return "", err
		}
		if found && rec.HasTokens() {
			lapsed := rec.ExpiresAt != nil && !f.now().Before(*rec.ExpiresAt) && len(rec.RefreshToken) == 0
			if rec.Expired || lapsed {
				return domain.MCPAuthOAuthExpired, nil
			}
			return domain.MCPAuthOAuthConnected, nil
		}
	}
	if authRequired, _ := health["auth_required"].(bool); authRequired {
		return domain.MCPAuthOAuthNeeded, nil
	}
	return domain.MCPAuthNone, nil
}

func (s *Service) forgetOAuth(ctx context.Context, serverID string) error {
	f := s.oauth
	if f == nil {
		return nil
	}
	f.dropPending(serverID)
	lock := f.lockFor(serverID)
	lock.Lock()
	defer lock.Unlock()
	return f.store.DeleteMCPOAuth(ctx, serverID)
}

func (s *Service) dropPendingOAuth(serverID string) {
	if s.oauth != nil {
		s.oauth.dropPending(serverID)
	}
}

func (s *Service) storedClient(rec port.MCPOAuthRecord) (domain.MCPOAuthClient, error) {
	client := domain.MCPOAuthClient{
		ClientID:    rec.ClientID,
		AuthMethod:  rec.ClientAuthMethod,
		RedirectURI: rec.ClientRedirectURI,
		Dynamic:     rec.ClientDynamic,
	}
	if len(rec.ClientSecret) > 0 {
		if s.cipher == nil {
			return domain.MCPOAuthClient{}, domain.ErrMCPOAuthUnavailable
		}
		secret, err := s.cipher.Decrypt(rec.ClientSecret)
		if err != nil {
			return domain.MCPOAuthClient{}, fmt.Errorf("decrypt oauth client secret: %w", err)
		}
		client.ClientSecret = secret
	}
	return client, nil
}

func (s *Service) applyToken(rec *port.MCPOAuthRecord, tok domain.MCPOAuthToken) error {
	access, err := s.cipher.Encrypt(tok.AccessToken)
	if err != nil {
		return fmt.Errorf("encrypt oauth access token: %w", err)
	}
	rec.AccessToken = access
	rec.RefreshToken = nil
	if tok.RefreshToken != "" {
		if rec.RefreshToken, err = s.cipher.Encrypt(tok.RefreshToken); err != nil {
			return fmt.Errorf("encrypt oauth refresh token: %w", err)
		}
	}
	rec.TokenType = tok.TokenType
	rec.Scope = tok.Scope
	rec.ExpiresAt = nil
	if !tok.ExpiresAt.IsZero() {
		expiresAt := tok.ExpiresAt
		rec.ExpiresAt = &expiresAt
	}
	rec.Expired = false
	return nil
}

// reloadInBackground reconnects every server after a sign-in without holding
// the browser tab open for it; the UI polls the list for the result.
func (s *Service) reloadInBackground() {
	if s.reload == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), oauthReloadBudget)
		defer cancel()
		_ = s.reloadStored(ctx)
	}()
}

func clearTokens(rec *port.MCPOAuthRecord) {
	rec.AccessToken = nil
	rec.RefreshToken = nil
	rec.TokenType = ""
	rec.Scope = ""
	rec.ExpiresAt = nil
	rec.Expired = false
}

func (f *oauthFlows) lockFor(serverID string) *sync.Mutex {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, ok := f.locks[serverID]
	if !ok {
		lock = &sync.Mutex{}
		f.locks[serverID] = lock
	}
	return lock
}

func (f *oauthFlows) addPending(state string, p pendingAuthorization) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for key, existing := range f.pending {
		if existing.serverID == p.serverID || !now.Before(existing.expiresAt) {
			delete(f.pending, key)
		}
	}
	for len(f.pending) >= oauthMaxPending {
		var oldest string
		for key, existing := range f.pending {
			if oldest == "" || existing.expiresAt.Before(f.pending[oldest].expiresAt) {
				oldest = key
			}
		}
		delete(f.pending, oldest)
	}
	f.pending[state] = p
}

func (f *oauthFlows) take(state string) (pendingAuthorization, bool) {
	if state == "" {
		return pendingAuthorization{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pending[state]
	delete(f.pending, state)
	if !ok || !f.now().Before(p.expiresAt) {
		return pendingAuthorization{}, false
	}
	return p, true
}

func (f *oauthFlows) dropPending(serverID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, p := range f.pending {
		if p.serverID == serverID {
			delete(f.pending, key)
		}
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oauth secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func authorizationURL(meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, redirect, state, challenge string) (string, error) {
	u, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("%w: invalid authorization endpoint", domain.ErrMCPOAuthDiscovery)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if meta.Resource != "" {
		q.Set("resource", meta.Resource)
	}
	if len(meta.Scopes) > 0 {
		q.Set("scope", strings.Join(meta.Scopes, " "))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// browserSafeURL is the only kind of URL handed to the system browser: https,
// or plain http to this machine (a local authorization server in development).
func browserSafeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	default:
		return false
	}
}

func tokenAuthMethod(hasSecret bool, supported []string) string {
	if !hasSecret {
		return "none"
	}
	if slices.Contains(supported, "client_secret_post") && !slices.Contains(supported, "client_secret_basic") {
		return "client_secret_post"
	}
	return "client_secret_basic"
}
