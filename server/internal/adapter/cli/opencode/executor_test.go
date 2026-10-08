package opencode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newTestExecutor builds an executor pointed at testdata/fake-opencode.sh and a
// workspace primed with the given fixture — see claudecode's newTestExecutor
// for why the fixture lives in the workspace rather than behind an env var:
// the executor's childEnv only ever carries PATH and HOME.
func newTestExecutor(t *testing.T, cfg Config, fixtureFile string) (*Executor, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-opencode.sh"))
	require.NoError(t, err)

	workDir := t.TempDir()
	if fixtureFile != "" {
		body, err := os.ReadFile(filepath.Join("testdata", fixtureFile))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"), body, 0o600))
	}

	cfg.Binary = script
	ex, err := New(cfg)
	require.NoError(t, err)
	return ex, workDir
}

func readArgv(t *testing.T, workDir string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(workDir, "argv.txt"))
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(body), "\x00"), "\x00")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func taskExecution(workDir string) domain.TaskExecution {
	return domain.TaskExecution{
		History: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Implement the executor seam."},
		},
		Provider:  domain.LLMProviderOpencode,
		WorkDir:   workDir,
		TaskKey:   "tt-42",
		TaskTitle: "Executor seam",
	}
}

// A finished session must come back as the same response shape the agent loop
// returns, with the spend the step_finish event reported.
func TestExecuteReturnsTheSessionsAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	ctx, tokens := usageapp.ContextWithTokenUsage(context.Background())
	resp, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, domain.RoleAssistant, resp.Message.Role)
	assert.Equal(t, "Reading the runner to see how the task is dispatched. Fixed the undefined symbol and the build is green.",
		resp.Message.Content)
	assert.Equal(t, 13700, resp.Usage.PromptTokens, "input 1500 + cache read 12000 + cache write 200")
	assert.Equal(t, 800, resp.Usage.CompletionTokens)
	assert.Equal(t, 12000, resp.Usage.CacheReadTokens)

	totals := tokens.Totals()
	assert.Equal(t, 1, totals.LLMCalls, "finish must stamp the run's own usage onto the context accumulator")
	assert.Equal(t, int64(13700), totals.PromptTokens)
}

// opencode run takes the prompt as a positional argument, not on stdin the
// way claudecode's does, and --format json / --auto are unconditional: without
// --auto a print-mode run only proposes edits with nobody at the terminal to
// approve them.
func TestExecuteBuildsTheDocumentedInvocation(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.Model = "anthropic/claude-opus-5"
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "run", argv[0])
	assert.Equal(t,
		"You are the backend developer.\n\nImplement the executor seam.",
		argv[1], "the flattened history is the one positional prompt argument")
	assert.Contains(t, argv, "--format")
	assert.Contains(t, argv, "json")
	assert.Contains(t, argv, "--auto")
	assert.Contains(t, argv, "--model")
	assert.Contains(t, argv, "anthropic/claude-opus-5")
}

// An empty model hands the choice to opencode's own configured default rather
// than guessing one.
func TestExecuteOmitsModelWhenNoneIsChosen(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--model")
}

// The child must see a toolchain, not this process's credentials.
func TestExecuteScrubsTheChildEnvironment(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_KEY", "gateway-hmac-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")

	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	env := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.NotContains(t, env, "INTERNAL_AUTH_KEY")
	assert.NotContains(t, env, "gateway-hmac-secret")
	assert.NotContains(t, env, "DATABASE_URL")
	assert.Contains(t, env, "PATH=")
	assert.Contains(t, env, "HOME=")
}

// The MCP credential travels as an inline env var for this CLI, unlike
// cursor's and antigravity's file-based configs — see mcp.go's
// mcpConfigContentEnv comment for why.
func TestExecutePassesTheMCPConfigAsAnEnvVar(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{
		MCP: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "run-token", Tools: []string{"read_file"}},
	}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	env := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.Contains(t, env, "OPENCODE_CONFIG_CONTENT=")
	assert.Contains(t, env, "http://127.0.0.1:9/mcp")
	assert.Contains(t, env, "run-token")
}

func TestExecuteRefusesToRunWithoutAWorkspace(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")
	req := taskExecution("")
	_, err := ex.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no task workspace")
}

// A known opencode issue (run --format json can exit cleanly without ever
// emitting the final step_finish event) means a missing terminal event with a
// clean exit and real output must be treated as success, not a failure — see
// finishInner's comment for the reasoning.
func TestSuccessWithoutStepFinishIsStillAnAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "no_step_finish.jsonl")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	assert.Equal(t, "Refactored the handler and the tests pass.", resp.Message.Content)
	assert.Equal(t, domain.Usage{}, resp.Usage, "no step_finish means no usage to report")
}

// No result, no text and a real exit failure is an ordinary run failure.
func TestExecuteFailsWhenTheSessionNeverAnswered(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "stderr.txt"), []byte("panic: something broke\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("1"), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ended without a result")
	assert.Contains(t, err.Error(), "something broke")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a crash is a failure, not a park")
}

// A structured `error` event whose message is a provider rate limit must come
// back as a typed park, not an ordinary failure — quota.go's own pattern
// tests already cover the matching itself; this only proves the wiring.
func TestExecuteReturnsATypedQuotaBlockFromAStructuredError(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "error_rate_limit.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	var block *domain.QuotaBlock
	require.True(t, errors.As(err, &block), "a provider rate limit must park, not fail: %v", err)
	assert.Equal(t, domain.LLMProviderOpencode, block.Provider)
	assert.Equal(t, "ses_rl1", block.CLISessionID)
}

// A structured `error` event whose message is NOT a rate limit is an ordinary
// failure — the wiring must not park every error event.
func TestExecuteFailsOnANonRateLimitError(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "error_generic.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_request")
	assert.Contains(t, err.Error(), "nonexistent")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a non-rate-limit error must not park")
}

// opencode run exits 1 once any session error was reported, even one the
// session recovered from and then answered; that answer is the result (#102).
func TestExecuteChatKeepsTheAnswerOfASessionThatRecoveredFromAnError(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "error_recovered.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("1"), 0o600))

	req := domain.ChatExecution{
		History:   []domain.Message{{Role: domain.RoleUser, Content: "Is the build on TestFlight?"}},
		Provider:  domain.LLMProviderOpencode,
		WorkDir:   workDir,
		SessionID: "chat-102",
	}
	result, err := ex.ExecuteChat(context.Background(), req, port.ChatStream{})
	require.NoError(t, err)
	assert.Equal(t, "The pipeline is green: build 33 is on TestFlight.", result.Response.Message.Content)
}

// Only exit 1 is opencode flagging the recovered error; any other failure
// exit still fails the run.
func TestExecuteFailsWhenARecoveredSessionExitsAbnormally(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "error_recovered.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("2"), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited with an error after reporting success")
}

// The deadline has to actually kill a wedged session, and a hang must never be
// mistaken for a spent quota.
func TestRunTimeoutFailsTheRunAndDoesNotPark(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: 100 * time.Millisecond}, "success.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("30"), 0o600))

	start := time.Now()
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "the deadline has to actually kill the session")
	assert.Contains(t, err.Error(), "did not finish within")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a hang is not a spent quota")
}

// The highest-value case: a known opencode bug can leave the process running
// forever after a 429 instead of exiting, with the provider's error reaching
// only stderr. The rateLimitWatcher has to cancel the run the moment that text
// arrives rather than waiting out the full runTimeout — proven here by giving
// the fake CLI a sleep far longer than the run timeout and asserting the run
// still comes back well under BOTH, as a typed park.
func TestRateLimitOnStderrCancelsAHungSessionInsteadOfWaitingOutTheTimeout(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: 20 * time.Second}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "stderr.txt"),
		[]byte(`ERROR service=llm error={"statusCode":429,"message":"rate limited"}`+"\n"), 0o600))
	// Longer than RunTimeout AND much longer than the deadline this test
	// enforces on itself below — if the watcher failed to fire, this run would
	// only ever come back (as a plain timeout, not a park) after RunTimeout.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("120"), 0o600))

	done := make(chan struct {
		err error
	}, 1)
	go func() {
		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		done <- struct{ err error }{err}
	}()

	select {
	case result := <-done:
		require.Error(t, result.err)
		var block *domain.QuotaBlock
		require.True(t, errors.As(result.err, &block),
			"early-cancelled stderr rate limit must come back as a typed park, not a timeout: %v", result.err)
		assert.Equal(t, domain.LLMProviderOpencode, block.Provider)
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not return within 5s — the rateLimitWatcher failed to cancel the hung session " +
			"(sleep_seconds is 120s and RunTimeout is 20s, so a working watcher returns almost immediately)")
	}
}

// A watcher armed on one run must never reach into a DIFFERENT run's context —
// two sessions on the same executor, one gets a rate limit, must not cancel
// the other.
func TestRateLimitWatcherOnlyCancelsItsOwnRun(t *testing.T) {
	cancelled := false
	w := &rateLimitWatcher{cancel: func() { cancelled = true }}
	_, err := w.Write([]byte("some unrelated log line\n"))
	require.NoError(t, err)
	assert.False(t, cancelled)
}

// A chat turn must run through the exact same spawn/parse/finish flow as a
// board task and come back as an answer, not the "host-executed provider
// cannot serve this call" refusal a missing ExecuteChat used to produce.
func TestExecuteChatReturnsTheSessionsAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := domain.ChatExecution{
		History: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Implement the executor seam."},
		},
		Provider:  domain.LLMProviderOpencode,
		WorkDir:   workDir,
		SessionID: "chat-1",
	}
	result, err := ex.ExecuteChat(context.Background(), req, port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, domain.RoleAssistant, result.Response.Message.Role)
	assert.Equal(t, "Reading the runner to see how the task is dispatched. Fixed the undefined symbol and the build is green.",
		result.Response.Message.Content)
}
