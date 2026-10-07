package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// writeFixture places one of the per-call fixture files the fake CLI reads. See
// testdata/fake-claude.sh: a chat turn can spawn the CLI twice, so the fixtures
// are numbered by call.
func writeFixture(t *testing.T, workDir, name, asName string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, asName), body, 0o600))
}

func chatExecution(workDir string) domain.ChatExecution {
	return domain.ChatExecution{
		History: []domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Where are the HTTP routes registered?"},
		},
		Prompt:    "Where are the HTTP routes registered?",
		Model:     "opus",
		Provider:  domain.LLMProviderClaudeCode,
		WorkDir:   workDir,
		SessionID: "11111111-2222-3333-4444-555555555555",
	}
}

// recorder captures what a chat turn streamed, in order, so a test can assert
// on the shape the browser would have seen rather than only on the final text.
type recorder struct {
	text   strings.Builder
	breaks int
	// events is the interleaving: "t:<text>" and "break". The ORDER is the
	// contract — a break in the wrong place is what makes a client show
	// reasoning as the answer.
	events []string
}

func (r *recorder) stream() port.ChatStream {
	return port.ChatStream{
		OnText: func(s string) {
			r.text.WriteString(s)
			r.events = append(r.events, "t:"+s)
		},
		OnSegmentBreak: func() {
			r.breaks++
			r.events = append(r.events, "break")
		},
	}
}

// The first turn of a chat has no session to resume, so it goes out exactly like
// a board run: the system blocks flattened into the system prompt file and the
// conversation on stdin, with no --resume anywhere.
func TestExecuteChatFirstTurnSendsTheFlattenedHistory(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_reply.jsonl")

	var rec recorder
	result, err := ex.ExecuteChat(context.Background(), chatExecution(workDir), rec.stream())
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.NotContains(t, argv, "--resume", "a first turn has no session to continue")
	assert.Contains(t, argv, "--append-system-prompt-file")
	assert.Contains(t, readSystemPrompt(t, workDir), "You are the backend developer.")
	assert.Contains(t, readPrompt(t, workDir), "Where are the HTTP routes registered?")

	assert.Equal(t, "cli-chat-1", result.CLISessionID, "the caller needs the id to continue this chat")
	assert.False(t, result.Resumed)
	assert.Equal(t, "Routes are registered in handler.go.", result.Response.Message.Content)
}

// The second turn is the reason this seam exists at all. It must resume the SAME
// CLI session and send only what the user just typed: re-flattening a growing
// transcript pays for the whole conversation on every message, and hands the
// model its own remembered context back as if it were a new instruction.
func TestExecuteChatSecondTurnResumesAndSendsOnlyTheNewMessage(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_resumed.jsonl")

	req := chatExecution(workDir)
	req.ResumeSessionID = "cli-chat-1"
	req.Prompt = "Is the models endpoint in there too?"
	req.History = append(req.History,
		domain.Message{Role: domain.RoleAssistant, Content: "Routes are registered in handler.go."},
		domain.Message{Role: domain.RoleUser, Content: "Is the models endpoint in there too?"},
	)

	var rec recorder
	result, err := ex.ExecuteChat(context.Background(), req, rec.stream())
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.Contains(t, argv, "--resume")
	assert.Contains(t, argv, "cli-chat-1")
	prompt := readPrompt(t, workDir)
	assert.Contains(t, prompt, "Is the models endpoint in there too?")
	assert.NotContains(t, argv, "--append-system-prompt-file",
		"the live session already holds the persona; replaying it costs tokens and reads as a new instruction")
	assert.NotContains(t, prompt, "You are the backend developer.")
	assert.NotContains(t, prompt, "Routes are registered in handler.go.",
		"the session remembers its own answers; sending them back is what makes a resumed agent redo work")

	assert.True(t, result.Resumed)
	assert.Equal(t, "cli-chat-1", result.CLISessionID)
	assert.Equal(t, "Yes — it is the Models handler, same file.", result.Response.Message.Content)
}

// A resume the CLI cannot honour — the session was pruned, the host was
// reinstalled, the last turn ran elsewhere — must not end the conversation. The
// transcript is in Postgres, so the turn starts a fresh session from it.
func TestExecuteChatFallsBackToAFreshSessionWhenTheResumeIsRefused(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "")
	writeFixture(t, workDir, "chat_resume_missing.jsonl", "fixture.1.jsonl")
	writeFixture(t, workDir, "chat_resume_missing_stderr.txt", "stderr.1.txt")
	writeFixture(t, workDir, "chat_reply.jsonl", "fixture.2.jsonl")

	req := chatExecution(workDir)
	req.ResumeSessionID = "cli-gone-9"

	var rec recorder
	result, err := ex.ExecuteChat(context.Background(), req, rec.stream())
	require.NoError(t, err, "a forgotten CLI session is not a failed chat")

	assert.Equal(t, "2", readFile(t, filepath.Join(workDir, "calls.txt")), "it retried exactly once")

	first := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.1.txt")), "\x00"), "\x00")
	assert.Contains(t, first, "--resume", "the first attempt did try to continue the session")

	second := strings.Split(strings.TrimRight(readFile(t, filepath.Join(workDir, "argv.2.txt")), "\x00"), "\x00")
	assert.NotContains(t, second, "--resume", "the retry starts a new conversation")
	assert.Contains(t, second, "--append-system-prompt-file", "which means it must carry the persona again")
	assert.Contains(t, readFile(t, filepath.Join(workDir, "system-prompt.2.txt")), "You are the backend developer.")

	assert.False(t, result.Resumed, "the caller is told this was not a continuation")
	assert.Equal(t, "cli-chat-1", result.CLISessionID, "and gets the NEW session id to continue from")
	assert.Equal(t, "Routes are registered in handler.go.", result.Response.Message.Content)
}

// The web transcript is fed by the same callback the agent loop's streamed turns
// use, so a CLI turn has to arrive the same way: text as it is produced, with a
// boundary closing the narration that led to a tool call. Without the boundary
// the browser renders "Let me look at the router first." as the reply and then
// swaps it for the shorter text that is actually persisted.
func TestExecuteChatStreamsTextAndClosesReasoningBeforeAToolCall(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_reply.jsonl")

	var rec recorder
	result, err := ex.ExecuteChat(context.Background(), chatExecution(workDir), rec.stream())
	require.NoError(t, err)

	assert.Equal(t, []string{
		"t:Let me look at the router first.",
		"break",
		"t:Routes are registered in handler.go.",
	}, rec.events)

	assert.Equal(t, 1, rec.breaks)
	// What survives the last break is what gets persisted. If these two ever
	// disagree the user watches their answer change after it finishes.
	assert.Equal(t, "Routes are registered in handler.go.", result.Response.Message.Content)
}

// A board run must be completely unaffected by the chat path existing: no
// listener, no wrapper, no segment breaks.
func TestExecuteStillStreamsNothing(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "success.jsonl")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	assert.Equal(t, "Added the executor seam and wired it in. Build and vet are green.", resp.Message.Content)
}

// The usage limit reaches a chat as the typed error, not as a generic failure —
// the caller cannot turn it into an actionable sentence otherwise. The live
// session id comes back with it, because a chat that resumes when the quota
// returns keeps everything said so far.
func TestExecuteChatReturnsTheQuotaBlockWithItsSession(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "usage_limit.jsonl")

	var rec recorder
	result, err := ex.ExecuteChat(context.Background(), chatExecution(workDir), rec.stream())
	require.Error(t, err)

	block, ok := domain.QuotaBlockOf(err)
	require.True(t, ok, "a spent subscription must stay recognisable, not become a string")
	assert.Equal(t, "sess-limit-9", block.CLISessionID)
	assert.Equal(t, "sess-limit-9", result.CLISessionID,
		"the parked session is still worth resuming when the window reopens")
}

// A chat with nowhere to run is a setup problem, not something to paper over
// with the server's own working directory — which on a desktop install is the
// server's source tree.
func TestExecuteChatRefusesWithoutAWorkspace(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_reply.jsonl")

	req := chatExecution(workDir)
	req.WorkDir = "  "

	_, err := ex.ExecuteChat(context.Background(), req, port.ChatStream{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace")
}

// An empty model means "omit --model", which hands the choice to the operator's
// own CLI configuration. It is the first option the picker offers, so it has to
// keep meaning that.
func TestExecuteChatOmitsTheModelFlagWhenNoneIsChosen(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_reply.jsonl")

	req := chatExecution(workDir)
	req.Model = ""

	_, err := ex.ExecuteChat(context.Background(), req, port.ChatStream{})
	require.NoError(t, err)
	assert.NotContains(t, readArgv(t, workDir), "--model")
}

// A chat turn is credentialled exactly like a board run: its own token, minted
// for the turn and revoked at the end of it. A thread that stays open for days
// must not leave a live credential for the board tools lying around between
// messages.
func TestExecuteChatMintsAndRevokesItsOwnMCPToken(t *testing.T) {
	provider := &recordingMCPProvider{cfg: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "chat-token"}}
	ex, workDir := newTestExecutor(t, Config{MCPProvider: provider}, "chat_reply.jsonl")

	req := chatExecution(workDir)
	req.Policy = domain.ToolPolicy{AllowTools: []string{"move_board_task"}}

	_, err := ex.ExecuteChat(context.Background(), req, port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, 1, provider.minted)
	assert.Equal(t, 1, provider.released, "the credential does not outlive the turn")
	assert.Equal(t, []string{"move_board_task"}, provider.run.Policy.AllowTools,
		"the endpoint serves the chat's own policy, the same way it serves a run's")
	assert.Equal(t, req.SessionID, provider.run.Label)

	// The config file the session was handed is deleted with the turn; the fake
	// CLI copied it while it was still running, which is the only way to see it.
	assert.Contains(t, readFile(t, filepath.Join(workDir, "mcp-config.json")), "chat-token")
}

// A chat passes no --tools, so every built-in is on; the CLI's own question
// card has to be withheld explicitly or a chat question asked through it is lost.
func TestExecuteChatWithholdsTheCLIsOwnQuestionCard(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{}, "chat_reply.jsonl")

	var rec recorder
	_, err := ex.ExecuteChat(context.Background(), chatExecution(workDir), rec.stream())
	require.NoError(t, err)

	argv := readArgv(t, workDir)
	assert.NotContains(t, argv, "--tools")
	assert.Contains(t, strings.Split(argv[indexOf(t, argv, "--disallowedTools")+1], ","), "AskUserQuestion")
}
