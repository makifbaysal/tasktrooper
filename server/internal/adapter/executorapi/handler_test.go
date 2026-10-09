package executorapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const (
	testToken = "0123456789abcdef-token"
	testKey   = "sk-live-0123456789"
)

type frameLine struct {
	V       int               `json:"v"`
	ID      string            `json:"id"`
	Event   string            `json:"event"`
	Payload map[string]any    `json:"payload"`
	OK      *bool             `json:"ok"`
	Result  map[string]any    `json:"result"`
	Error   *executor.Failure `json:"error"`
}

type HandlerSuite struct {
	suite.Suite
	llm     *mocks.LLMClient
	svc     *executor.Service
	server  *httptest.Server
	running chan struct{}
}

func TestHandlerSuite(t *testing.T) {
	suite.Run(t, new(HandlerSuite))
}

func (s *HandlerSuite) SetupTest() {
	s.llm = &mocks.LLMClient{}
	s.running = make(chan struct{}, 1)
	s.svc = executor.NewService(executor.Deps{
		LLM:           s.llm,
		Providers:     map[string]executor.Provider{"main": {Type: domain.LLMProviderOpenAI, DefaultModel: "gpt-4o"}},
		WorkspaceRoot: s.T().TempDir(),
		Limits:        executor.Limits{MaxIterations: 5, TaskMaxIterations: 5, History: appcontext.Budget{KeepRecentMessages: 10}},
	})
	s.server = httptest.NewServer(NewHandler(s.svc, Options{Token: testToken, Version: "test", Secrets: []string{testKey}}))
}

func (s *HandlerSuite) TearDownTest() {
	s.server.Close()
	s.llm.AssertExpectations(s.T())
}

func (s *HandlerSuite) request(ctx context.Context, method, path, token string, body any) *http.Response {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		s.Require().NoError(err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.server.URL+path, reader)
	s.Require().NoError(err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	return resp
}

func (s *HandlerSuite) errorOf(resp *http.Response) executor.Failure {
	defer resp.Body.Close()
	var body struct {
		V     int              `json:"v"`
		Error executor.Failure `json:"error"`
	}
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(1, body.V)
	return body.Error
}

func (s *HandlerSuite) frames(resp *http.Response) []frameLine {
	defer resp.Body.Close()
	s.Equal("application/x-ndjson", resp.Header.Get("Content-Type"))
	var frames []frameLine
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var f frameLine
		s.Require().NoError(json.Unmarshal(scanner.Bytes(), &f), scanner.Text())
		frames = append(frames, f)
	}
	return frames
}

func chatRun(id string) map[string]any {
	return map[string]any{
		"id":     id,
		"run_id": "run-9",
		"kind":   "chat",
		"agent":  map[string]any{"name": "pm", "system_prompt": "You plan.", "provider_id": "main"},
		"prompt": "Plan it.",
	}
}

func (s *HandlerSuite) TestEveryRouteRequiresTheToken() {
	routes := []struct{ method, path string }{
		{http.MethodGet, PathHealth},
		{http.MethodPost, PathAgentRun},
		{http.MethodPost, PathLLMComplete},
		{http.MethodPost, PathCancel},
		{http.MethodPost, PathIndexEnsure},
		{http.MethodPost, PathIndexSearch},
		{http.MethodPost, PathEmbeddings},
		{http.MethodPost, PathMCPOpen},
		{http.MethodPost, PathMCPClose},
		{http.MethodPost, PathMCPCalls},
		{http.MethodGet, "/nowhere"},
	}
	for _, route := range routes {
		for _, token := range []string{"", "wrong-token-0123456789"} {
			s.Run(route.method+" "+route.path+" token="+token, func() {
				resp := s.request(context.Background(), route.method, route.path, token, nil)

				s.Equal(http.StatusUnauthorized, resp.StatusCode)
				s.Equal(codeUnauthorized, s.errorOf(resp).Code)
			})
		}
	}
}

// An executor built without a surface server says so, and advertises no local
// tools; a close names its run.
func (s *HandlerSuite) TestMCPOpenWithoutASurfaceServerIsNotReady() {
	resp := s.request(context.Background(), http.MethodPost, PathMCPOpen, testToken, map[string]any{
		"run_id": "cli-1", "cloud_mcp": map[string]any{"url": "https://cloud.example/api/mcp", "token": "cloud-token-0123456789", "server_name": "tasktrooper"},
	})
	s.Equal(http.StatusServiceUnavailable, resp.StatusCode)
	s.Equal(executor.CodeNotReady, s.errorOf(resp).Code)

	resp = s.request(context.Background(), http.MethodPost, PathMCPClose, testToken, map[string]any{})
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	resp = s.request(context.Background(), http.MethodGet, PathHealth, testToken, nil)
	defer resp.Body.Close()
	var body map[string]any
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.NotContains(body, "local_tools")
}

func (s *HandlerSuite) TestHealthReportsTheProtocol() {
	resp := s.request(context.Background(), http.MethodGet, PathHealth, testToken, nil)
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
	var body healthResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(healthResponse{OK: true, Version: "test", Protocol: 1}, body)
}

func (s *HandlerSuite) TestUnknownRoutesAndIndexCallsWithoutAnIndex() {
	tests := []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{http.MethodGet, "/exec/nothing", nil, http.StatusNotFound, codeUnsupportedMethod},
		{http.MethodGet, PathAgentRun, nil, http.StatusNotFound, codeUnsupportedMethod},
		{http.MethodPost, PathIndexEnsure, map[string]any{"workspace": ".", "repo_key": "app"}, http.StatusServiceUnavailable, executor.CodeNotReady},
		{http.MethodPost, PathIndexSearch, map[string]any{"repo_key": "app", "query": "auth"}, http.StatusServiceUnavailable, executor.CodeNotReady},
	}
	for _, tt := range tests {
		s.Run(tt.method+" "+tt.path, func() {
			resp := s.request(context.Background(), tt.method, tt.path, testToken, tt.body)

			s.Equal(tt.status, resp.StatusCode)
			s.Equal(tt.code, s.errorOf(resp).Code)
		})
	}
}

func (s *HandlerSuite) TestAgentRunStreamsStartedThenEventsThenOneDone() {
	s.llm.On("ChatStream", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { args.Get(2).(func(string))("Plan: ship it.") }).
		Return(domain.AgentResponse{
			Message: domain.Message{Role: domain.RoleAssistant, Content: "Plan: ship it."},
			Usage:   domain.Usage{PromptTokens: 12, CompletionTokens: 4},
		}, nil).Once()

	frames := s.frames(s.request(context.Background(), http.MethodPost, PathAgentRun, testToken, chatRun("call-1")))

	s.Require().GreaterOrEqual(len(frames), 3)
	s.Equal("started", frames[0].Event)
	last := frames[len(frames)-1]
	s.Equal("done", last.Event)
	s.Require().NotNil(last.OK)
	s.True(*last.OK)
	s.Equal("Plan: ship it.", last.Result["final_text"])
	s.Equal([]any{}, last.Result["tool_usage"])
	kinds := map[string]int{}
	for i, f := range frames {
		s.Equal(1, f.V)
		s.Equal("call-1", f.ID)
		if i > 0 && i < len(frames)-1 {
			s.Equal("event", f.Event)
			s.InDelta(float64(i), f.Payload["seq"], 0)
			kinds[f.Payload["kind"].(string)]++
		}
	}
	s.Equal(1, kinds[executor.EventText])
	s.Equal(1, kinds[executor.EventUsage])
	s.Positive(kinds[executor.EventStep])
}

func (s *HandlerSuite) TestARunTheExecutorRefusesIsAnHTTPErrorNotAStream() {
	run := chatRun("")
	run["agent"] = map[string]any{"provider_id": "elsewhere"}

	resp := s.request(context.Background(), http.MethodPost, PathAgentRun, testToken, run)

	s.Equal(http.StatusBadRequest, resp.StatusCode)
	s.Equal(executor.CodeBadRequest, s.errorOf(resp).Code)
}

func (s *HandlerSuite) blockingStream() {
	s.llm.On("ChatStream", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			s.running <- struct{}{}
			<-args.Get(0).(context.Context).Done()
		}).
		Return(domain.AgentResponse{}, context.Canceled).Once()
}

func (s *HandlerSuite) TestCancelByTheStreamIDEndsTheRun() {
	s.blockingStream()
	streamed := make(chan []frameLine, 1)
	go func() {
		streamed <- s.frames(s.request(context.Background(), http.MethodPost, PathAgentRun, testToken, chatRun("call-7")))
	}()
	<-s.running

	resp := s.request(context.Background(), http.MethodPost, PathCancel, testToken, map[string]string{"id": "call-7"})
	defer resp.Body.Close()
	var cancelled cancelResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&cancelled))
	s.Equal(cancelResponse{V: 1, RunID: "run-9", Cancelled: true}, cancelled)

	select {
	case frames := <-streamed:
		last := frames[len(frames)-1]
		s.Equal("done", last.Event)
		s.False(*last.OK)
		s.Equal(executor.CodeCancelled, last.Error.Code)
	case <-time.After(5 * time.Second):
		s.Fail("the stream did not end after the cancel")
	}
}

func (s *HandlerSuite) TestCancelOfARunThatIsNotRunningIsNotAnError() {
	resp := s.request(context.Background(), http.MethodPost, PathCancel, testToken, map[string]string{"run_id": "gone"})
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
	var cancelled cancelResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&cancelled))
	s.False(cancelled.Cancelled)
}

func (s *HandlerSuite) TestACallerThatDisconnectsCancelsTheRun() {
	s.blockingStream()
	ctx, disconnect := context.WithCancel(context.Background())
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.server.URL+PathAgentRun, strings.NewReader(mustJSON(chatRun(""))))
		req.Header.Set("Authorization", "Bearer "+testToken)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	<-s.running
	s.Equal(1, s.svc.ActiveRuns())

	disconnect()

	s.Eventually(func() bool { return s.svc.ActiveRuns() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func (s *HandlerSuite) TestProviderKeysNeverLeaveInAResponse() {
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{}, errors.New(`401: Incorrect API key provided: `+testKey)).Once()

	resp := s.request(context.Background(), http.MethodPost, PathLLMComplete, testToken, map[string]any{
		"provider_id": "main", "messages": []map[string]string{{"role": "user", "content": "title"}},
	})
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)

	s.NotContains(string(raw), testKey)
	s.Contains(string(raw), "[redacted]")
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestRedactor(t *testing.T) {
	tests := []struct {
		name    string
		secrets []string
		in      string
		want    string
	}{
		{"raw secret", []string{"sk-0123456789"}, `{"m":"key sk-0123456789 bad"}`, `{"m":"key [redacted] bad"}`},
		{"escaped secret", []string{"ab\"cdefgh</"}, mustJSON(map[string]string{"m": "ab\"cdefgh</"}), `{"m":"[redacted]"}`},
		{"short values are left alone", []string{"abc", ""}, `{"m":"abc"}`, `{"m":"abc"}`},
		{"every secret", []string{"first-secret", "second-secret"}, "first-secret second-secret", "[redacted] [redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(newRedactor(tt.secrets...)([]byte(tt.in))))
		})
	}
}
