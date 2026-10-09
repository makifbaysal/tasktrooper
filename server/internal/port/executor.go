package port

import "context"

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
