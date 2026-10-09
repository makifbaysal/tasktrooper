package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/executorapi"
	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

// SurfaceSuite drives /exec/mcp.open end to end: a real executor, a real
// coordination endpoint (the server's own mcpserver) that records the bearer
// it is called with, and a real MCP client in place of the agent CLI.
type SurfaceSuite struct {
	suite.Suite
	cloud      *httptest.Server
	board      *mocks.ToolExecutor
	cloudToken string
	cloudAuth  []string
	authMu     sync.Mutex
	root       string
	server     *Server
	baseURL    string
	logs       *syncBuffer
	restoreLog zerolog.Logger
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSurfaceSuite(t *testing.T) {
	suite.Run(t, new(SurfaceSuite))
}

func (s *SurfaceSuite) SetupTest() {
	s.logs = &syncBuffer{}
	s.restoreLog = log.Logger
	log.Logger = zerolog.New(s.logs).Level(zerolog.DebugLevel)

	s.board = mocks.NewToolExecutor(s.T())
	s.board.On("Name").Return("list_board_tasks").Maybe()
	s.board.On("Definition").Return(domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{
		Name: "list_board_tasks", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}}).Maybe()
	s.board.On("Execute", mock.Anything, mock.Anything).Return(domain.ToolResult{Content: "3 tasks on the board"}).Maybe()
	cloudTools := registry.New()
	cloudTools.Register(s.board)
	tokens := mcpserver.NewRunTokenRegistry()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	mcpserver.New(cloudTools, tokens).Register(app)
	fiberHandler := adaptor.FiberApp(app)
	s.cloudAuth = nil
	s.cloud = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.authMu.Lock()
		s.cloudAuth = append(s.cloudAuth, r.Header.Get("Authorization"))
		s.authMu.Unlock()
		fiberHandler(w, r)
	}))
	var err error
	s.cloudToken, err = tokens.Mint(mcpserver.Run{Ctx: context.Background(), Policy: domain.ToolPolicy{AllowTools: []string{"list_board_tasks"}}})
	s.Require().NoError(err)

	s.root = s.T().TempDir()
	workspace := filepath.Join(s.root, "repos", "demo", "task-1")
	s.Require().NoError(os.MkdirAll(workspace, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(workspace, "auth.go"),
		[]byte("package auth\n\n// RefreshToken renews a session.\nfunc RefreshToken(id string) (string, error) {\n\treturn id, nil\n}\n"), 0o644))

	var stdout bytes.Buffer
	s.server, err = Start(Config{Listen: defaultListen, Token: validToken, WorkspaceRoot: s.root}, &stdout, Options{})
	s.Require().NoError(err)
	s.baseURL = strings.TrimPrefix(strings.TrimSpace(stdout.String()), ListeningPrefix)
}

func (s *SurfaceSuite) TearDownTest() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.NoError(s.server.Shutdown(ctx))
	s.cloud.Close()
	log.Logger = s.restoreLog
}

func (s *SurfaceSuite) call(path string, body any) (int, []byte) {
	raw, err := json.Marshal(body)
	s.Require().NoError(err)
	req, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(raw))
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	return resp.StatusCode, out
}

type openedSurface struct {
	RunID      string   `json:"run_id"`
	URL        string   `json:"url"`
	Token      string   `json:"token"`
	ServerName string   `json:"server_name"`
	Tools      []string `json:"tools"`
}

func (s *SurfaceSuite) open(extra map[string]any) (openedSurface, []byte) {
	body := map[string]any{
		"run_id":    "cli-run-1",
		"workspace": "repos/demo/task-1",
		"cloud_mcp": map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": s.cloudToken, "server_name": "tasktrooper"},
	}
	for k, v := range extra {
		body[k] = v
	}
	status, raw := s.call(executorapi.PathMCPOpen, body)
	s.Require().Equal(http.StatusOK, status, string(raw))
	var opened openedSurface
	s.Require().NoError(json.Unmarshal(raw, &opened))
	return opened, raw
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(ctx context.Context, url, token string) (*sdkmcp.ClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "fake-cli", Version: "1.0.0"}, nil)
	return client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearerTransport{token: token}, Timeout: 10 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
}

func names(tools []*sdkmcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func text(res *sdkmcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

func (s *SurfaceSuite) TestTheSurfaceServesLocalToolsAndProxiesTheCoordinationTools() {
	opened, raw := s.open(nil)
	s.NotContains(string(raw), s.cloudToken, "the coordination token came back in the answer")
	s.NotEmpty(opened.Token)
	s.NotEqual(s.cloudToken, opened.Token)
	s.NotEqual(validToken, opened.Token)
	s.Equal("tasktrooper", opened.ServerName)
	s.True(strings.HasPrefix(opened.URL, "http://127.0.0.1:"), opened.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := connect(ctx, opened.URL, opened.Token)
	s.Require().NoError(err)
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	s.Require().NoError(err)
	served := names(listed.Tools)
	for _, want := range []string{"list_board_tasks", "http_request", "get_symbol_skeleton", "download_file", "browser_navigate"} {
		s.Contains(served, want)
	}
	for _, native := range []string{"read_file", "write_file", "run_terminal", "grep_code", "get_repo_tree"} {
		s.NotContains(served, native, "a CLI has its own %s", native)
	}
	s.ElementsMatch(opened.Tools, served)

	cloudCalls := len(s.cloudAuthSnapshot())
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "list_board_tasks", Arguments: map[string]any{}})
	s.Require().NoError(err)
	s.False(res.IsError, text(res))
	s.Equal("3 tasks on the board", text(res))
	auth := s.cloudAuthSnapshot()
	s.Greater(len(auth), cloudCalls)
	for _, header := range auth {
		s.Equal("Bearer "+s.cloudToken, header, "the coordination endpoint was called without the run's own bearer")
	}

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "get_symbol_skeleton", Arguments: map[string]any{"file_path": "auth.go"}})
	s.Require().NoError(err)
	s.False(res.IsError, text(res))
	s.Contains(text(res), "RefreshToken", "the local tool did not read the run's workspace")

	for _, secret := range []string{s.cloudToken, opened.Token, validToken} {
		s.NotContains(s.logs.String(), secret)
	}
}

func (s *SurfaceSuite) TestTheBearerIsRequired() {
	opened, _ := s.open(nil)
	for _, token := range []string{"", "not-the-token", s.cloudToken, validToken} {
		req, err := http.NewRequest(http.MethodPost, opened.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		s.Require().NoError(err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		s.Require().NoError(err)
		_ = resp.Body.Close()
		s.Equal(http.StatusUnauthorized, resp.StatusCode, "token %q", token)
	}
}

func (s *SurfaceSuite) TestToolPolicyNarrowsTheSurface() {
	opened, _ := s.open(map[string]any{"tool_policy": map[string]any{"allow_tools": []string{"http_request", "list_board_tasks"}}})
	s.ElementsMatch([]string{"http_request", "list_board_tasks"}, opened.Tools)
}

func (s *SurfaceSuite) TestCloseStopsTheSurfaceAndASecondOpenOfALiveRunConflicts() {
	opened, _ := s.open(nil)
	status, _ := s.call(executorapi.PathMCPOpen, map[string]any{"run_id": "cli-run-1"})
	s.Equal(http.StatusConflict, status)

	status, raw := s.call(executorapi.PathMCPClose, map[string]any{"run_id": "cli-run-1"})
	s.Equal(http.StatusOK, status)
	s.Contains(string(raw), `"closed":true`)
	s.surfaceGone(opened)

	_, raw = s.call(executorapi.PathMCPClose, map[string]any{"run_id": "cli-run-1"})
	s.Contains(string(raw), `"closed":false`)
	reopened, _ := s.open(nil)
	s.NotEqual(opened.Token, reopened.Token)
}

func (s *SurfaceSuite) TestTheSurfaceClosesAtItsTimeout() {
	opened, _ := s.open(map[string]any{"timeout_ms": 300})
	s.surfaceGone(opened)
	_, raw := s.call(executorapi.PathMCPClose, map[string]any{"run_id": "cli-run-1"})
	s.Contains(string(raw), `"closed":false`)
}

func (s *SurfaceSuite) TestShutdownClosesEverySurface() {
	opened, _ := s.open(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.Require().NoError(s.server.Shutdown(ctx))
	s.surfaceGone(opened)
	srv, err := Start(Config{Listen: defaultListen, Token: validToken, WorkspaceRoot: s.root}, io.Discard, Options{})
	s.Require().NoError(err)
	s.server = srv
}

func (s *SurfaceSuite) TestHealthAdvertisesTheLocalTools() {
	req, err := http.NewRequest(http.MethodGet, s.baseURL+executorapi.PathHealth, nil)
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	defer resp.Body.Close()
	var health struct {
		LocalTools []string `json:"local_tools"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&health))
	for _, want := range []string{"http_request", "download_file", "get_symbol_skeleton", "browser_navigate"} {
		s.Contains(health.LocalTools, want)
	}
	s.NotContains(health.LocalTools, "read_file")
	s.NotContains(health.LocalTools, "codebase_search", "an executor with no data_dir has no index to serve")
}

func (s *SurfaceSuite) TestABadRequestIsRefusedAndNamesNoToken() {
	status, raw := s.call(executorapi.PathMCPOpen, map[string]any{
		"run_id": "cli-run-2", "workspace": "../outside",
		"cloud_mcp": map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": s.cloudToken, "server_name": "tasktrooper"},
	})
	s.Equal(http.StatusBadRequest, status)
	s.NotContains(string(raw), s.cloudToken)

	status, raw = s.call(executorapi.PathMCPOpen, map[string]any{
		"run_id":    "cli-run-3",
		"cloud_mcp": map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": "revoked-token-0000000000", "server_name": "tasktrooper"},
	})
	s.Equal(http.StatusBadGateway, status, string(raw))
	s.NotContains(string(raw), "revoked-token-0000000000")
}

func (s *SurfaceSuite) cloudAuthSnapshot() []string {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return append([]string(nil), s.cloudAuth...)
}

func (s *SurfaceSuite) surfaceGone(opened openedSurface) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		session, err := connect(ctx, opened.URL, opened.Token)
		cancel()
		if err != nil {
			return
		}
		_ = session.Close()
		if time.Now().After(deadline) {
			s.Fail("the surface still answers")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
