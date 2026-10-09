package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// RemoteToolEndpoint is the coordination tool surface one executor run is
// handed: an MCP endpoint and the bearer that scopes it to that run.
type RemoteToolEndpoint struct {
	URL        string
	Token      string
	ServerName string
}

// RemoteToolConnector opens a RemoteToolEndpoint for the length of one run.
// The tools come back under the names the endpoint serves them by, not
// namespaced the way a user-configured MCP server's tools are, because the
// agent's tool policy names them that way. closeSession ends the session and
// is safe to call once the run is over, whatever its outcome.
type RemoteToolConnector interface {
	Connect(ctx context.Context, endpoint RemoteToolEndpoint) (tools []ToolExecutor, closeSession func(), err error)
}

// ToolSurface is a tool registry served to a process on this computer — an
// agent CLI the runner starts — for the length of one run.
type ToolSurface struct {
	// Name is what the client knows the server by; a CLI prefixes every tool
	// name with it, so it is the coordination endpoint's own.
	Name     string
	Registry ToolRegistry
	Policy   domain.ToolPolicy
	// Context is what each call runs under: the run's workspace, environment,
	// process scope and index. A call also ends when its own request does.
	Context context.Context
}

// ServedToolSurface is where a surface answers and the bearer it requires.
// Close stops it and is safe to call more than once.
type ServedToolSurface struct {
	URL   string
	Token string
	Close func()
}

// ToolSurfaceServer serves a ToolSurface on loopback, behind a bearer of its
// own.
type ToolSurfaceServer interface {
	Serve(surface ToolSurface) (ServedToolSurface, error)
}
