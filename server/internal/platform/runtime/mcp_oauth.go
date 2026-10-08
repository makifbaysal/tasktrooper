package runtime

import (
	"strings"

	httpadapter "github.com/makifbaysal/tasktrooper/server/internal/adapter/http"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpoauth"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	mcpsvc "github.com/makifbaysal/tasktrooper/server/internal/application/mcp"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// enableMCPOAuth gives HTTP MCP servers OAuth sign-in: the service runs the
// flows and stores tokens with the MCP secrets cipher, and the manager asks it
// for a bearer token on every request. Discovery and token calls go through
// the same outbound guard as the MCP connections themselves.
func (e *engine) enableMCPOAuth(svc *mcpsvc.Service, store port.MCPStore) {
	oauthStore, ok := store.(port.MCPOAuthStore)
	if !ok || svc == nil {
		return
	}
	svc.EnableOAuth(oauthStore, mcpoauth.New(urlguard.Default()), e.mcpOAuthRedirectURI)
	if e.mcpManager != nil {
		e.mcpManager.SetTokenSource(svc)
	}
}

// mcpOAuthRedirectURI is the loopback callback on the port this process
// listens on now — the same 127.0.0.1 listener as everything else, never a
// hosted broker. The port changes per launch, which is why the service reads
// it per flow and re-registers a dynamic client when it moved.
func (e *engine) mcpOAuthRedirectURI() string {
	if e.mcpEndpoint == nil {
		return ""
	}
	base := strings.TrimSuffix(e.mcpEndpoint.get(), mcpserver.Path)
	if base == "" {
		return ""
	}
	return base + httpadapter.MCPOAuthCallbackPath
}
