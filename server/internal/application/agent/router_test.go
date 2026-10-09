package agent_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type cliExecutor struct {
	supports domain.LLMProviderType
	resp     domain.AgentResponse
	err      error

	mu       sync.Mutex
	calls    int
	last     domain.TaskExecution
	dirIsSet bool
	dirEmpty bool
}

var _ port.TaskExecutor = (*cliExecutor)(nil)

func (e *cliExecutor) Supports(provider domain.LLMProviderType) bool {
	return provider == e.supports
}

func (e *cliExecutor) Execute(_ context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.last = req
	if info, err := os.Stat(req.WorkDir); err == nil && info.IsDir() {
		e.dirIsSet = true
		entries, _ := os.ReadDir(req.WorkDir)
		e.dirEmpty = len(entries) == 0
	}
	return e.resp, e.err
}

func (e *cliExecutor) snapshot() (int, domain.TaskExecution) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.last
}

func newRouter(t *testing.T, ex port.TaskExecutor) (*agent.Router, *mocks.LLMClient, *mocks.ToolRegistry) {
	t.Helper()
	llm := new(mocks.LLMClient)
	reg := new(mocks.ToolRegistry)
	router := agent.NewRouter(agent.NewLoop(llm, reg, 5, 5, 16000))
	if ex != nil {
		router.SetTaskExecutor(ex)
	}
	t.Cleanup(func() {
		llm.AssertExpectations(t)
		reg.AssertExpectations(t)
	})
	return router, llm, reg
}

func TestRouterSendsHostExecutedRunsToTheExecutor(t *testing.T) {
	ex := &cliExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "done"}},
	}
	router, _, _ := newRouter(t, ex)

	dir := t.TempDir()
	ctx := registry.ContextWithWorkspaceDir(context.Background(), dir)
	messages := []domain.Message{{Role: domain.RoleUser, Content: "fix the build"}}
	policy := domain.ToolPolicy{AllowTools: []string{"read_file"}}

	resp, err := router.RunTask(ctx, messages, "opus", domain.LLMProviderClaudeCode, policy,
		agent.WithCLILabel("tt-42 verify-fix", "wire the gate"))

	require.NoError(t, err)
	require.Equal(t, "done", resp.Message.Content)

	calls, req := ex.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, dir, req.WorkDir)
	require.Equal(t, "opus", req.Model)
	require.Equal(t, domain.LLMProviderClaudeCode, req.Provider)
	require.Equal(t, policy, req.Policy, "the policy the loop would have enforced travels unchanged")
	require.Equal(t, messages, req.History)
	require.Equal(t, "tt-42 verify-fix", req.TaskKey)
	require.Empty(t, req.ResumeSessionID, "a router has no session of its own to resume")
}

func TestRouterPrefersTheSubtaskWorkspace(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode}
	router, _, _ := newRouter(t, ex)

	parent := t.TempDir()
	subtask := t.TempDir()
	ctx := registry.ContextWithSubtaskWorkspace(registry.ContextWithWorkspaceDir(context.Background(), parent), subtask)

	_, err := router.RunTask(ctx, nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{})
	require.NoError(t, err)

	_, req := ex.snapshot()
	require.Equal(t, subtask, req.WorkDir)
}

func TestRouterLeavesHTTPProvidersOnTheLoop(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode}
	router, llm, reg := newRouter(t, ex)

	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	reg.On("DefinitionsForPolicy", domain.ToolPolicy{}).Return([]domain.ToolDefinition{})
	llm.On("Chat", mock.Anything, mock.Anything).Return(
		domain.AgentResponse{Message: domain.Message{Content: "hi"}}, nil)

	resp, err := router.Run(context.Background(), messages, "gpt-4o", domain.LLMProviderOpenAI, domain.ToolPolicy{})
	require.NoError(t, err)
	require.Equal(t, "hi", resp.Message.Content)

	calls, _ := ex.snapshot()
	require.Zero(t, calls)
}

func TestRouterRefusesWithNoExecutor(t *testing.T) {
	router, _, _ := newRouter(t, nil)

	_, err := router.RunTask(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		nil, "opus", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "local runner host")
}

func TestRouterRefusesAnExecutorThatDoesNotSupportTheProvider(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderType("some_other_cli")}
	router, _, _ := newRouter(t, ex)

	_, err := router.RunTask(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

	require.Error(t, err)
	calls, _ := ex.snapshot()
	require.Zero(t, calls, "an executor must never be handed a provider it disclaimed")
}

func TestRouterRefusesWhenThereIsNoWorkspace(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode}
	router, _, _ := newRouter(t, ex)

	_, err := router.RunTask(context.Background(), nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "working directory")
	calls, _ := ex.snapshot()
	require.Zero(t, calls)
}

func TestRouterGivesAScratchWorkspaceWhenAskedTo(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode}
	router, _, _ := newRouter(t, ex)

	_, err := router.Run(context.Background(), nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{},
		agent.WithScratchWorkspace())
	require.NoError(t, err)

	calls, req := ex.snapshot()
	require.Equal(t, 1, calls)
	require.NotEmpty(t, req.WorkDir)
	require.True(t, ex.dirIsSet, "the scratch directory must exist while the session runs")
	require.True(t, ex.dirEmpty, "a scratch workspace is empty by definition")
	_, statErr := os.Stat(req.WorkDir)
	require.True(t, os.IsNotExist(statErr), "the scratch workspace must be removed afterwards")
}

func TestRouterPassesExecutorErrorsThrough(t *testing.T) {
	sentinel := errors.New("claude code did not finish in time")
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode, err: sentinel}
	router, _, _ := newRouter(t, ex)

	_, err := router.RunTask(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{})

	require.ErrorIs(t, err, sentinel)
}

func TestRouterResumesTheCLISessionOnAFollowUpStep(t *testing.T) {
	ex := &cliExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "ticked"}, CLISessionID: "sess-main"},
	}
	router, _, _ := newRouter(t, ex)
	ctx := agent.ContextWithCLISession(
		registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		&agent.CLISession{},
	)
	policy := domain.ToolPolicy{}

	main := []domain.Message{{Role: domain.RoleUser, Content: "implement the gate"}}
	_, err := router.RunTask(ctx, main, "opus", domain.LLMProviderClaudeCode, policy,
		agent.WithCLILabel("tt-42", "wire the gate"))
	require.NoError(t, err)

	_, firstReq := ex.snapshot()
	require.Empty(t, firstReq.ResumeSessionID, "the main run has no session to resume")
	require.Equal(t, main, firstReq.History)

	// A follow-up: the assistant's close-out plus one new user prompt, the shape sweepOpenCriteria builds.
	followUp := []domain.Message{
		{Role: domain.RoleUser, Content: "implement the gate"},
		{Role: domain.RoleAssistant, Content: "done, the gate is wired"},
		{Role: domain.RoleUser, Content: "these criteria are still open: the gate refuses a red build"},
	}
	_, err = router.RunTask(ctx, followUp, "opus", domain.LLMProviderClaudeCode, policy,
		agent.WithCLILabel("tt-42 criteria-sweep 1", "wire the gate"))
	require.NoError(t, err)

	calls, secondReq := ex.snapshot()
	require.Equal(t, 2, calls)
	require.Equal(t, "sess-main", secondReq.ResumeSessionID,
		"the follow-up must resume the session the main run's executor returned")
	require.Equal(t, "these criteria are still open: the gate refuses a red build", secondReq.Prompt,
		"only the new instruction is sent, not the whole conversation")
	require.Equal(t, followUp, secondReq.History, "History travels unchanged even though the executor ignores it on a resume")
}

func TestRouterCarriesTheRunEnvironmentIntoFollowUpTurns(t *testing.T) {
	ex := &cliExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "ticked"}, CLISessionID: "sess-main"},
	}
	router, _, _ := newRouter(t, ex)
	env := map[string]string{"NODE_VERSION": "20.11.0", "GOTOOLCHAIN": "go1.24.0"}
	ctx := agent.ContextWithCLISession(
		registry.ContextWithSessionEnv(
			registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()), env),
		&agent.CLISession{},
	)
	policy := domain.ToolPolicy{}

	main := []domain.Message{{Role: domain.RoleUser, Content: "implement the gate"}}
	_, err := router.RunTask(ctx, main, "opus", domain.LLMProviderClaudeCode, policy,
		agent.WithCLILabel("tt-42", "wire the gate"))
	require.NoError(t, err)
	_, firstReq := ex.snapshot()

	followUp := []domain.Message{
		{Role: domain.RoleUser, Content: "implement the gate"},
		{Role: domain.RoleAssistant, Content: "done, the gate is wired"},
		{Role: domain.RoleUser, Content: "these criteria are still open: the gate refuses a red build"},
	}
	_, err = router.RunTask(ctx, followUp, "opus", domain.LLMProviderClaudeCode, policy,
		agent.WithCLILabel("tt-42 criteria-sweep 1", "wire the gate"))
	require.NoError(t, err)
	calls, secondReq := ex.snapshot()

	require.Equal(t, 2, calls)
	require.Equal(t, env, secondReq.Env,
		"a follow-up turn must hand the executor the environment the run was resolved with")
	require.Equal(t, firstReq.Env, secondReq.Env,
		"a follow-up turn must hand the executor the same environment as the original run")
	require.Equal(t, env, firstReq.Env)
}

func TestRouterFallsBackToFullHistoryWhenThereIsNoTrailingUserInstruction(t *testing.T) {
	ex := &cliExecutor{
		supports: domain.LLMProviderClaudeCode,
		resp:     domain.AgentResponse{Message: domain.Message{Content: "ok"}, CLISessionID: "sess-1"},
	}
	router, _, _ := newRouter(t, ex)
	session := &agent.CLISession{}
	session.Set("sess-1")
	ctx := agent.ContextWithCLISession(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()), session)

	// A history that does not end on a user turn has nothing new to resume with.
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "implement the gate"},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	_, err := router.RunTask(ctx, messages, "opus", domain.LLMProviderClaudeCode, domain.ToolPolicy{})
	require.NoError(t, err)

	_, req := ex.snapshot()
	require.Empty(t, req.ResumeSessionID, "no trailing user instruction means no resume")
	require.Empty(t, req.Prompt)
	require.Equal(t, messages, req.History)
}

func TestRouterDoesNotFakeAStreamForHostExecutedProviders(t *testing.T) {
	ex := &cliExecutor{supports: domain.LLMProviderClaudeCode}
	router, _, _ := newRouter(t, ex)

	_, err := router.RunStream(registry.ContextWithWorkspaceDir(context.Background(), t.TempDir()),
		nil, "", domain.LLMProviderClaudeCode, domain.ToolPolicy{}, func(string) {})

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "local runner host"))
	calls, _ := ex.snapshot()
	require.Zero(t, calls)
}
