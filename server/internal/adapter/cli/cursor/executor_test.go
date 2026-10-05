package cursor

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

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// newTestExecutor builds an executor pointed at testdata/fake-cursor-agent.sh
// and a workspace primed with the given fixture — see claudecode's identical
// helper for why the fixture is a file in the workspace rather than an
// environment variable.
func newTestExecutor(t *testing.T, cfg Config, fixtureFile string) (*Executor, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-cursor-agent.sh"))
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
		Model:    "sonnet-4-thinking",
		Provider: domain.LLMProviderCursorAgent,
		WorkDir:  workDir,
		TaskKey:  "tt-42",
	}
}

func indexOf(t *testing.T, values []string, want string) int {
	t.Helper()
	for i, v := range values {
		if v == want {
			return i
		}
	}
	t.Fatalf("%q not found in %v", want, values)
	return -1
}

// A finished session must come back as the same response shape the board
// runner reads, with the CLI's own text as the answer.
func TestExecuteReturnsTheSessionsAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, domain.RoleAssistant, resp.Message.Role)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", resp.Message.Content)
	assert.Equal(t, 1250, resp.Usage.PromptTokens, "inputTokens excludes the cache buckets, which are added back")
	assert.Equal(t, 120, resp.Usage.CompletionTokens)
	assert.Equal(t, 1370, resp.Usage.TotalTokens)
	assert.Equal(t, 300, resp.Usage.CacheReadTokens)
	assert.Equal(t, 50, resp.Usage.CacheWriteTokens)
}

// The invocation is the contract with the CLI: --model only travels when the
// agent actually chose one, and --force/--output-format are unconditional —
// without --force a print-mode run only proposes edits, and there is no human
// at this terminal to approve them.
func TestExecuteBuildsTheDocumentedInvocation(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "-p", argv[0], "print mode has to come first")
	assert.Equal(t, "You are the backend developer.\n\nImplement the executor seam.", argv[1],
		"the flattened history is the -p prompt, not stdin")
	assert.Contains(t, argv, "--force")
	assert.Contains(t, argv, "--output-format")
	assert.Contains(t, argv, "stream-json")
	assert.Equal(t, "sonnet-4-thinking", argv[indexOf(t, argv, "--model")+1])

	_, statErr := os.Stat(filepath.Join(workDir, "stdin.txt"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "cursor-agent takes its prompt in argv, never on stdin")
}

// An agent with no model chosen must not force one on cursor-agent: an empty
// --model would either error or silently pick something the operator did not
// ask for, where omitting the flag hands the choice to the CLI's own default.
func TestExecuteOmitsTheModelFlagWhenNoneIsChosen(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.Model = ""
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--model")
}

// The child must see a toolchain, not this pod's credentials.
func TestExecuteScrubsTheChildEnvironment(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_KEY", "gateway-hmac-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")

	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	env := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.NotContains(t, env, "INTERNAL_AUTH_KEY", "the gateway key must never reach a child process")
	assert.NotContains(t, env, "DATABASE_URL")
	assert.NotContains(t, env, "gateway-hmac-secret")
	assert.Contains(t, env, "PATH=", "a child with no PATH cannot run a single build command")
	assert.Contains(t, env, "HOME=")
}

// A result event with is_error true fails the run with the CLI's own status
// and text, not a generic message.
func TestExecuteFailsOnAnErrorResult(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "result_error.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error_during_execution")
	assert.Contains(t, err.Error(), "uncommitted changes")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "an unrelated failure must not be mistaken for a usage limit")
}

// No terminal "result" event means the session did not finish: killed,
// crashed, or stopped mid-stream. Reporting the last partial text as an
// answer would hand the board a half-run to commit.
func TestExecuteFailsWhenTheSessionNeverFinished(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"),
		[]byte(`{"type":"system","subtype":"init","session_id":"sess-dead"}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "stderr.txt"), []byte("killed: out of memory\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("137"), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "killed by signal killed", "137 is 128+SIGKILL, and the message has to name it")
	assert.Contains(t, err.Error(), "out of memory")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a crash is a failure, not a park")
}

// A deadline this executor imposed must actually stop a wedged session, and
// must not be reported as a quota park.
func TestRunTimeoutFailsTheRunAndDoesNotPark(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: 50 * time.Millisecond}, "success.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("30"), 0o600))

	start := time.Now()
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second, "the deadline has to actually kill the session")
	assert.Contains(t, err.Error(), "did not finish within")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a hang is not a spent subscription")
}

// A caller's own cancellation (stop button, pod drain) is reported as itself,
// not dressed up as this executor's deadline.
func TestCallerCancellationIsNotReportedAsATimeout(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{RunTimeout: time.Hour}, "success.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "sleep_seconds"), []byte("30"), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := ex.Execute(ctx, taskExecution(workDir))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "did not finish within")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// writeMCPConfigFile merges the tasktrooper server into .cursor/mcp.json for
// the run and restores the file exactly afterward — see mcp.go's own
// comment for why a merge rather than an overwrite.
func TestMCPConfigIsWrittenAndRestored(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{
		MCP: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "run-token"},
	}, "success.jsonl")

	mcpPath := filepath.Join(workDir, ".cursor", "mcp.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(mcpPath), 0o700))
	require.NoError(t, os.WriteFile(mcpPath, []byte(`{"mcpServers":{"other":{"url":"http://existing"}}}`), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	restored := readFile(t, mcpPath)
	assert.Contains(t, restored, "other", "a pre-existing developer server must survive the run")
	assert.NotContains(t, restored, "tasktrooper", "the run's own entry must be cleaned up once the session ends")
}

func TestMCPConfigIsAbsentWithNoProvider(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(workDir, ".cursor", "mcp.json"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "no MCPConfig means the run must not touch the developer's file at all")
}

func TestExecuteRefusesWithoutAWorkspace(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution("")
	_, err := ex.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no task workspace")
}

func TestNewRefusesAMissingBinary(t *testing.T) {
	_, err := New(Config{Binary: "definitely-not-a-real-cursor-agent-binary"})
	require.Error(t, err)
}

func TestSupportsOnlyCursorAgent(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")
	assert.True(t, ex.Supports(domain.LLMProviderCursorAgent))
	assert.False(t, ex.Supports(domain.LLMProviderClaudeCode))
	assert.False(t, ex.Supports(domain.LLMProviderOpencode))

	var nilEx *Executor
	assert.False(t, nilEx.Supports(domain.LLMProviderCursorAgent), "a nil executor must answer, not panic")
}
