package mcp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4}

func newFakeMCPServer(tools map[string]sdkmcp.ToolHandler) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake", Version: "1.0.0"}, nil)
	for name, handler := range tools {
		server.AddTool(&sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, handler)
	}
	return server
}

func screenshotTool(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: "frame 12:3"},
		&sdkmcp.ImageContent{MIMEType: "image/png", Data: pngBytes},
		&sdkmcp.ImageContent{MIMEType: "image/svg+xml", Data: []byte("<svg/>")},
	}}, nil
}

// recordingRegistry is only a sink for what LoadAndRegister registers and
// unregisters; nothing here filters.
type recordingRegistry struct {
	mu           sync.Mutex
	tools        map[string]port.ToolExecutor
	unregistered []string
}

func newRecordingRegistry() *recordingRegistry {
	return &recordingRegistry{tools: map[string]port.ToolExecutor{}}
}

func (r *recordingRegistry) Register(e port.ToolExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[e.Name()] = e
}

func (r *recordingRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	r.unregistered = append(r.unregistered, name)
}

func (r *recordingRegistry) Definitions() []domain.ToolDefinition { return nil }
func (r *recordingRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition {
	return nil
}
func (r *recordingRegistry) AllToolNames() []string { return nil }
func (r *recordingRegistry) Execute(context.Context, domain.ToolCall) domain.ToolResult {
	return domain.ToolResult{}
}
func (r *recordingRegistry) ExecuteWithPolicy(context.Context, domain.ToolCall, domain.ToolPolicy) domain.ToolResult {
	return domain.ToolResult{}
}

func (r *recordingRegistry) tool(name string) port.ToolExecutor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tools[name]
}

type ContentAuthSuite struct {
	suite.Suite
	registry *recordingRegistry
	manager  *Manager
}

func (s *ContentAuthSuite) SetupTest() {
	s.registry = newRecordingRegistry()
	s.manager = &Manager{}
	s.manager.SetURLPolicy(localPolicy())
}

func (s *ContentAuthSuite) TearDownTest() {
	s.manager.Close()
}

func (s *ContentAuthSuite) serve(handler http.Handler) string {
	srv := httptest.NewServer(handler)
	s.T().Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

func streamable(server *sdkmcp.Server) http.Handler {
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
}

func (s *ContentAuthSuite) TestImageBlocksReachTheModelAsImagesAndTextStaysText() {
	url := s.serve(streamable(newFakeMCPServer(map[string]sdkmcp.ToolHandler{"get_screenshot": screenshotTool})))

	s.manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{
		{ID: "design_tool", Enabled: true, Transport: "http", URL: url, Access: domain.MCPAccessListed},
	}, s.registry)

	tool := s.registry.tool("mcp_design_tool_get_screenshot")
	s.Require().NotNil(tool)
	result := tool.Execute(context.Background(), "{}")

	s.False(result.IsError)
	s.Require().Len(result.Images, 1)
	s.Equal("image/png", result.Images[0].MediaType)
	s.Equal(base64.StdEncoding.EncodeToString(pngBytes), result.Images[0].Data)
	s.Contains(result.Content, "frame 12:3")
	s.Contains(result.Content, `"mimeType":"image/svg+xml"`, "an image no provider renders keeps its type")
	s.NotContains(result.Content, base64.StdEncoding.EncodeToString([]byte("<svg/>")), "but not its base64")
	s.NotContains(result.Content, base64.StdEncoding.EncodeToString(pngBytes))

	source, ok := tool.(port.MCPServerTool)
	s.Require().True(ok)
	s.Equal(domain.MCPToolSource{ServerID: "design_tool", Access: domain.MCPAccessListed}, source.MCPSource())
}

func (s *ContentAuthSuite) TestBearerTokenIsSentAndARejectedOneIsRefreshedOnce() {
	inner := streamable(newFakeMCPServer(map[string]sdkmcp.ToolHandler{"get_screenshot": screenshotTool}))
	var seen []string
	var seenMu sync.Mutex
	url := s.serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		seenMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))

	tokens := mocks.NewMCPTokenSource(s.T())
	tokens.On("MCPAccessToken", mock.Anything, "figma", "").Return("stale-token", nil).Once()
	tokens.On("MCPAccessToken", mock.Anything, "figma", "stale-token").Return("fresh-token", nil).Once()
	tokens.On("MCPAccessToken", mock.Anything, "figma", "").Return("fresh-token", nil)
	s.manager.SetTokenSource(tokens)

	s.manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{
		{ID: "figma", Enabled: true, Transport: "http", URL: url,
			Headers: map[string]string{"Authorization": "Bearer configured-header"}},
	}, s.registry)

	s.Require().NotNil(s.registry.tool("mcp_figma_get_screenshot"), "the connection succeeded after the refresh")
	health := s.manager.Health()
	s.Require().Len(health, 1)
	s.Equal(true, health[0]["connected"])
	s.Nil(health[0]["auth_required"])

	seenMu.Lock()
	defer seenMu.Unlock()
	s.Equal("Bearer stale-token", seen[0], "the sign-in token replaces a configured Authorization header")
	s.Equal("Bearer fresh-token", seen[1])
}

func (s *ContentAuthSuite) TestAServerThatKeepsAnswering401IsReportedAsNeedingSignIn() {
	url := s.serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	s.manager.SetTokenSource(nil)

	s.manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{
		{ID: "linear", Enabled: true, Transport: "http", URL: url},
	}, s.registry)

	health := s.manager.Health()
	s.Require().Len(health, 1)
	s.Equal(false, health[0]["connected"])
	s.Equal(true, health[0]["auth_required"])
}

func (s *ContentAuthSuite) TestReloadRetiresTheToolsOfAServerThatIsGone() {
	url := s.serve(streamable(newFakeMCPServer(map[string]sdkmcp.ToolHandler{"get_screenshot": screenshotTool})))
	cfg := domain.MCPServerConfig{ID: "design", Enabled: true, Transport: "http", URL: url}

	s.manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{cfg}, s.registry)
	s.Require().NotNil(s.registry.tool("mcp_design_get_screenshot"))

	s.manager.Close()
	cfg.Enabled = false
	s.manager.LoadAndRegister(context.Background(), []domain.MCPServerConfig{cfg}, s.registry)

	s.Nil(s.registry.tool("mcp_design_get_screenshot"))
	s.Equal([]string{"mcp_design_get_screenshot"}, s.registry.unregistered)
}

func (s *ContentAuthSuite) TestTheSignInTokenNeverLeavesTheConfiguredHost() {
	tokens := mocks.NewMCPTokenSource(s.T())
	transport := &authTransport{
		base:     http.DefaultTransport,
		serverID: "figma",
		host:     "mcp.example.com",
		auth:     &httpAuth{tokens: tokens},
	}
	req, err := http.NewRequest(http.MethodGet, "https://attacker.example.net/mcp", nil)
	s.Require().NoError(err)

	resp, err := transport.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	s.Error(err)
	s.True(strings.Contains(err.Error(), "refusing"))
}

func TestContentAuthSuite(t *testing.T) {
	suite.Run(t, new(ContentAuthSuite))
}
