package port

import (
	"context"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// MCPOAuthRecord is one MCP server's stored OAuth state. ClientSecret,
// AccessToken and RefreshToken are ciphertext from the MCP secrets cipher.
type MCPOAuthRecord struct {
	ServerID          string
	Metadata          domain.MCPOAuthMetadata
	ClientID          string
	ClientSecret      []byte
	ClientAuthMethod  string
	ClientRedirectURI string
	ClientDynamic     bool
	AccessToken       []byte
	RefreshToken      []byte
	TokenType         string
	Scope             string
	ExpiresAt         *time.Time
	// Expired marks a sign-in the authorization server stopped honouring
	// (refresh rejected, or a 401 with nothing to refresh); only a new
	// authorization clears it.
	Expired   bool
	UpdatedAt time.Time
}

func (r MCPOAuthRecord) HasTokens() bool { return len(r.AccessToken) > 0 }

type MCPOAuthStore interface {
	GetMCPOAuth(ctx context.Context, serverID string) (MCPOAuthRecord, bool, error)
	SaveMCPOAuth(ctx context.Context, rec MCPOAuthRecord) error
	DeleteMCPOAuth(ctx context.Context, serverID string) error
}

// MCPOAuthProvider speaks to an MCP server's authorization server: metadata
// discovery, dynamic client registration and the token endpoint.
type MCPOAuthProvider interface {
	Discover(ctx context.Context, serverURL string) (domain.MCPOAuthMetadata, error)
	Register(ctx context.Context, meta domain.MCPOAuthMetadata, redirectURI string) (domain.MCPOAuthClient, error)
	Exchange(ctx context.Context, meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, code, verifier, redirectURI string) (domain.MCPOAuthToken, error)
	// Refresh returns domain.ErrMCPOAuthGrantRejected when the refresh token
	// itself was refused, as opposed to a transport failure worth retrying.
	Refresh(ctx context.Context, meta domain.MCPOAuthMetadata, client domain.MCPOAuthClient, refreshToken string) (domain.MCPOAuthToken, error)
}

// MCPTokenSource hands the MCP client the bearer token for a server. An empty
// token with a nil error means the server has no OAuth sign-in. rejected is
// the token a server just answered 401 to; the source refreshes unless a
// newer token than that one is already stored.
type MCPTokenSource interface {
	MCPAccessToken(ctx context.Context, serverID, rejected string) (string, error)
}
