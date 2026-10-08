// Package mcpoauth is the client half of the MCP authorization spec (OAuth
// 2.1): protected-resource metadata (RFC 9728), authorization server metadata
// (RFC 8414 / OIDC discovery), dynamic client registration (RFC 7591) and the
// token endpoint. Every URL it dials is attacker-influenced — it comes out of
// an MCP server's response or a metadata document — so every request goes
// through the urlguard policy, and nothing that carries a credential follows
// a redirect.
package mcpoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	clientName      = "TaskTrooper"
	requestTimeout  = 20 * time.Second
	maxResponseBody = 1 << 20
	probeVersion    = "2025-06-18"
)

var _ port.MCPOAuthProvider = (*Client)(nil)

type Client struct {
	policy urlguard.Policy
	now    func() time.Time
}

func New(policy urlguard.Policy) *Client {
	return &Client{policy: policy, now: time.Now}
}

func (c *Client) httpClient(followRedirects bool) *http.Client {
	hc := c.policy.Client(requestTimeout)
	if !followRedirects {
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return hc
}

// Discover follows the MCP spec's order: the WWW-Authenticate challenge of an
// unauthenticated request, then the well-known protected-resource documents,
// then (servers written to the 2025-03-26 revision) the server's own origin as
// the authorization server.
func (c *Client) Discover(ctx context.Context, serverURL string) (domain.MCPOAuthMetadata, error) {
	if err := c.checkEndpoint(serverURL); err != nil {
		return domain.MCPOAuthMetadata{}, fmt.Errorf("mcp url: %w", err)
	}
	metaClient := c.httpClient(true)
	challenges := c.probe(ctx, serverURL)

	meta := domain.MCPOAuthMetadata{}
	var issuer string
	if prm := c.protectedResource(ctx, serverURL, challenges, metaClient); prm != nil {
		issuer = prm.AuthorizationServers[0]
		meta.Resource = prm.Resource
		meta.Scopes = prm.ScopesSupported
	} else {
		origin, err := originOf(serverURL)
		if err != nil {
			return domain.MCPOAuthMetadata{}, err
		}
		issuer = origin
		meta.Resource = serverURL
	}
	if scopes := scopesFromChallenges(challenges); len(scopes) > 0 {
		meta.Scopes = scopes
	}
	if err := c.checkEndpoint(issuer); err != nil {
		return domain.MCPOAuthMetadata{}, fmt.Errorf("authorization server: %w", err)
	}

	asm, err := sdkauth.GetAuthServerMetadata(ctx, issuer, metaClient)
	if err != nil {
		return domain.MCPOAuthMetadata{}, fmt.Errorf("authorization server metadata: %w", err)
	}
	if asm == nil {
		base := strings.TrimRight(issuer, "/")
		meta.AuthorizationEndpoint = base + "/authorize"
		meta.TokenEndpoint = base + "/token"
		meta.RegistrationEndpoint = base + "/register"
	} else {
		if !slices.Contains(asm.CodeChallengeMethodsSupported, "S256") {
			return domain.MCPOAuthMetadata{}, errors.New("the authorization server does not support PKCE with S256")
		}
		meta.Issuer = asm.Issuer
		meta.AuthorizationEndpoint = asm.AuthorizationEndpoint
		meta.TokenEndpoint = asm.TokenEndpoint
		meta.RegistrationEndpoint = asm.RegistrationEndpoint
		meta.TokenAuthMethods = asm.TokenEndpointAuthMethodsSupported
	}

	for name, endpoint := range map[string]string{
		"authorization_endpoint": meta.AuthorizationEndpoint,
		"token_endpoint":         meta.TokenEndpoint,
	} {
		if err := c.checkEndpoint(endpoint); err != nil {
			return domain.MCPOAuthMetadata{}, fmt.Errorf("%s: %w", name, err)
		}
	}
	if meta.RegistrationEndpoint != "" {
		if err := c.checkEndpoint(meta.RegistrationEndpoint); err != nil {
			meta.RegistrationEndpoint = ""
		}
	}
	return meta, nil
}

// probe sends the unauthenticated initialize a real connection would open
// with, only to read the challenge off a 401.
func (c *Client) probe(ctx context.Context, serverURL string) []oauthex.Challenge {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": probeVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": clientName, "version": "1.0.0"},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.httpClient(false).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusUnauthorized {
		return nil
	}
	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil {
		return nil
	}
	return challenges
}

type prmCandidate struct {
	url      string
	resource string
}

func (c *Client) protectedResource(ctx context.Context, serverURL string, challenges []oauthex.Challenge, hc *http.Client) *oauthex.ProtectedResourceMetadata {
	for _, candidate := range prmCandidates(resourceMetadataURL(challenges), serverURL) {
		if err := c.checkEndpoint(candidate.url); err != nil {
			continue
		}
		prm, err := oauthex.GetProtectedResourceMetadata(ctx, candidate.url, candidate.resource, hc)
		if err != nil || prm == nil || len(prm.AuthorizationServers) == 0 {
			continue
		}
		return prm
	}
	return nil
}

func prmCandidates(challengeURL, serverURL string) []prmCandidate {
	var out []prmCandidate
	if challengeURL != "" {
		out = append(out, prmCandidate{url: challengeURL, resource: serverURL})
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return out
	}
	atPath := *u
	atPath.RawQuery, atPath.Fragment = "", ""
	atPath.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(u.Path, "/")
	out = append(out, prmCandidate{url: atPath.String(), resource: serverURL})

	atRoot := atPath
	atRoot.Path = "/.well-known/oauth-protected-resource"
	origin := *u
	origin.Path, origin.RawQuery, origin.Fragment = "", "", ""
	out = append(out, prmCandidate{url: atRoot.String(), resource: origin.String()})
	return out
}

func resourceMetadataURL(challenges []oauthex.Challenge) string {
	for _, ch := range challenges {
		if u := ch.Params["resource_metadata"]; u != "" {
			return u
		}
	}
	return ""
}

func scopesFromChallenges(challenges []oauthex.Challenge) []string {
	for _, ch := range challenges {
		if ch.Scheme == "bearer" && ch.Params["scope"] != "" {
			return strings.Fields(ch.Params["scope"])
		}
	}
	return nil
}

func originOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("invalid mcp url")
	}
	return u.Scheme + "://" + u.Host, nil
}

// checkEndpoint is the urlguard precheck plus the OAuth rule that every
// authorization server endpoint is https; plain http is only tolerated to a
// loopback host, and only where the policy lets loopback through at all.
func (c *Client) checkEndpoint(raw string) error {
	u, err := c.policy.Precheck(raw)
	if err != nil {
		return err
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("%s is not https", urlguard.LogValue(u))
}

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ApplicationType         string   `json:"application_type"`
	Scope                   string   `json:"scope,omitempty"`
}

type registrationResponse struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

type oauthError struct {
	Code string `json:"error"`
}

// Register asks for a public client (no secret, PKCE only) bound to the
// loopback redirect; an authorization server may still issue a secret, and
// then that is what the token endpoint gets.
func (c *Client) Register(ctx context.Context, meta domain.MCPOAuthMetadata, redirectURI string) (domain.MCPOAuthClient, error) {
	if err := c.checkEndpoint(meta.RegistrationEndpoint); err != nil {
		return domain.MCPOAuthClient{}, fmt.Errorf("registration_endpoint: %w", err)
	}
	payload, err := json.Marshal(registrationRequest{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              clientName,
		ApplicationType:         "native",
		Scope:                   strings.Join(meta.Scopes, " "),
	})
	if err != nil {
		return domain.MCPOAuthClient{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, bytes.NewReader(payload))
	if err != nil {
		return domain.MCPOAuthClient{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	status, body, err := c.do(req)
	if err != nil {
		return domain.MCPOAuthClient{}, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return domain.MCPOAuthClient{}, fmt.Errorf("registration refused: %s", describeOAuthError(status, body))
	}
	var reg registrationResponse
	if err := json.Unmarshal(body, &reg); err != nil || reg.ClientID == "" {
		return domain.MCPOAuthClient{}, errors.New("registration response has no client_id")
	}
	method := reg.TokenEndpointAuthMethod
	if method == "" {
		method = "none"
		if reg.ClientSecret != "" {
			method = "client_secret_basic"
		}
	}
	return domain.MCPOAuthClient{
		ClientID:     reg.ClientID,
		ClientSecret: reg.ClientSecret,
		AuthMethod:   method,
		RedirectURI:  redirectURI,
		Dynamic:      true,
	}, nil
}

func (c *Client) Exchange(ctx context.Context, meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, code, verifier, redirectURI string) (domain.MCPOAuthToken, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	return c.token(ctx, meta, client, form)
}

func (c *Client) Refresh(ctx context.Context, meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, refreshToken string) (domain.MCPOAuthToken, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return c.token(ctx, meta, client, form)
}

type tokenResponse struct {
	AccessToken  string          `json:"access_token"`
	TokenType    string          `json:"token_type"`
	ExpiresIn    json.RawMessage `json:"expires_in"`
	RefreshToken string          `json:"refresh_token"`
	Scope        string          `json:"scope"`
}

// token posts to the token endpoint with the RFC 8707 resource on every
// grant, refreshes included — an access token minted for another audience
// would be useless to this MCP server, or worse, usable elsewhere.
func (c *Client) token(ctx context.Context, meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, form url.Values) (domain.MCPOAuthToken, error) {
	if err := c.checkEndpoint(meta.TokenEndpoint); err != nil {
		return domain.MCPOAuthToken{}, fmt.Errorf("token_endpoint: %w", err)
	}
	if meta.Resource != "" {
		form.Set("resource", meta.Resource)
	}
	useBasic := client.ClientSecret != "" && client.AuthMethod != "client_secret_post" && client.AuthMethod != "none"
	if !useBasic {
		form.Set("client_id", client.ClientID)
		if client.ClientSecret != "" && client.AuthMethod == "client_secret_post" {
			form.Set("client_secret", client.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return domain.MCPOAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if useBasic {
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	}

	status, body, err := c.do(req)
	if err != nil {
		return domain.MCPOAuthToken{}, err
	}
	if status != http.StatusOK {
		var oe oauthError
		_ = json.Unmarshal(body, &oe)
		switch oe.Code {
		case "invalid_grant", "invalid_client", "unauthorized_client":
			return domain.MCPOAuthToken{}, fmt.Errorf("%w (%s)", domain.ErrMCPOAuthGrantRejected, oe.Code)
		}
		return domain.MCPOAuthToken{}, fmt.Errorf("token request refused: %s", describeOAuthError(status, body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return domain.MCPOAuthToken{}, errors.New("token response is not JSON")
	}
	if tr.AccessToken == "" {
		return domain.MCPOAuthToken{}, errors.New("token response has no access_token")
	}
	if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
		return domain.MCPOAuthToken{}, fmt.Errorf("unsupported token_type %q", tr.TokenType)
	}
	tok := domain.MCPOAuthToken{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    "Bearer",
		Scope:        tr.Scope,
	}
	if seconds, ok := parseExpiresIn(tr.ExpiresIn); ok && seconds > 0 {
		tok.ExpiresAt = c.now().Add(time.Duration(seconds) * time.Second)
	}
	return tok, nil
}

func (c *Client) do(req *http.Request) (int, []byte, error) {
	resp, err := c.httpClient(false).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// describeOAuthError names the status and the RFC 6749 error code only; the
// body itself never reaches a log or the UI, since a misbehaving server could
// echo anything into it.
func describeOAuthError(status int, body []byte) string {
	var oe oauthError
	if err := json.Unmarshal(body, &oe); err == nil && oe.Code != "" && len(oe.Code) <= 64 {
		return fmt.Sprintf("HTTP %d, %s", status, oe.Code)
	}
	return fmt.Sprintf("HTTP %d", status)
}

func parseExpiresIn(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		if v, err := n.Int64(); err == nil {
			return v, true
		}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}
