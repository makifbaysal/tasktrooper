package domain

import (
	"errors"
	"time"
)

// MCPAuthStatus is how an MCP server authenticates right now, as the server
// list reports it.
type MCPAuthStatus string

const (
	MCPAuthNone           MCPAuthStatus = "none"
	MCPAuthOAuthConnected MCPAuthStatus = "oauth_connected"
	MCPAuthOAuthNeeded    MCPAuthStatus = "oauth_needed"
	MCPAuthOAuthExpired   MCPAuthStatus = "oauth_expired"
)

var (
	ErrMCPOAuthUnavailable    = errors.New("mcp oauth is not available on this server")
	ErrMCPOAuthNotHTTP        = errors.New("oauth applies to http mcp servers only")
	ErrMCPOAuthClientRequired = errors.New("the authorization server does not support dynamic client registration; enter a client id")
	ErrMCPOAuthDiscovery      = errors.New("could not discover the server's oauth authorization server")
	ErrMCPOAuthState          = errors.New("this sign-in link is unknown, expired or already used; start the connection again")
	ErrMCPOAuthDenied         = errors.New("the authorization server did not grant access")
	ErrMCPOAuthExchange       = errors.New("the authorization server refused the token request")
	// ErrMCPOAuthGrantRejected is a refresh the authorization server answered
	// with invalid_grant: the refresh token is dead and only a new sign-in helps.
	ErrMCPOAuthGrantRejected = errors.New("the authorization server rejected the refresh token")
	ErrMCPOAuthExpired       = errors.New("the oauth sign-in for this mcp server expired; connect it again")
)

// MCPOAuthMetadata is what discovery learned about the authorization server
// guarding one MCP server (RFC 9728 + RFC 8414).
type MCPOAuthMetadata struct {
	Resource              string   `json:"resource"`
	Issuer                string   `json:"issuer,omitempty"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	Scopes                []string `json:"scopes,omitempty"`
	TokenAuthMethods      []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// MCPOAuthClient is the OAuth client TaskTrooper presents to one
// authorization server: registered dynamically (RFC 7591) or entered by hand.
type MCPOAuthClient struct {
	ClientID     string
	ClientSecret string
	// AuthMethod is the token endpoint auth method: none,
	// client_secret_post or client_secret_basic.
	AuthMethod string
	// RedirectURI is the one a dynamic client was registered with; the
	// loopback port changes per launch, so a mismatch means re-register.
	RedirectURI string
	Dynamic     bool
}

type MCPOAuthToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	// ExpiresAt is zero when the authorization server gave no lifetime.
	ExpiresAt time.Time
}

type MCPOAuthStartRequest struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type MCPOAuthStartResponse struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// MCPOAuthCallback is what the authorization server sent back to the
// loopback redirect: code and state, or an error code; Issuer is the RFC 9207
// "iss" parameter when the server sends one.
type MCPOAuthCallback struct {
	State  string
	Code   string
	Issuer string
	Error  string
}
