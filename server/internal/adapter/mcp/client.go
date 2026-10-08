package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const namespaceSep = "_"

type mcpToolExecutor struct {
	toolName   string
	serverID   string
	rawName    string
	access     domain.MCPAccess
	session    *sdkmcp.ClientSession
	definition domain.ToolDefinition
}

var _ port.MCPServerTool = (*mcpToolExecutor)(nil)

func (e *mcpToolExecutor) Name() string {
	return e.toolName
}

func (e *mcpToolExecutor) MCPSource() domain.MCPToolSource {
	return domain.MCPToolSource{ServerID: e.serverID, Access: e.access}
}

func (e *mcpToolExecutor) Definition() domain.ToolDefinition {
	return e.definition
}

func (e *mcpToolExecutor) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var args any
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return domain.ToolResult{
				Name:    e.toolName,
				Content: fmt.Sprintf("invalid arguments: %v", err),
				IsError: true,
			}
		}
	}

	result, err := e.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      e.rawName,
		Arguments: args,
	})
	if err != nil {
		return domain.ToolResult{
			Name:    e.toolName,
			Content: fmt.Sprintf("mcp call error: %v", err),
			IsError: true,
		}
	}

	content, images := toolResultContent(result.Content)
	return domain.ToolResult{
		Name:    e.toolName,
		Content: content,
		IsError: result.IsError,
		Images:  images,
	}
}

// visionImageTypes are the formats every vision-capable provider accepts; an
// image in any other format would fail the whole model request.
var visionImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// maxImageBase64 matches the largest image the providers take in one block.
const maxImageBase64 = 5 * 1024 * 1024

type omittedImage struct {
	Type     string `json:"type"`
	MIMEType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
}

// toolResultContent keeps text blocks as text and hands images to the model
// the way browser_screenshot does (ToolResult.Images). Other blocks keep
// their JSON form; an image no provider would accept is reduced to its type
// and size so its base64 never lands in the text.
func toolResultContent(blocks []sdkmcp.Content) (string, []domain.ToolResultImage) {
	var parts []string
	var images []domain.ToolResultImage
	for _, c := range blocks {
		switch block := c.(type) {
		case *sdkmcp.TextContent:
			parts = append(parts, block.Text)
		case *sdkmcp.ImageContent:
			mime := strings.ToLower(strings.TrimSpace(block.MIMEType))
			encoded := base64.StdEncoding.EncodeToString(block.Data)
			if len(block.Data) > 0 && visionImageTypes[mime] && len(encoded) <= maxImageBase64 {
				images = append(images, domain.ToolResultImage{MediaType: mime, Data: encoded})
				continue
			}
			raw, _ := json.Marshal(omittedImage{Type: "image", MIMEType: block.MIMEType, Bytes: len(block.Data)})
			parts = append(parts, string(raw))
		default:
			raw, _ := json.Marshal(c)
			parts = append(parts, string(raw))
		}
	}
	return strings.Join(parts, "\n"), images
}

func NamespacedToolName(serverID, toolName string) string {
	return fmt.Sprintf("mcp%s%s%s%s", namespaceSep, serverID, namespaceSep, toolName)
}

// httpAuth is one HTTP server's authentication state for a connection:
// where its OAuth token comes from, and whether the server answered 401.
type httpAuth struct {
	tokens       port.MCPTokenSource
	unauthorized atomic.Bool
}

func connectServer(ctx context.Context, cfg domain.MCPServerConfig, policy urlguard.Policy, auth *httpAuth) ([]*mcpToolExecutor, func(), error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "local-llm-bridge",
		Version: "1.0.0",
	}, nil)

	var transport sdkmcp.Transport
	var stdioCmd *exec.Cmd
	switch cfg.Transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, nil, fmt.Errorf("stdio transport requires command")
		}
		cmd := winshim.Cmd(cfg.Command, cfg.Args...)
		cmd.Env = stdioServerEnv(cfg.Env)
		isolateStdio(cmd)
		stdioCmd = cmd
		transport = &sdkmcp.CommandTransport{Command: cmd}
	case "http":
		if cfg.URL == "" {
			return nil, nil, fmt.Errorf("http transport requires url")
		}
		httpClient, err := guardedMCPClientWithAuth(ctx, cfg, policy, auth)
		if err != nil {
			return nil, nil, err
		}
		transport = &sdkmcp.StreamableClientTransport{
			Endpoint:             cfg.URL,
			HTTPClient:           httpClient,
			DisableStandaloneSSE: true,
		}
	default:
		return nil, nil, fmt.Errorf("unsupported transport: %q (use 'stdio' or 'http')", cfg.Transport)
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if stdioCmd != nil {
			killStdioTree(stdioCmd)
		}
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	closeFunc := func() {
		if err := session.Close(); err != nil {
			log.Warn().Err(err).Str("server", cfg.ID).Msg("mcp session close error")
		}
		if stdioCmd != nil {
			killStdioTree(stdioCmd)
		}
	}

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		closeFunc()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	allowedSet := make(map[string]bool)
	for _, t := range cfg.AllowedTools {
		allowedSet[t] = true
	}

	var executors []*mcpToolExecutor
	for _, tool := range result.Tools {
		if len(allowedSet) > 0 && !allowedSet[tool.Name] {
			continue
		}

		nsName := NamespacedToolName(cfg.ID, tool.Name)

		params, err := toolInputSchema(tool.InputSchema)
		if err != nil {
			log.Warn().Err(err).Str("tool", tool.Name).Msg("could not parse input schema, using empty schema")
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}

		executor := &mcpToolExecutor{
			toolName: nsName,
			serverID: cfg.ID,
			rawName:  tool.Name,
			access:   cfg.Access.Effective(),
			session:  session,
			definition: domain.ToolDefinition{
				Type: "function",
				Function: domain.FunctionDefinition{
					Name:        nsName,
					Description: tool.Description,
					Parameters:  params,
				},
			},
		}
		executors = append(executors, executor)
		log.Debug().Str("server", cfg.ID).Str("tool", nsName).Msg("discovered mcp tool")
	}

	return executors, closeFunc, nil
}

func stdioServerEnv(overrides map[string]string) []string {
	configured := make([]string, 0, len(overrides))
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		configured = append(configured, key+"="+overrides[key])
	}
	return childenv.For(os.Environ(), configured)
}

func toolInputSchema(schema any) (map[string]interface{}, error) {
	if schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func guardedMCPClient(ctx context.Context, cfg domain.MCPServerConfig, policy urlguard.Policy) (*http.Client, error) {
	return guardedMCPClientWithAuth(ctx, cfg, policy, nil)
}

// guardedMCPClientWithAuth layers, outermost first: the configured headers,
// then the OAuth bearer — so a sign-in replaces a configured Authorization
// header rather than the other way round — then the guarded dialer.
func guardedMCPClientWithAuth(ctx context.Context, cfg domain.MCPServerConfig, policy urlguard.Policy, auth *httpAuth) (*http.Client, error) {
	target, err := policy.Validate(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("mcp endpoint refused: %w", err)
	}

	client := policy.ClientFor(target, 30*time.Second)
	client.CheckRedirect = policy.SameHostRedirect(target.URL)
	if auth != nil {
		client.Transport = &authTransport{
			base:     client.Transport,
			serverID: cfg.ID,
			host:     target.URL.Host,
			auth:     auth,
		}
	}
	if len(cfg.Headers) > 0 {
		client.Transport = &headerTransport{
			base:    client.Transport,
			headers: cfg.Headers,
			host:    target.URL.Host,
		}
	}
	return client, nil
}

// authTransport attaches the server's OAuth access token. On a 401 it asks
// the token source once for a newer token and replays the request with it;
// a 401 that survives that is recorded so the server list can offer sign-in.
type authTransport struct {
	base     http.RoundTripper
	serverID string
	host     string
	auth     *httpAuth
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.host != "" && !strings.EqualFold(req.URL.Host, t.host) {
		closeRequestBody(req)
		return nil, fmt.Errorf("mcp: refusing to send a sign-in token to %q, expected %q", req.URL.Host, t.host)
	}
	token, err := t.token(req.Context(), "")
	if err != nil {
		closeRequestBody(req)
		return nil, err
	}
	resp, err := t.send(req, req.Body, token)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	if token != "" && (req.Body == nil || req.GetBody != nil) {
		if fresh, err := t.token(req.Context(), token); err == nil && fresh != "" && fresh != token {
			var body io.ReadCloser = http.NoBody
			if req.GetBody != nil {
				if body, err = req.GetBody(); err != nil {
					t.auth.unauthorized.Store(true)
					return resp, nil
				}
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			resp, err = t.send(req, body, fresh)
			if err != nil {
				return nil, err
			}
		}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		t.auth.unauthorized.Store(true)
	}
	return resp, nil
}

// closeRequestBody keeps the RoundTripper contract on the paths that never
// reach the base transport, which would otherwise have closed it.
func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

func (t *authTransport) token(ctx context.Context, rejected string) (string, error) {
	if t.auth.tokens == nil {
		return "", nil
	}
	return t.auth.tokens.MCPAccessToken(ctx, t.serverID, rejected)
}

func (t *authTransport) send(req *http.Request, body io.ReadCloser, token string) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Body = body
	if token != "" {
		out.Header.Set("Authorization", "Bearer "+token)
	}
	return t.base.RoundTrip(out)
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
	host    string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.host != "" && !strings.EqualFold(req.URL.Host, t.host) {
		return nil, fmt.Errorf("mcp: refusing to send configured headers to %q, expected %q", req.URL.Host, t.host)
	}
	reqCopy := req.Clone(req.Context())
	for k, v := range t.headers {
		reqCopy.Header.Set(k, v)
	}
	return t.base.RoundTrip(reqCopy)
}

type serverHealth struct {
	ID           string
	Connected    bool
	Disabled     bool
	AuthRequired bool
	ToolCount    int
	Tools        []string
	LastError    string
}

type Manager struct {
	// loadMu serialises whole loads: a lazy boot load and a reload can
	// otherwise interleave and leave one of them's sessions open forever.
	loadMu     sync.Mutex
	mu         sync.RWMutex
	closeFuncs []func()
	servers    []serverHealth
	policy     urlguard.Policy
	tokens     port.MCPTokenSource
	registered map[string]bool
}

func (m *Manager) SetTokenSource(tokens port.MCPTokenSource) {
	m.mu.Lock()
	m.tokens = tokens
	m.mu.Unlock()
}

func (m *Manager) SetURLPolicy(p urlguard.Policy) {
	m.mu.Lock()
	m.policy = p
	m.mu.Unlock()
}

func (m *Manager) urlPolicy() urlguard.Policy {
	m.mu.RLock()
	p := m.policy
	m.mu.RUnlock()
	if len(p.Schemes) == 0 {
		return urlguard.Default()
	}
	return p
}

func (m *Manager) Health() []map[string]interface{} {
	m.mu.RLock()
	servers := append([]serverHealth(nil), m.servers...)
	m.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(servers))
	for _, s := range servers {
		entry := map[string]interface{}{
			"id":         s.ID,
			"connected":  s.Connected,
			"tool_count": s.ToolCount,
		}
		if s.Disabled {
			entry["status"] = "disabled"
			entry["connected"] = false
		} else if s.Connected {
			entry["status"] = "connected"
		} else {
			entry["status"] = "error"
		}
		if s.LastError != "" {
			entry["last_error"] = s.LastError
		}
		if len(s.Tools) > 0 {
			entry["tools"] = s.Tools
		}
		if s.AuthRequired {
			entry["auth_required"] = true
		}
		result = append(result, entry)
	}
	return result
}

// LoadAndRegister replaces whatever the previous load connected: its sessions
// are closed, every enabled server is connected and its tools registered, and
// a tool registered last time but not now (its server was removed, disabled
// or failed) is dropped from the registry, so a closed session's tools never
// stay visible to agents.
func (m *Manager) LoadAndRegister(ctx context.Context, configs []domain.MCPServerConfig, registry port.ToolRegistry) {
	m.loadMu.Lock()
	defer m.loadMu.Unlock()
	m.Close()
	var (
		servers    []serverHealth
		closeFuncs []func()
	)
	current := make(map[string]bool)
	defer func() {
		m.mu.Lock()
		previous := m.registered
		m.servers = servers
		m.closeFuncs = append(m.closeFuncs, closeFuncs...)
		m.registered = current
		m.mu.Unlock()
		if unregisterer, ok := registry.(port.ToolUnregisterer); ok {
			for name := range previous {
				if !current[name] {
					unregisterer.Unregister(name)
				}
			}
		}
	}()
	policy := m.urlPolicy()
	m.mu.RLock()
	tokens := m.tokens
	m.mu.RUnlock()
	for _, cfg := range configs {
		if !cfg.Enabled {
			servers = append(servers, serverHealth{ID: cfg.ID, Connected: false, Disabled: true})
			log.Debug().Str("server", cfg.ID).Msg("mcp server disabled, skipping")
			continue
		}

		auth := &httpAuth{tokens: tokens}
		connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		executors, closeFunc, err := connectServer(connectCtx, cfg, policy, auth)
		cancel()
		if err != nil {
			log.Error().Err(err).Str("server", cfg.ID).Msg("failed to connect mcp server, skipping")
			servers = append(servers, serverHealth{
				ID: cfg.ID, Connected: false, LastError: err.Error(), AuthRequired: auth.unauthorized.Load(),
			})
			continue
		}

		closeFuncs = append(closeFuncs, closeFunc)
		toolNames := make([]string, 0, len(executors))
		for _, e := range executors {
			registry.Register(e)
			current[e.Name()] = true
			toolNames = append(toolNames, e.Name())
		}
		servers = append(servers, serverHealth{ID: cfg.ID, Connected: true, ToolCount: len(executors), Tools: toolNames})
		log.Info().Str("server", cfg.ID).Int("tools", len(executors)).Msg("mcp server connected")
	}
}

func (m *Manager) Reload(ctx context.Context, configs []domain.MCPServerConfig, registry port.ToolRegistry) {
	m.Close()
	m.LoadAndRegister(ctx, configs, registry)
}

func (m *Manager) Close() {
	m.mu.Lock()
	closeFuncs := m.closeFuncs
	m.closeFuncs = nil
	m.mu.Unlock()
	for _, fn := range closeFuncs {
		fn()
	}
}
