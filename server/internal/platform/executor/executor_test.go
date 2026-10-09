package executor

import (
	"bufio"
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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/mcpserver"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const providerKey = "sk-test-provider-key-0001"

// scriptedProvider is an OpenAI-compatible endpoint that answers each chat
// request with the next scripted turn and remembers what it was sent.
type scriptedProvider struct {
	mu       sync.Mutex
	turns    []map[string]any
	requests []map[string]any
	auth     []string
	block    chan struct{}
}

func (p *scriptedProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	p.mu.Lock()
	p.requests = append(p.requests, body)
	p.auth = append(p.auth, r.Header.Get("Authorization"))
	block := p.block
	var turn map[string]any
	if len(p.turns) > 0 {
		turn, p.turns = p.turns[0], p.turns[1:]
	}
	p.mu.Unlock()
	if block != nil {
		block <- struct{}{}
		<-r.Context().Done()
		return
	}
	if turn == nil {
		http.Error(w, `{"error":"no scripted turn left"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{turn},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	})
}

func toolCallChoice(id, name, args string) map[string]any {
	return map[string]any{
		"finish_reason": "tool_calls",
		"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}},
		}},
	}
}

func textChoice(text string) map[string]any {
	return map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": text}}
}

type frame struct {
	ID      string          `json:"id"`
	Event   string          `json:"event"`
	Payload map[string]any  `json:"payload"`
	OK      *bool           `json:"ok"`
	Result  json.RawMessage `json:"result"`
	Error   map[string]any  `json:"error"`
}

type ExecutorSuite struct {
	suite.Suite
	provider  *scriptedProvider
	llmServer *httptest.Server
	cloud     *httptest.Server
	board     *mocks.ToolExecutor
	mcpToken  string
	root      string
	server    *Server
	baseURL   string
}

func TestExecutorSuite(t *testing.T) {
	suite.Run(t, new(ExecutorSuite))
}

func (s *ExecutorSuite) SetupTest() {
	s.provider = &scriptedProvider{}
	s.llmServer = httptest.NewServer(s.provider)

	s.board = mocks.NewToolExecutor(s.T())
	s.board.On("Name").Return("list_board_tasks").Maybe()
	s.board.On("Definition").Return(domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{
		Name: "list_board_tasks", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}}).Maybe()
	cloudTools := registry.New()
	cloudTools.Register(s.board)
	tokens := mcpserver.NewRunTokenRegistry()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	mcpserver.New(cloudTools, tokens).Register(app)
	s.cloud = httptest.NewServer(adaptor.FiberApp(app))
	var err error
	s.mcpToken, err = tokens.Mint(mcpserver.Run{
		Ctx:    context.Background(),
		Policy: domain.ToolPolicy{AllowTools: []string{"list_board_tasks", "read_file"}},
	})
	s.Require().NoError(err)

	s.root = s.T().TempDir()
	workspace := filepath.Join(s.root, "repos", "demo", "task-1")
	s.Require().NoError(os.MkdirAll(workspace, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o644))

	var stdout bytes.Buffer
	s.server, err = Start(Config{
		Listen:        defaultListen,
		Token:         validToken,
		WorkspaceRoot: s.root,
		Providers: []ProviderConfig{{
			ID: "byok", Type: typeOpenAICompatible, BaseURL: s.llmServer.URL, APIKey: providerKey, Models: []string{"gpt-test"},
		}},
	}, &stdout, Options{})
	s.Require().NoError(err)
	line := strings.TrimSpace(stdout.String())
	s.Require().True(strings.HasPrefix(line, ListeningPrefix+"http://127.0.0.1:"), line)
	s.baseURL = strings.TrimPrefix(line, ListeningPrefix)
}

func (s *ExecutorSuite) TearDownTest() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.NoError(s.server.Shutdown(ctx))
	s.cloud.Close()
	s.llmServer.Close()
}

func (s *ExecutorSuite) post(path string, body any) *http.Response {
	raw, err := json.Marshal(body)
	s.Require().NoError(err)
	req, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(raw))
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer "+validToken)
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	return resp
}

func (s *ExecutorSuite) frames(resp *http.Response) []frame {
	defer resp.Body.Close()
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	var frames []frame
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		s.NotContains(scanner.Text(), providerKey)
		s.NotContains(scanner.Text(), s.mcpToken)
		var f frame
		s.Require().NoError(json.Unmarshal(scanner.Bytes(), &f))
		frames = append(frames, f)
	}
	return frames
}

func (s *ExecutorSuite) boardRun() map[string]any {
	return map[string]any{
		"run_id": "run-e2e",
		"kind":   "board",
		"agent": map[string]any{
			"name": "backend-developer", "system_prompt": "You build.", "provider_id": "byok",
			"tool_policy": map[string]any{"allow_tools": []string{"read_file", "list_board_tasks"}},
		},
		"prompt":    "Read main.go, then check the board.",
		"workspace": "repos/demo/task-1",
		"mcp":       map[string]any{"url": s.cloud.URL + mcpserver.Path, "token": s.mcpToken, "server_name": "tasktrooper"},
	}
}

func (s *ExecutorSuite) TestBoardRunUsesALocalToolAndACloudToolAndReportsBoth() {
	s.provider.turns = []map[string]any{
		toolCallChoice("c1", "read_file", `{"path":"main.go"}`),
		toolCallChoice("c2", "list_board_tasks", `{}`),
		textChoice("Read it and checked the board."),
	}
	s.board.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Name: "list_board_tasks", Content: `[{"key":"T-1"}]`}).Once()

	frames := s.frames(s.post("/exec/agent.run", s.boardRun()))

	s.Require().NotEmpty(frames)
	s.Equal("started", frames[0].Event)
	var sequence []string
	for _, f := range frames[1 : len(frames)-1] {
		s.Equal("event", f.Event)
		switch kind := f.Payload["kind"]; kind {
		case "tool_call", "tool_result":
			sequence = append(sequence, kind.(string)+":"+f.Payload["name"].(string)+":"+f.Payload["source"].(string))
		}
	}
	s.Equal([]string{
		"tool_call:read_file:local", "tool_result:read_file:local",
		"tool_call:list_board_tasks:remote", "tool_result:list_board_tasks:remote",
	}, sequence)

	done := frames[len(frames)-1]
	s.Equal("done", done.Event)
	s.Require().True(*done.OK, "%v", done.Error)
	var result struct {
		FinalText string   `json:"final_text"`
		ToolUsage []string `json:"tool_usage"`
		Usage     struct {
			LLMCalls int `json:"llm_calls"`
		} `json:"usage"`
		DurationMS *int64 `json:"duration_ms"`
	}
	s.Require().NoError(json.Unmarshal(done.Result, &result))
	s.Equal("Read it and checked the board.", result.FinalText)
	s.Equal([]string{"list_board_tasks", "read_file"}, result.ToolUsage)
	s.Equal(3, result.Usage.LLMCalls)
	s.NotNil(result.DurationMS)

	s.Equal([]string{"Bearer " + providerKey, "Bearer " + providerKey, "Bearer " + providerKey}, s.provider.auth)
	secondTurn := s.provider.requests[1]["messages"].([]any)
	s.Contains(secondTurn[len(secondTurn)-1].(map[string]any)["content"], "package main")
}

func (s *ExecutorSuite) TestRunEnvReachesRunTerminalForThatRunOnly() {
	runWith := func(runID string, env map[string]string) string {
		s.provider.mu.Lock()
		s.provider.turns = []map[string]any{
			toolCallChoice("c1", "run_terminal", `{"command":"echo probe=$TT_PROBE_VERSION"}`),
			textChoice("Printed it."),
		}
		s.provider.mu.Unlock()
		run := s.boardRun()
		run["run_id"] = runID
		run["agent"].(map[string]any)["tool_policy"] = map[string]any{"allow_tools": []string{"run_terminal"}}
		delete(run, "mcp")
		if env != nil {
			run["env"] = env
		}
		frames := s.frames(s.post("/exec/agent.run", run))
		s.Require().True(*frames[len(frames)-1].OK, "%v", frames[len(frames)-1].Error)
		for _, f := range frames {
			if f.Event == "event" && f.Payload["kind"] == "tool_result" && f.Payload["name"] == "run_terminal" {
				return f.Payload["content"].(string)
			}
		}
		s.Fail("no run_terminal result")
		return ""
	}

	s.Contains(runWith("run-env", map[string]string{"TT_PROBE_VERSION": "1.22.1"}), "probe=1.22.1")
	s.Contains(runWith("run-no-env", nil), "probe=\n")
}

func (s *ExecutorSuite) TestARunThatSetsPathIsRefused() {
	run := s.boardRun()
	run["env"] = map[string]string{"PATH": "/opt/cloud/bin"}

	resp := s.post("/exec/agent.run", run)
	defer resp.Body.Close()

	s.Equal(http.StatusBadRequest, resp.StatusCode)
}

func (s *ExecutorSuite) TestOneShotCompletionUsesTheUsersKey() {
	s.provider.turns = []map[string]any{textChoice("Add login form")}

	resp := s.post("/exec/llm.complete", map[string]any{
		"provider_id": "byok", "system": "Title this task.",
		"messages": []map[string]string{{"role": "user", "content": "users cannot sign in"}}, "max_tokens": 32,
	})
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
	var completion struct {
		Text  string `json:"text"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&completion))
	s.Equal("Add login form", completion.Text)
	s.Equal(10, completion.Usage.PromptTokens)
	s.Equal("gpt-test", s.provider.requests[0]["model"])
	s.Equal([]string{"Bearer " + providerKey}, s.provider.auth)
}

func (s *ExecutorSuite) TestShutdownEndsARunningRunWithItsDoneFrame() {
	s.provider.block = make(chan struct{}, 1)
	run := s.boardRun()
	delete(run, "mcp")
	streamed := make(chan []frame, 1)
	go func() { streamed <- s.frames(s.post("/exec/agent.run", run)) }()
	<-s.provider.block

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.Require().NoError(s.server.Shutdown(ctx))

	select {
	case frames := <-streamed:
		done := frames[len(frames)-1]
		s.Equal("done", done.Event)
		s.False(*done.OK)
		s.Equal("cancelled", done.Error["code"])
	case <-time.After(10 * time.Second):
		s.Fail("the run's stream did not end on shutdown")
	}
	s.server = restartedForTeardown(s)
}

// restartedForTeardown gives TearDownTest a live server to shut down after a
// test already shut the original one.
func restartedForTeardown(s *ExecutorSuite) *Server {
	srv, err := Start(Config{Listen: defaultListen, Token: validToken, WorkspaceRoot: s.root}, io.Discard, Options{})
	s.Require().NoError(err)
	return srv
}
