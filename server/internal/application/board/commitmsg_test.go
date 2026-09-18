package board

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type commitLLM struct {
	reply string
	err   error
	seen  domain.AgentRequest
}

func (c *commitLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	c.seen = req
	if c.err != nil {
		return domain.AgentResponse{}, c.err
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: c.reply}}, nil
}

func (c *commitLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return c.Chat(ctx, req)
}

func (c *commitLLM) Models(context.Context) ([]string, error) { return nil, nil }

func (c *commitLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

func TestWriteCommitMessageRewritesIntoEnglishAndStampsTheAgent(t *testing.T) {
	llm := &commitLLM{reply: "feat(web): add the android app link\n\nDrop the wishlist section from the landing page."}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey:   "PI-3",
		Title:     "Android uygulama linkini Acme web sitesine ekle ve wishlist kısmını kaldır",
		Summary:   "Linki ekledim, wishlist bölümünü sildim.",
		AgentName: "Frontend Developer",
	})

	assert.Equal(t,
		"feat(web): add the android app link\n\nDrop the wishlist section from the landing page.\n\nTask: PI-3\nCo-authored-by: Frontend Developer <frontend-developer@agents.tasktrooper.ai>\n",
		msg)

	require.Len(t, llm.seen.Messages, 2)
	assert.Contains(t, llm.seen.Messages[0].Content, "ALWAYS write in English")
	assert.Contains(t, llm.seen.Messages[1].Content, "Android uygulama linkini")
}

func TestWriteCommitMessageFallsBackToTheOriginalOnError(t *testing.T) {
	llm := &commitLLM{err: errors.New("provider down")}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey:   "PI-3",
		Title:     "Bir şeyi düzelt",
		Summary:   "Düzeltildi.",
		AgentName: "Backend Developer",
	})

	assert.Equal(t, "Bir şeyi düzelt\n\nDüzeltildi.\n\nTask: PI-3\nCo-authored-by: Backend Developer <backend-developer@agents.tasktrooper.ai>\n", msg)
}

func TestWriteCommitMessageWithoutLLMKeepsTheOriginal(t *testing.T) {
	msg := writeCommitMessage(context.Background(), nil, commitDetails{
		Title:     "Fix the deploy target lookup",
		AgentName: "Backend Developer",
	})

	assert.Equal(t, "Fix the deploy target lookup\n\nCo-authored-by: Backend Developer <backend-developer@agents.tasktrooper.ai>\n", msg)
}

func TestWriteCommitMessageKeepsThePrefix(t *testing.T) {
	llm := &commitLLM{reply: "feat(api): add the delete endpoint"}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		Title:  "delete endpoint",
		Prefix: "wip: ",
	})

	assert.True(t, strings.HasPrefix(msg, "wip: feat(api): add the delete endpoint"), msg)
}

func TestWriteCommitMessageKeepsThePrefixWithTaskKey(t *testing.T) {
	llm := &commitLLM{reply: "feat(api): add the delete endpoint"}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey: "T-9",
		Title:   "delete endpoint",
		Prefix:  "wip: ",
	})

	assert.True(t, strings.HasPrefix(msg, "wip: feat(api): add the delete endpoint"), msg)
	assert.Contains(t, msg, "Task: T-9")
}

func TestWriteCommitMessageCapsMaxTokens(t *testing.T) {
	llm := &commitLLM{reply: "feat(web): add the android app link"}

	writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey: "PI-3",
		Title:   "Add the android link",
		Summary: "Added it.",
	})

	assert.Equal(t, commitMessageMaxTokens, llm.seen.MaxTokens)
}

func TestCommitTrailersCombinesTaskAndAgent(t *testing.T) {
	assert.Equal(t,
		"\n\nTask: PI-3\nCo-authored-by: Frontend Developer <frontend-developer@agents.tasktrooper.ai>\n",
		commitTrailers("PI-3", "Frontend Developer"))
}

func TestCommitTrailersOmitsWhicheverIsMissing(t *testing.T) {
	assert.Equal(t, "\n\nTask: T-1\n", commitTrailers("T-1", ""))
	assert.Equal(t,
		"\n\nCo-authored-by: Backend Developer <backend-developer@agents.tasktrooper.ai>\n",
		commitTrailers("", "Backend Developer"))
	assert.Equal(t, "", commitTrailers("", ""))
}

// A commit-message policy that only recognises standard git trailers (which
// "Agent: <name>" was not) still has to accept this one, since Co-authored-by
// is exactly that — and it is not optional: it is the one place per-agent
// performance tracking can still tell which agent wrote a commit once it has
// landed.
func TestCommitTrailersUsesCoAuthoredByNotABespokeAgentLine(t *testing.T) {
	trailer := commitTrailers("", "QA Reviewer")
	assert.Contains(t, trailer, "Co-authored-by: QA Reviewer <qa-reviewer@agents.tasktrooper.ai>")
	assert.NotContains(t, trailer, "Agent:")
}

func TestWriteCommitMessageLeavesTheSubjectAsConventionalCommits(t *testing.T) {
	llm := &commitLLM{reply: "feat(web): add the link"}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey: "T-32",
		Title:   "add the link",
	})

	assert.True(t, strings.HasPrefix(msg, "feat(web): add the link"), msg)
	assert.Contains(t, msg, "Task: T-32")
}

func TestSanitizeCommitMessage(t *testing.T) {
	t.Run("strips a fenced reply", func(t *testing.T) {
		assert.Equal(t, "fix(auth): expire the token", sanitizeCommitMessage("```\nfix(auth): expire the token\n```"))
	})
	t.Run("truncates a runaway subject", func(t *testing.T) {
		long := "feat(board): " + strings.Repeat("x", 200)
		got := sanitizeCommitMessage(long)
		assert.Len(t, got, commitSubjectMaxChars)
	})
	t.Run("empty stays empty", func(t *testing.T) {
		assert.Equal(t, "", sanitizeCommitMessage("   \n  "))
	})
}

func TestCommitMessageRewriteDegradesWithALogWhenTheAgentCannotServeIt(t *testing.T) {
	refusal := domain.ErrHostExecutedProvider(domain.LLMProviderClaudeCode)
	if !errors.Is(refusal, domain.ErrHostExecutedUnservable) {
		t.Fatal("the refusal must be recognisable, or no caller can tell it apart from a flaky endpoint")
	}

	logs := captureBoardLogs(t)
	llm := &commitLLM{err: refusal}

	msg := writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey:   "PI-9",
		Title:     "Android uygulama linkini ekle",
		Summary:   "Linki header'a koydum.",
		AgentName: "frontend-developer",
		Writer:    modelRef{Provider: domain.LLMProviderClaudeCode, Model: "sonnet[1m]"},
	})

	if !strings.Contains(msg, "Android uygulama linkini ekle") {
		t.Fatalf("the commit lost the task's own title when the rewrite was skipped: %q", msg)
	}
	if !strings.Contains(msg, "PI-9") {
		t.Fatalf("the commit lost its task key: %q", msg)
	}

	line := logs.String()
	if !strings.Contains(line, `"permanent":true`) {
		t.Errorf("the skip must be flagged permanent — every commit by this agent will skip it.\n%s", line)
	}
	if !strings.Contains(line, "claude_code") {
		t.Errorf("the log must name the engine that could not serve it.\n%s", line)
	}
	if !strings.Contains(line, "commit message rewrite skipped") {
		t.Errorf("the log must say WHAT was skipped.\n%s", line)
	}
}

func TestCommitMessageRewriteDoesNotCallATransientFailurePermanent(t *testing.T) {
	logs := captureBoardLogs(t)
	llm := &commitLLM{err: errors.New("connection reset by peer")}

	writeCommitMessage(context.Background(), llm, commitDetails{
		TaskKey: "PI-9", Title: "t", Summary: "s", AgentName: "a",
		Writer: modelRef{Provider: domain.LLMProviderOpenAI, Model: "gpt-4o"},
	})

	if line := logs.String(); !strings.Contains(line, `"permanent":false`) {
		t.Errorf("a reset connection was reported as permanent.\n%s", line)
	}
}

func captureBoardLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = previous })
	return &buf
}
