package domain

import (
	"errors"
	"time"
)

var (
	ErrMCPServerNotFound      = errors.New("mcp server not found")
	ErrMCPServerAlreadyExists = errors.New("mcp server already exists")
	ErrMCPInvalidRequest      = errors.New("invalid mcp server request")
)

type MCPServer struct {
	ID           string            `json:"id"`
	Enabled      bool              `json:"enabled"`
	Transport    string            `json:"transport"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	URL          string            `json:"url,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	AllowedTools []string          `json:"allowed_tools,omitempty"`
	Access       MCPAccess         `json:"access"`
	CreatedAt    time.Time         `json:"created_at"`
}

type MCPServerView struct {
	MCPServer
	ConfigFields []MCPConfigField `json:"config_fields,omitempty"`
	// The required fields with no value yet; an enabled server with any of
	// these cannot connect, and the UI names them instead of only reporting
	// the resulting connection failure.
	MissingConfig []MCPConfigField `json:"missing_config,omitempty"`
	Connected     bool             `json:"connected"`
	ToolCount     int              `json:"tool_count"`
	Tools         []string         `json:"tools,omitempty"`
	Status        string           `json:"status"`
	LastError     string           `json:"last_error,omitempty"`
	Auth          MCPAuthStatus    `json:"auth"`
}

type MCPServerListResponse struct {
	Servers []MCPServerView `json:"servers"`
	Count   int             `json:"count"`
	// OAuthRedirectURI is the loopback callback an authorization server
	// sends a sign-in back to; shown so a hand-registered OAuth client can be
	// given it. Empty when OAuth is not wired.
	OAuthRedirectURI string `json:"oauth_redirect_uri,omitempty"`
}

type CreateMCPServerRequest struct {
	ID           string            `json:"id"`
	Enabled      bool              `json:"enabled"`
	Transport    string            `json:"transport"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers"`
	AllowedTools []string          `json:"allowed_tools"`
	// Access is "all" or "listed"; empty means DefaultNewMCPAccess on create
	// and "unchanged" on update.
	Access MCPAccess `json:"access"`
}

type UpdateMCPServerRequest = CreateMCPServerRequest

type MCPSecretRef struct {
	Location string
	Key      string
}

func (c MCPServerConfig) ToMCPServer() MCPServer {
	return MCPServer{
		ID:           c.ID,
		Enabled:      c.Enabled,
		Transport:    c.Transport,
		Command:      c.Command,
		Args:         c.Args,
		Env:          c.Env,
		URL:          c.URL,
		Headers:      c.Headers,
		AllowedTools: c.AllowedTools,
		Access:       c.Access,
	}
}

func (s MCPServer) ToConfig() MCPServerConfig {
	return MCPServerConfig{
		ID:           s.ID,
		Enabled:      s.Enabled,
		Transport:    s.Transport,
		Command:      s.Command,
		Args:         s.Args,
		Env:          s.Env,
		URL:          s.URL,
		Headers:      s.Headers,
		AllowedTools: s.AllowedTools,
		Access:       s.Access,
	}
}
