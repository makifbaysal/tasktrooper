package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const remoteConnectTimeout = 20 * time.Second

// remoteCallTimeout bounds one tool call on the coordination endpoint. Board
// tools answer in milliseconds; the ceiling is for one that waits on a slow
// database, and it sits well above anything that is not a hung connection.
const remoteCallTimeout = 5 * time.Minute

// RemoteConnector opens a run's coordination endpoint. The URL and token come
// from the process that started the executor, not from a model, so the URL
// guard user-configured servers go through does not apply: a development
// cloud on loopback or a private network is a legitimate target.
type RemoteConnector struct {
	// Transport replaces http.DefaultTransport underneath the bearer; tests only.
	Transport http.RoundTripper
}

var _ port.RemoteToolConnector = RemoteConnector{}

func (c RemoteConnector) Connect(ctx context.Context, endpoint port.RemoteToolEndpoint) ([]port.ToolExecutor, func(), error) {
	target, err := url.Parse(strings.TrimSpace(endpoint.URL))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, nil, fmt.Errorf("coordination endpoint %q is not an http(s) URL", endpoint.URL)
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	var transport http.RoundTripper = base
	if endpoint.Token != "" {
		transport = &headerTransport{
			base:    base,
			headers: map[string]string{"Authorization": "Bearer " + endpoint.Token},
			host:    target.Host,
		}
	}
	httpClient := &http.Client{
		Timeout:   remoteCallTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !strings.EqualFold(req.URL.Host, target.Host) {
				return fmt.Errorf("coordination endpoint redirected to %q; refusing to follow off %q", req.URL.Host, target.Host)
			}
			return nil
		},
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "tasktrooper-executor", Version: "1.0.0"}, nil)
	connectCtx, cancel := context.WithTimeout(ctx, remoteConnectTimeout)
	defer cancel()
	session, err := client.Connect(connectCtx, &sdkmcp.StreamableClientTransport{
		Endpoint:             target.String(),
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to the coordination endpoint: %w", err)
	}
	closeSession := func() {
		if err := session.Close(); err != nil {
			log.Debug().Err(err).Str("server", endpoint.ServerName).Msg("coordination mcp session close")
		}
	}

	listed, err := session.ListTools(connectCtx, nil)
	if err != nil {
		closeSession()
		return nil, nil, fmt.Errorf("list the coordination endpoint's tools: %w", err)
	}

	tools := make([]port.ToolExecutor, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		params, err := toolInputSchema(tool.InputSchema)
		if err != nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tools = append(tools, remoteTool{exec: &mcpToolExecutor{
			toolName: tool.Name,
			serverID: endpoint.ServerName,
			rawName:  tool.Name,
			session:  session,
			definition: domain.ToolDefinition{
				Type: "function",
				Function: domain.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  params,
				},
			},
		}})
	}
	return tools, closeSession, nil
}

// remoteTool hides MCPSource on purpose: a registry that sees it decides the
// tool by MCP server access instead of by name, and these tools are named in
// the agent's allow list like the built-ins they stand in for.
type remoteTool struct {
	exec *mcpToolExecutor
}

func (t remoteTool) Name() string                      { return t.exec.Name() }
func (t remoteTool) Definition() domain.ToolDefinition { return t.exec.Definition() }
func (t remoteTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	return t.exec.Execute(ctx, arguments)
}
