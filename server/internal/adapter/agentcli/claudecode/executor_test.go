package claudecode

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

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// newTestExecutor builds an executor pointed at testdata/fake-claude.sh and a
// workspace primed with the given fixture.
//
// The fixture is placed IN the workspace rather than named through the
// environment because the executor scrubs the child's environment: a
// FAKE_CLAUDE_FIXTURE variable would never reach the script. Having to work
// around the scrub in the test is the same thing as proving it happens.
func newTestExecutor(t *testing.T, cfg Config, fixtureFile string) (*Executor, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
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

// readArgv reads the arguments the fake CLI recorded. NUL-separated, because
// one of them (the flattened system prompt) contains newlines.
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
			{Role: domain.RoleSystem, Content: "Project: TaskTrooper."},
			{Role: domain.RoleUser, Content: "Implement the executor seam."},
		},
		Model:     "opus",
		Provider:  domain.LLMProviderClaudeCode,
		WorkDir:   workDir,
		TaskKey:   "tt-42",
		TaskTitle: "Executor seam",
	}
}

// A finished session must come back as the same response shape the agent loop
// returns, with the run's spend on the context accumulator the board runner
// stamps onto the row, and the tool ledger the grounding gates read.
func TestExecuteReturnsTheSessionsAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	ctx, toolUsage := registry.ContextWithToolUsage(context.Background())
	ctx, tokens := usageapp.ContextWithTokenUsage(ctx)

	resp, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, domain.RoleAssistant, resp.Message.Role)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", resp.Message.Content)

	totals := tokens.Totals()
	assert.Equal(t, 1, totals.LLMCalls)
	assert.Equal(t, int64(13700), totals.PromptTokens)
	assert.Equal(t, int64(800), totals.CompletionTokens)
	assert.Equal(t, int64(12000), totals.CacheReadTokens)

	// The CLI's Read/Bash are counted under the TaskTrooper names the board's
	// grounding gates ask for; without the mapping an analiz run worked by the
	// CLI would be rejected for never having read the repository it read.
	assert.Equal(t, 1, toolUsage.Count("read_file"))
	assert.True(t, toolUsage.UsedAny(domain.CodeExplorationTools...))
	calls, failures := toolUsage.Totals()
	assert.Equal(t, 2, calls)
	assert.Equal(t, 1, failures, "the failed Bash call is a failure in the ledger, not a success")
}

// The invocation is the contract with the CLI. Each flag is here for a reason
// the comments in buildArgs give, and a silently dropped one changes what the
// session is allowed to do.
func TestExecuteBuildsTheDocumentedInvocation(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MaxTurns: 55}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "-p", argv[0], "print mode has to come first")
	assert.Contains(t, argv, "--output-format")
	assert.Contains(t, argv, "stream-json")
	assert.Contains(t, argv, "--verbose", "stream-json in -p mode is only per-event with --verbose")
	assert.Contains(t, argv, "--dangerously-skip-permissions", "nobody is at this terminal to approve an edit")
	assert.Contains(t, argv, "--max-turns")
	assert.Contains(t, argv, "55")
	assert.Contains(t, argv, "--model")
	assert.Contains(t, argv, "opus")
	assert.NotContains(t, argv, "--mcp-config", "with no MCPConfig the session runs on the CLI's native tools")
	assert.NotContains(t, argv, "--resume", "a fresh run must not resume anything")

	// System blocks are flattened into one --append-system-prompt, in order;
	// the user content is the positional prompt.
	system := argv[indexOf(t, argv, "--append-system-prompt")+1]
	assert.Equal(t, "You are the backend developer.\n\nProject: TaskTrooper.", system)
	assert.Equal(t, "Implement the executor seam.", argv[1], "the prompt is the positional argument after -p")
}

// The child must see a toolchain, not this pod's credentials. Everything about
// the scrub lives in platform/childenv; what is asserted here is that this
// executor uses it and that the Claude-specific passthrough does not smuggle
// anything back in.
func TestExecuteScrubsTheChildEnvironment(t *testing.T) {
	t.Setenv("INTERNAL_AUTH_KEY", "gateway-hmac-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-tenant-key")
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/agent/.claude")

	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")
	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	env := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.NotContains(t, env, "INTERNAL_AUTH_KEY", "the gateway key must never reach a child process")
	assert.NotContains(t, env, "DATABASE_URL")
	assert.NotContains(t, env, "gateway-hmac-secret")
	assert.NotContains(t, env, "ANTHROPIC_API_KEY",
		"the configured API key is not Claude Code's credential, and forwarding it would move the session off the subscription")
	assert.Contains(t, env, "CLAUDE_CONFIG_DIR=/home/agent/.claude", "the CLI's own config location has to survive")
	assert.Contains(t, env, "PATH=", "a child with no PATH cannot run a single build command")
	assert.Contains(t, env, "HOME=", "the CLI reads its subscription credentials out of HOME")
}

// A usage limit is a park, not a failure: it comes back as a typed error the
// board runner recognises, carrying the reset time and the session to resume.
func TestExecuteReturnsATypedQuotaBlock(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "usage_limit.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)

	var block *domain.QuotaBlock
	require.True(t, errors.As(err, &block), "the runner keys off the type, not the message: %v", err)
	// The fixture's epoch is deliberately far in the future: a fixed one in the
	// past would quietly stop testing the parse and start testing the fallback
	// window as the clock moved past it.
	assert.Equal(t, int64(4102444800), block.ResumeAt.Unix(), "the epoch on the CLI's message is the reset time")
	assert.Equal(t, "sess-limit-9", block.CLISessionID, "without the session the resume is a restart")
	assert.Contains(t, block.Detail, "usage limit reached")
}

// The limit is also reported on stderr, in builds that give up before writing a
// result event. Same park, and — with no epoch to read — the default window.
func TestExecuteDetectsAUsageLimitOnStderr(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"),
		[]byte(`{"type":"system","subtype":"init","session_id":"sess-stderr"}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "stderr.txt"),
		[]byte("Claude AI usage limit reached\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("1"), 0o600))

	before := time.Now()
	_, err := ex.Execute(context.Background(), taskExecution(workDir))

	var block *domain.QuotaBlock
	require.True(t, errors.As(err, &block), "a limit on stderr parks the task too: %v", err)
	assert.Equal(t, "sess-stderr", block.CLISessionID)
	assert.WithinDuration(t, before.Add(domain.DefaultQuotaParkWindow), block.ResumeAt, time.Minute)
}

// Spending the turn budget is the CLI's version of the loop's giveUp: the work
// that got done is real and the gates downstream judge it. Failing here would
// throw away a mostly-finished task.
func TestExecuteTreatsTheTurnBudgetAsAnAnswer(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MaxTurns: 100}, "max_turns.jsonl")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.Contains(t, resp.Message.Content, "Half of the refactor is done")
	assert.Contains(t, resp.Message.Content, "100-turn budget", "the summary has to say why the answer stops where it does")
}

// A stream with no terminal event is a killed or crashed session. It must not
// be reported as an answer — the board would commit and hand off half a task.
func TestExecuteFailsWhenTheSessionNeverFinished(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"),
		[]byte(`{"type":"system","subtype":"init","session_id":"sess-dead"}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "stderr.txt"), []byte("killed: out of memory\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("137"), 0o600))

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a result")
	assert.Contains(t, err.Error(), "out of memory", "the stderr tail is what says why")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block), "a crash is a failure, not a park")
}

// A resumed run continues the session it parked in: the id it was given, a
// short continue prompt, and none of the original context — which the session
// already holds and would read as a second, competing instruction.
func TestExecuteResumesTheParkedSession(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-abc123"
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, []string{"-p", "--resume", "sess-abc123"}, argv[:3])
	assert.NotContains(t, argv, "--append-system-prompt", "the resumed session already has the persona and the task")
	assert.Contains(t, argv[3], "Continue tt-42 Executor seam")
	assert.NotContains(t, argv[3], "You are the backend developer.")
}

// A board task's resumed CLI session can be gone the same way a chat's can —
// pruned on the host's own schedule, lost with a reinstall, or run on a
// different machine during the quota park window. It must not turn into a
// generic "failed" run: that is exactly the run the reconciler gives up on
// after three of them, leaving the card parked forever even after the limit
// resets.
func TestExecuteFallsBackToAFreshSessionWhenTheResumeIsRefused(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	writeFixture(t, workDir, "chat_resume_missing.jsonl", "fixture.1.jsonl")
	writeFixture(t, workDir, "chat_resume_missing_stderr.txt", "stderr.1.txt")
	writeFixture(t, workDir, "success.jsonl", "fixture.2.jsonl")

	req := taskExecution(workDir)
	req.ResumeSessionID = "sess-gone-9"

	resp, err := ex.Execute(context.Background(), req)
	require.NoError(t, err, "a forgotten CLI session must not fail the run")

	assert.Equal(t, "2", readFile(t, filepath.Join(workDir, "calls.txt")), "it retried exactly once")

	first := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.1.txt")), "\x00"), "\x00")
	assert.Contains(t, first, "--resume", "the first attempt did try to continue the session")

	second := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.2.txt")), "\x00"), "\x00")
	assert.NotContains(t, second, "--resume", "the retry starts a new conversation")
	assert.Contains(t, second, "--append-system-prompt", "which means it must carry the task's full history again")
	assert.Contains(t, second[indexOf(t, second, "--append-system-prompt")+1], "You are the backend developer.")

	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", resp.Message.Content)
}

// Without the binary there is no executor, which is what makes the board
// runner's failure a clear sentence about a missing install instead of an exec
// error mid-run.
func TestNewRefusesAMissingBinary(t *testing.T) {
	_, err := New(Config{Binary: "claude-that-is-not-installed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found on PATH")
}

// The workspace is the only tree this run may touch. A default would put the
// session in the shared project root, on whatever branch it happens to be on.
func TestExecuteRefusesToRunWithoutAWorkspace(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution("")
	_, err := ex.Execute(context.Background(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no task workspace")
}

// Supports is what the runner asks before it picks a path. It must answer for
// exactly one provider, and answer safely when there is no executor at all.
func TestSupportsOnlyClaudeCode(t *testing.T) {
	ex, _ := newTestExecutor(t, Config{}, "success.jsonl")

	assert.True(t, ex.Supports(domain.LLMProviderClaudeCode))
	assert.False(t, ex.Supports(domain.LLMProviderAnthropic))
	assert.False(t, ex.Supports(domain.LLMProviderOpenAI))

	var missing *Executor
	assert.False(t, missing.Supports(domain.LLMProviderClaudeCode), "a nil executor supports nothing")
}

// A fixed MCPConfig writes a per-run config file and points the session at it;
// unset, the flag is absent entirely.
func TestMCPConfigIsPassedWhenSet(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MCP: MCPConfig{URL: "http://127.0.0.1:8080/mcp", Token: "run-token"}}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	path := argv[indexOf(t, argv, "--mcp-config")+1]
	assert.NotEmpty(t, path)
	assert.NotContains(t, path, workDir, "a bearer token must not be written into a tree the agent commits from")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "the per-run config file is removed when the run ends")
}

// recordingMCPProvider is a stand-in for platform/runtime's token minter. It
// counts both halves, because the pair is the whole point: a token that is
// minted and not released outlives the run it authenticated.
type recordingMCPProvider struct {
	cfg      MCPConfig
	err      error
	minted   int
	released int
	// run is the last request this provider was asked about — the policy it was
	// told to serve and the caller's label. Captured because a chat turn must be
	// credentialled from ITS policy under ITS own name, and the only way to see
	// that is what arrives here.
	run MCPRun
}

func (p *recordingMCPProvider) ForRun(_ context.Context, run MCPRun) (MCPConfig, func(), error) {
	p.run = run
	if p.err != nil {
		return MCPConfig{}, nil, p.err
	}
	p.minted++
	return p.cfg, func() { p.released++ }, nil
}

// The per-run credential is the whole mechanism: the session must actually
// RECEIVE the endpoint and the token — in a file, never on the command line —
// and the token must be gone the moment the run ends.
func TestMCPProviderMintsAndRevokesPerRun(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "per-run-secret"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	// What the session was handed, copied out by the fake CLI while it was
	// still running — the executor deletes the original on the way out.
	handed := readFile(t, filepath.Join(workDir, "mcp-config.json"))
	assert.Contains(t, handed, "http://127.0.0.1:9110/mcp")
	assert.Contains(t, handed, "Bearer per-run-secret")
	assert.Contains(t, handed, `"type":"http"`)

	argv := readArgv(t, workDir)
	assert.NotContains(t, argv, "per-run-secret", "a command line is world-readable in ps")

	assert.Equal(t, 1, provider.minted)
	assert.Equal(t, 1, provider.released, "the token dies with the run")
}

// Every exit path releases, not just the happy one. A quota park returns a
// typed error with the session still resumable later — and a token left live
// across that wait would authenticate a session nobody is running.
func TestMCPTokenIsReleasedOnEveryExitPath(t *testing.T) {
	t.Run("quota park", func(t *testing.T) {
		provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "parked"}}
		ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "usage_limit.jsonl")

		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		var block *domain.QuotaBlock
		require.True(t, errors.As(err, &block))
		assert.Equal(t, 1, provider.released)
	})

	t.Run("crashed session", func(t *testing.T) {
		provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "crashed"}}
		ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "")
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "exit_code"), []byte("1"), 0o600))

		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		require.Error(t, err)
		assert.Equal(t, 1, provider.released)
	})

	t.Run("minting failed", func(t *testing.T) {
		provider := &recordingMCPProvider{err: errors.New("no entropy")}
		ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

		_, err := ex.Execute(context.Background(), taskExecution(workDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no entropy")
		assert.Equal(t, 0, provider.released, "nothing was minted, so there is nothing to release")
		_, statErr := os.Stat(filepath.Join(workDir, "argv.txt"))
		assert.True(t, os.IsNotExist(statErr), "a run without its credential must not start the CLI at all")
	})
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

// The limit is recognised from TEXT, and text is something a SUCCESSFUL run can
// legitimately contain — a resumed session reporting why the last attempt
// stopped writes that exact sentence. Parking on it would throw the finished
// work away and park the card on its own summary, which the resumed run would
// then write again: a card that cycles forever with every step looking correct.
func TestSuccessfulRunThatMentionsAUsageLimitIsNotParked(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success_mentions_limit.jsonl")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err, "a finished session's text is never searched for a usage limit")

	assert.Contains(t, resp.Message.Content, "Finished the migration")

	var block *domain.QuotaBlock
	assert.False(t, errors.As(err, &block))
}

// The gate itself, at every outcome: only a session that ended badly may be
// read as a quota park.
func TestOnlyAFailedSessionIsSearchedForAUsageLimit(t *testing.T) {
	tests := []struct {
		name       string
		s          session
		wantFailed bool
	}{
		{name: "clean success", s: session{out: outcome{SawResult: true, Subtype: "success"}}},
		{name: "turn budget spent", s: session{out: outcome{SawResult: true, Subtype: "error_max_turns"}}},
		{name: "result event flagged an error", s: session{out: outcome{SawResult: true, Subtype: "success", IsError: true}}, wantFailed: true},
		{name: "an execution error subtype", s: session{out: outcome{SawResult: true, Subtype: "error_during_execution"}}, wantFailed: true},
		{name: "no terminal event", s: session{out: outcome{}}, wantFailed: true},
		{name: "non-zero exit", s: session{out: outcome{SawResult: true, Subtype: "success"}, waitErr: errors.New("exit 1")}, wantFailed: true},
		{name: "unreadable stream", s: session{out: outcome{SawResult: true, Subtype: "success"}, parseErr: errors.New("token too long")}, wantFailed: true},
		{name: "our own deadline", s: session{out: outcome{SawResult: true, Subtype: "success"}, timedOut: true}, wantFailed: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantFailed, tc.s.failed())
		})
	}
}

// A wedged CLI is the one failure nothing else in the system can see: the run's
// heartbeat keeps the row fresh, so the stale-run reconciler never fires, and
// the session holds its board run for as long as it
// lives. The deadline is the only thing that ends it — and it must end it as a
// FAILURE, because parking would wait out the window and then hand the same
// hang another hour.
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

// Tools this server serves the session over MCP are executed by the registry,
// which records them exactly as it does for a loop run. Counting them here too
// would double every board tool in the ledger the grounding gates read, and
// duplicate the pair in the transcript.
func TestOwnMCPToolsAreNotCountedTwice(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "mcp_tools.jsonl")

	ctx, toolUsage := registry.ContextWithToolUsage(context.Background())
	_, err := ex.Execute(ctx, taskExecution(workDir))
	require.NoError(t, err)

	assert.Equal(t, 1, toolUsage.Count("read_file"), "the CLI's own tools are still counted here")
	assert.Zero(t, toolUsage.Count("mcp__tasktrooper__move_board_task"),
		"a tasktrooper tool is counted where it is executed — in the registry, once")
	assert.Equal(t, 1, toolUsage.Count("mcp__othervendor__lookup"),
		"another server's tools pass through nothing else, so this side is the only place they can be counted")

	calls, _ := toolUsage.Totals()
	assert.Equal(t, 2, calls)
}

// A task's credential request carries the run's OWN policy and says the run
// cannot proceed without tools.
//
// Both halves are load-bearing and both were live suspects for a QA run that
// reported TaskTrooper's tools missing. The policy is what the endpoint serves
// tools/list from, so a zero one here would hand the session a surface nobody
// scoped; RequiresTools is what stops a board run from starting at all when
// there is no endpoint to serve it, instead of running to a plausible-looking
// finish with no way to tick a criterion.
func TestTaskCredentialCarriesThePolicyAndDemandsTools(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "t"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	req := taskExecution(workDir)
	req.Policy = domain.ToolPolicy{AllowTools: []string{"list_acceptance_criteria", "review_criterion"}}
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, req.Policy, provider.run.Policy, "the endpoint serves this run's own policy")
	assert.False(t, provider.run.Policy.IsZero(), "a zero policy would scope the session to nothing in particular")
	assert.Equal(t, "tt-42", provider.run.Label)
	assert.True(t, provider.run.RequiresTools, "a board run cannot finish its workflow without them")
}

// A chat turn asks for the same credential but does NOT demand it: a
// conversation with fewer tools is still a conversation.
func TestChatCredentialDoesNotDemandTools(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "t"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	_, err := ex.ExecuteChat(context.Background(), domain.ChatExecution{
		History:   []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		Prompt:    "hi",
		WorkDir:   workDir,
		SessionID: "chat-1",
		Policy:    domain.ToolPolicy{AllowTools: []string{"list_board_tasks"}},
	}, port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, "chat-1", provider.run.Label)
	assert.False(t, provider.run.RequiresTools)
}

// Whether the endpoint serves load_skill is decided by the CALLER, not by this
// executor, and the request is the only place that knowledge can come from.
//
// Both directions are pinned. A materialised board run says so, and the
// endpoint drops the tool that would fetch a skill body it can already read off
// the workspace. A run nobody materialised for — the agent router's CLI runs,
// which execute in a scratch directory application/agentfs never touched — comes
// through this same method and must keep it: hardcoding true here would leave
// those runs with no way to reach a skill at all, and nothing would report it.
func TestTaskCredentialCarriesWhetherTheSkillsAreOnDisk(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "t"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	materialised := taskExecution(workDir)
	materialised.SkillsOnDisk = true
	_, err := ex.Execute(context.Background(), materialised)
	require.NoError(t, err)
	assert.True(t, provider.run.SkillsOnDisk, "the board runner wrote this run's skills into its workspace")

	_, err = ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	assert.False(t, provider.run.SkillsOnDisk, "a run nothing materialised for still needs the tool")
}

// A chat turn is never materialised: application/agentfs runs in the board
// runner and only for a task, so the workspace a chat opens in holds no skill
// files. Claiming otherwise would remove the turn's only access to the agent's
// skills, and the conversation would go on looking entirely normal without them.
func TestChatCredentialNeverClaimsItsSkillsAreOnDisk(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9110/mcp", Token: "t"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "success.jsonl")

	_, err := ex.ExecuteChat(context.Background(), domain.ChatExecution{
		History:   []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		Prompt:    "hi",
		WorkDir:   workDir,
		SessionID: "chat-1",
	}, port.ChatStream{})
	require.NoError(t, err)

	assert.False(t, provider.run.SkillsOnDisk)
}

// A per-agent ceiling is the reason these fields exist: a session's cost grows
// with the SQUARE of its turns, so one global number priced for the most
// open-ended agent was charged to every reviewer and verifier too.
func TestExecuteLetsTheAgentOverrideTheTurnCeiling(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MaxTurns: 100}, "success.jsonl")

	req := taskExecution(workDir)
	req.MaxTurns = 40
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "40", argv[indexOf(t, argv, "--max-turns")+1])
	assert.NotContains(t, argv, "100", "the executor-wide default must not survive an agent's own ceiling")
}

// 0 is how an agent says it has no opinion, matching the convention Model uses:
// the executor's configured value stands.
func TestExecuteKeepsTheExecutorCeilingWhenTheAgentHasNone(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{MaxTurns: 55}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "55", argv[indexOf(t, argv, "--max-turns")+1])
}

func TestExecutePassesTheAgentsEffortLevel(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.Effort = "low"
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Equal(t, "low", argv[indexOf(t, argv, "--effort")+1])
}

func TestExecuteOmitsEffortWhenTheAgentHasNone(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--effort",
		"an agent with no level must leave the CLI on its own default")
}

// The package comment used to state that a claude_code agent had the CLI's
// file-and-shell surface whatever its policy said, because the policy governed
// only the MCP half. --tools is the CLI's own mechanism for the other half, so
// the policy now reaches both.
func TestExecuteNarrowsTheBuiltinToolsToThePolicy(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	req := taskExecution(workDir)
	req.Policy = domain.ToolPolicy{AllowTools: []string{"read_file", "grep_code"}}
	_, err := ex.Execute(context.Background(), req)
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	// Split rather than substring-match the joined value: "TodoWrite" contains
	// "Write", and a substring assertion would pass on a session that really had
	// been handed the write tool.
	tools := strings.Split(argv[indexOf(t, argv, "--tools")+1], ",")
	assert.Subset(t, tools, []string{"Read", "Grep"})
	assert.NotContains(t, tools, "Bash", "this policy grants no terminal")
	assert.NotContains(t, tools, "Write", "this policy grants no writes")
	assert.NotContains(t, tools, "Edit", "this policy grants no writes")
}

// An unrestricted policy must not be narrowed on the way through: it reaches
// the CLI as no flag at all, which is the CLI's own default of every built-in.
func TestExecuteOmitsToolsForAnUnrestrictedPolicy(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)

	assert.NotContains(t, readArgv(t, workDir), "--tools")
}
