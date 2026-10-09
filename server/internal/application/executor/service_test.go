package executor

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const workspaceRel = "repos/demo/task-1"

type recordingSink struct {
	mu     sync.Mutex
	events []Event
	fail   error
}

func (s *recordingSink) Send(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.events = append(s.events, event)
	return nil
}

func (s *recordingSink) snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

type ServiceSuite struct {
	suite.Suite
	llm     *mocks.LLMClient
	remote  *mocks.RemoteToolConnector
	local   *mocks.ToolExecutor
	root    string
	svc     *Service
	closed  bool
	running chan struct{}
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	s.llm = &mocks.LLMClient{}
	s.remote = mocks.NewRemoteToolConnector(s.T())
	s.local = s.tool("read_file")
	s.root = s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(s.root, workspaceRel), 0o755))
	s.closed = false
	s.running = make(chan struct{}, 1)
	s.svc = NewService(Deps{
		LLM:            s.llm,
		Providers:      map[string]Provider{"anthropic-main": {Type: domain.LLMProviderAnthropic, DefaultModel: "claude-sonnet-4-5"}},
		WorkspaceRoot:  s.root,
		WorkspaceTools: []port.ToolExecutor{s.local},
		Remote:         s.remote,
		Limits: Limits{
			MaxIterations: 10, TaskMaxIterations: 10, MaxToolOutputChars: 16000,
			History: appcontext.Budget{KeepRecentMessages: 10},
		},
	})
}

func (s *ServiceSuite) TearDownTest() {
	s.llm.AssertExpectations(s.T())
}

func (s *ServiceSuite) tool(name string) *mocks.ToolExecutor {
	tool := mocks.NewToolExecutor(s.T())
	tool.On("Name").Return(name).Maybe()
	tool.On("Definition").Return(domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: name, Description: name, Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}).Maybe()
	return tool
}

func (s *ServiceSuite) run(kind string) AgentRun {
	return AgentRun{
		RunID: "run-1",
		Kind:  kind,
		Agent: AgentSpec{
			Name: "backend-developer", SystemPrompt: "You build things.", ProviderID: "anthropic-main",
			ToolPolicy: domain.ToolPolicy{AllowTools: []string{"read_file", "list_board_tasks", "codebase_search"}},
		},
		Prompt:    "Do the task.",
		Workspace: workspaceRel,
		MCP:       &RemoteTools{URL: "https://cloud.example/api/mcp", Token: "run-token-123456", ServerName: "tasktrooper"},
	}
}

func (s *ServiceSuite) expectRemote(tools ...port.ToolExecutor) {
	s.remote.On("Connect", mock.Anything, port.RemoteToolEndpoint{
		URL: "https://cloud.example/api/mcp", Token: "run-token-123456", ServerName: "tasktrooper",
	}).Return(tools, func() { s.closed = true }, nil).Once()
}

func toolCallTurn(id, name, args string, usage domain.Usage) domain.AgentResponse {
	return domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: id, Type: "function", Function: domain.FunctionCall{Name: name, Arguments: args}},
		}},
		Usage:      usage,
		StopReason: domain.StopReasonToolUse,
	}
}

func finalTurn(text string) domain.AgentResponse {
	return domain.AgentResponse{
		Message:    domain.Message{Role: domain.RoleAssistant, Content: text},
		Usage:      domain.Usage{PromptTokens: 30, CompletionTokens: 5},
		StopReason: domain.StopReasonEnd,
	}
}

func toolNames(req domain.AgentRequest) []string {
	names := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		names = append(names, t.Function.Name)
	}
	return names
}

func (s *ServiceSuite) execute(run AgentRun, sink Sink) (*RunResult, *Failure) {
	prepared, failure := s.svc.Prepare(context.Background(), run)
	s.Require().Nil(failure)
	return prepared.Execute(sink)
}

func (s *ServiceSuite) TestBoardRunCallsALocalAndARemoteToolAndStreamsTheFramesInOrder() {
	remoteBoard := s.tool("list_board_tasks")
	remoteBoard.On("Execute", mock.Anything, "{}").Return(domain.ToolResult{Name: "list_board_tasks", Content: `[{"key":"T-1"}]`}).Once()
	shadowedRead := s.tool("read_file")
	withheldSearch := s.tool("codebase_search")
	s.expectRemote(remoteBoard, shadowedRead, withheldSearch)
	s.local.On("Execute", mock.Anything, `{"path":"main.go"}`).Return(domain.ToolResult{Name: "read_file", Content: "package main"}).Once()

	var firstRequest domain.AgentRequest
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { firstRequest = args.Get(1).(domain.AgentRequest) }).
		Return(toolCallTurn("c1", "read_file", `{"path":"main.go"}`, domain.Usage{PromptTokens: 10, CompletionTokens: 2}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(toolCallTurn("c2", "list_board_tasks", "{}", domain.Usage{PromptTokens: 20, CompletionTokens: 3}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("All done."), nil).Once()

	sink := &recordingSink{}
	result, failure := s.execute(s.run(KindBoard), sink)

	s.Require().Nil(failure)
	s.Equal("All done.", result.FinalText)
	s.Equal([]string{"list_board_tasks", "read_file"}, result.ToolUsage)
	s.Equal(map[string]int{"list_board_tasks": 1, "read_file": 1}, result.ToolCounts)
	s.Equal(UsageTotals{LLMCalls: 3, PromptTokens: 60, CompletionTokens: 10, TotalTokens: 70}, result.Usage)
	s.True(s.closed)

	s.Equal([]string{"list_board_tasks", "read_file"}, toolNames(firstRequest))
	s.Equal(domain.LLMProviderType("anthropic-main"), firstRequest.ProviderType)
	s.Equal("claude-sonnet-4-5", firstRequest.Model)
	s.Equal(domain.RoleSystem, firstRequest.Messages[0].Role)
	s.Equal("Do the task.", firstRequest.Messages[1].Content)

	events := sink.snapshot()
	var calls []*ToolCallEvent
	var results []*ToolResultEvent
	var usages, steps int
	for i, ev := range events {
		s.Equal(int64(i+1), ev.header().Seq)
		switch e := ev.(type) {
		case *ToolCallEvent:
			calls = append(calls, e)
		case *ToolResultEvent:
			results = append(results, e)
		case *UsageEvent:
			usages++
		case *StepEvent:
			steps++
		}
	}
	s.Require().Len(calls, 2)
	s.Equal(ToolCallEvent{EventHeader: calls[0].EventHeader, CallID: "c1", Name: "read_file", Source: SourceLocal, Arguments: `{"path":"main.go"}`}, *calls[0])
	s.Equal(ToolCallEvent{EventHeader: calls[1].EventHeader, CallID: "c2", Name: "list_board_tasks", Source: SourceRemote, Arguments: "{}"}, *calls[1])
	s.Require().Len(results, 2)
	s.Equal("package main", results[0].Content)
	s.Equal(SourceRemote, results[1].Source)
	s.Less(calls[0].Seq, results[0].Seq)
	s.Less(results[0].Seq, calls[1].Seq)
	s.Equal(3, usages)
	s.Positive(steps)
	s.Zero(s.svc.ActiveRuns())
}

func (s *ServiceSuite) TestChatRunStreamsTextDeltas() {
	run := s.run(KindChat)
	run.MCP = nil
	s.llm.On("ChatStream", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			onToken := args.Get(2).(func(string))
			onToken("Hel")
			onToken("lo")
		}).
		Return(finalTurn("Hello"), nil).Once()

	sink := &recordingSink{}
	result, failure := s.execute(run, sink)

	s.Require().Nil(failure)
	s.Equal("Hello", result.FinalText)
	s.Empty(result.ToolUsage)
	var deltas []string
	for _, ev := range sink.snapshot() {
		if text, ok := ev.(*TextEvent); ok {
			deltas = append(deltas, text.Delta)
		}
	}
	s.Equal([]string{"Hel", "lo"}, deltas)
}

func (s *ServiceSuite) blockingChat() {
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			s.running <- struct{}{}
			<-args.Get(0).(context.Context).Done()
		}).
		Return(domain.AgentResponse{}, context.Canceled).Once()
}

func (s *ServiceSuite) TestCancelEndsTheRunAsCancelled() {
	run := s.run(KindBoard)
	run.MCP = nil
	s.blockingChat()
	prepared, failure := s.svc.Prepare(context.Background(), run)
	s.Require().Nil(failure)

	type outcome struct {
		result  *RunResult
		failure *Failure
	}
	done := make(chan outcome, 1)
	go func() {
		result, failure := prepared.Execute(&recordingSink{})
		done <- outcome{result, failure}
	}()
	<-s.running
	s.True(s.svc.Cancel("run-1"))

	select {
	case out := <-done:
		s.Nil(out.result)
		s.Require().NotNil(out.failure)
		s.Equal(CodeCancelled, out.failure.Code)
		s.NotNil(out.failure.Run)
	case <-time.After(5 * time.Second):
		s.Fail("the run did not end after its cancel")
	}
	s.False(s.svc.Cancel("run-1"))
	s.Zero(s.svc.ActiveRuns())
}

func (s *ServiceSuite) TestTimeoutEndsTheRunAsTimeout() {
	run := s.run(KindBoard)
	run.MCP = nil
	run.TimeoutMS = 50
	s.blockingChat()

	_, failure := s.execute(run, &recordingSink{})

	s.Require().NotNil(failure)
	s.Equal(CodeTimeout, failure.Code)
}

func (s *ServiceSuite) TestCallerThatStopsReadingCancelsTheRun() {
	run := s.run(KindBoard)
	run.MCP = nil

	_, failure := s.execute(run, &recordingSink{fail: errors.New("broken pipe")})

	s.Require().NotNil(failure)
	s.Equal(CodeCancelled, failure.Code)
	s.Equal(errCallerGone.Error(), failure.Message)
}

func (s *ServiceSuite) TestRunOutOfTurnsReportsBudgetExhaustedWithItsPartialSummary() {
	run := s.run(KindBoard)
	run.MCP = nil
	run.Agent.MaxTurns = 1
	s.local.On("Execute", mock.Anything, `{"path":"main.go"}`).Return(domain.ToolResult{Name: "read_file", Content: "package main"}).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(toolCallTurn("c1", "read_file", `{"path":"main.go"}`, domain.Usage{PromptTokens: 10}), nil).Once()
	s.llm.On("Chat", mock.Anything, mock.Anything).Return(finalTurn("Read main.go; nothing written yet."), nil).Once()

	_, failure := s.execute(run, &recordingSink{})

	s.Require().NotNil(failure)
	s.Equal(CodeBudgetExhausted, failure.Code)
	s.Equal("Read main.go; nothing written yet.", failure.Partial)
	s.Equal([]string{"read_file"}, failure.Run.ToolUsage)
}

func (s *ServiceSuite) TestUnreachableCoordinationEndpointFailsAsUpstream() {
	s.remote.On("Connect", mock.Anything, mock.Anything).Return(nil, nil, errors.New("connection refused")).Once()

	_, failure := s.execute(s.run(KindBoard), &recordingSink{})

	s.Require().NotNil(failure)
	s.Equal(CodeUpstream, failure.Code)
}

func (s *ServiceSuite) TestARunIDIsHeldUntilItsRunEnds() {
	first, failure := s.svc.Prepare(context.Background(), s.run(KindBoard))
	s.Require().Nil(failure)

	_, failure = s.svc.Prepare(context.Background(), s.run(KindBoard))
	s.Require().NotNil(failure)
	s.Equal(CodeConflict, failure.Code)

	first.Release()
	again, failure := s.svc.Prepare(context.Background(), s.run(KindBoard))
	s.Require().Nil(failure)
	again.Release()
}

func (s *ServiceSuite) TestShutdownRefusesNewRuns() {
	s.svc.Shutdown()

	_, failure := s.svc.Prepare(context.Background(), s.run(KindBoard))

	s.Require().NotNil(failure)
	s.Equal(CodeCancelled, failure.Code)
}

func (s *ServiceSuite) TestPrepareRejectsInvalidRuns() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.root, "file.txt"), []byte("x"), 0o644))
	tests := []struct {
		name   string
		mutate func(*AgentRun)
	}{
		{"no run id", func(r *AgentRun) { r.RunID = " " }},
		{"unknown kind", func(r *AgentRun) { r.Kind = "batch" }},
		{"unknown provider", func(r *AgentRun) { r.Agent.ProviderID = "openai-other" }},
		{"no provider", func(r *AgentRun) { r.Agent.ProviderID = "" }},
		{"nothing to say", func(r *AgentRun) { r.Prompt = ""; r.Messages = nil }},
		{"bad role", func(r *AgentRun) { r.Messages = []domain.Message{{Role: "robot", Content: "hi"}} }},
		{"workspace outside the root", func(r *AgentRun) { r.Workspace = "../../etc" }},
		{"workspace not prepared", func(r *AgentRun) { r.Workspace = "repos/demo/task-2" }},
		{"workspace is a file", func(r *AgentRun) { r.Workspace = "file.txt" }},
		{"negative timeout", func(r *AgentRun) { r.TimeoutMS = -1 }},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			run := s.run(KindBoard)
			tt.mutate(&run)

			_, failure := s.svc.Prepare(context.Background(), run)

			s.Require().NotNil(failure)
			s.Equal(CodeBadRequest, failure.Code)
		})
	}
	s.Zero(s.svc.ActiveRuns())
}

func (s *ServiceSuite) TestCompleteAnswersAOneShotCall() {
	s.llm.On("Chat", mock.Anything, mock.MatchedBy(func(req domain.AgentRequest) bool {
		return req.ProviderType == "anthropic-main" && req.Model == "claude-haiku-4-5" && req.MaxTokens == 64 &&
			len(req.Messages) == 2 && req.Messages[0].Role == domain.RoleSystem && len(req.Tools) == 0
	})).Return(finalTurn("feat: add login"), nil).Once()

	completion, failure := s.svc.Complete(context.Background(), CompletionRequest{
		ProviderID: "anthropic-main", Model: "claude-haiku-4-5", System: "Write a commit message.",
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "diff"}}, MaxTokens: 64,
	})

	s.Require().Nil(failure)
	s.Equal("feat: add login", completion.Text)
	s.Equal(30, completion.Usage.PromptTokens)
}

func (s *ServiceSuite) TestCompleteMapsAProviderRateLimit() {
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{}, &domain.LLMHTTPError{StatusCode: http.StatusTooManyRequests, Body: "slow down", RetryAfter: 2 * time.Second}).Once()

	_, failure := s.svc.Complete(context.Background(), CompletionRequest{
		ProviderID: "anthropic-main", Messages: []domain.Message{{Role: domain.RoleUser, Content: "title this"}},
	})

	s.Require().NotNil(failure)
	s.Equal(CodeRateLimited, failure.Code)
	s.Equal(int64(2000), failure.RetryAfterMS)
}

func (s *ServiceSuite) TestCompleteRejectsInvalidCalls() {
	tests := []struct {
		name string
		req  CompletionRequest
	}{
		{"unknown provider", CompletionRequest{ProviderID: "nope", Messages: []domain.Message{{Role: domain.RoleUser, Content: "x"}}}},
		{"no messages", CompletionRequest{ProviderID: "anthropic-main"}},
		{"tool message", CompletionRequest{ProviderID: "anthropic-main", Messages: []domain.Message{{Role: domain.RoleTool, Content: "x"}}}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, failure := s.svc.Complete(context.Background(), tt.req)

			s.Require().NotNil(failure)
			s.Equal(CodeBadRequest, failure.Code)
		})
	}
}

func TestRemoteWithheld(t *testing.T) {
	tests := []struct {
		name     string
		withheld bool
	}{
		{"download_file", true},
		{"start_task_preview", true},
		{"commit_task_changes", true},
		{"deploy_release", true},
		{"codebase_search", true},
		{"browser_navigate", true},
		{"mcp_filesystem_read_file", true},
		{"list_board_tasks", false},
		{"ask_user", false},
		{"browser", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.withheld, remoteWithheld(tt.name))
		})
	}
}

func (s *ServiceSuite) TestCompleteBlamesTheProviderForAFailureItCannotName() {
	s.llm.On("Chat", mock.Anything, mock.Anything).
		Return(domain.AgentResponse{}, errors.New("http request: dial tcp 127.0.0.1:9: connect: connection refused")).Once()

	_, failure := s.svc.Complete(context.Background(), CompletionRequest{
		ProviderID: "anthropic-main", Messages: []domain.Message{{Role: domain.RoleUser, Content: "title this"}},
	})

	s.Require().NotNil(failure)
	s.Equal(CodeUpstream, failure.Code)
}
