package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type fakeChatExecutor struct {
	supports bool
	got      domain.ChatExecution
	calls    int
	result   domain.ChatResult
	err      error

	stream func(port.ChatStream)
	// asks is what the CLI's ask_user call, served over MCP, hands back.
	asks *domain.ClarificationRequest
}

func (f *fakeChatExecutor) Supports(p domain.LLMProviderType) bool {
	return f.supports && p == domain.LLMProviderClaudeCode
}

func (f *fakeChatExecutor) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	f.calls++
	f.got = req
	if f.asks != nil {
		domain.ClarificationSinkFrom(ctx).Record(*f.asks)
	}
	if f.stream != nil {
		f.stream(out)
	}
	return f.result, f.err
}

type recordingSessionStore struct {
	port.SessionStore
	cliSessionID string
	writes       int
	err          error
}

func (s *recordingSessionStore) UpdateCLISessionID(_ context.Context, _ uuid.UUID, id string) error {
	s.writes++
	if s.err != nil {
		return s.err
	}
	s.cliSessionID = id
	return nil
}

func chatSession() domain.Session {
	return domain.Session{ID: uuid.New(), WorkspaceDir: "/tmp/repo"}
}

func runTurn(
	t *testing.T,
	svc *session.Service,
	sess domain.Session,
	out port.ChatStream,
) (domain.AgentResponse, error) {
	t.Helper()
	return svc.RunHostExecutedTurnForTest(
		context.Background(), sess,
		domain.LLMProviderClaudeCode, "opus", domain.ToolPolicy{AllowTools: []string{"read_file"}},
		"/tmp/repo",
		[]domain.Message{
			{Role: domain.RoleSystem, Content: "You are the backend developer."},
			{Role: domain.RoleUser, Content: "Where are the routes?"},
		},
		"Where are the routes?", "en", out,
	)
}

func TestChatTurnGoesToTheExecutorNotTheHTTPClient(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result: domain.ChatResult{
			Response:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "In handler.go."}},
			CLISessionID: "cli-1",
		},
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	resp, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, 1, executor.calls)
	assert.Equal(t, "In handler.go.", resp.Message.Content)
	assert.Equal(t, "/tmp/repo", executor.got.WorkDir)
	assert.Equal(t, "opus", executor.got.Model)
	assert.Equal(t, domain.LLMProviderClaudeCode, executor.got.Provider)
	assert.Equal(t, []string{"read_file"}, executor.got.Policy.AllowTools,
		"the CLI session is served the same tools the loop would have enforced")
	assert.Empty(t, executor.got.ResumeSessionID, "a fresh chat has nothing to resume")
}

func TestChatRecordsTheCLISessionAndResumesItOnTheNextTurn(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result: domain.ChatResult{
			Response:     domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "In handler.go."}},
			CLISessionID: "cli-77",
		},
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	sess := chatSession()
	_, err := runTurn(t, svc, sess, port.ChatStream{})
	require.NoError(t, err)
	assert.Equal(t, "cli-77", store.cliSessionID, "the id is the conversation's continuity, and only the caller can persist it")

	sess.CLISessionID = store.cliSessionID
	_, err = runTurn(t, svc, sess, port.ChatStream{})
	require.NoError(t, err)

	assert.Equal(t, 2, executor.calls)
	assert.Equal(t, "cli-77", executor.got.ResumeSessionID)
	assert.Equal(t, "Where are the routes?", executor.got.Prompt,
		"a resumed session is sent only what is new; it already holds the rest")
	assert.Equal(t, 1, store.writes, "an unchanged id is not rewritten on every turn")
}

func TestChatRecordsTheCLISessionEvenWhenTheTurnFailed(t *testing.T) {
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{CLISessionID: "cli-parked"},
		err:      errors.New("claude code ended without a result"),
	}
	store := &recordingSessionStore{}
	svc := session.NewHostExecutedServiceForTest(store, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.Error(t, err)
	assert.Equal(t, "cli-parked", store.cliSessionID)
}

func TestQuotaBlockBecomesAnActionableChatMessage(t *testing.T) {
	resumeAt := time.Now().Add(90 * time.Minute)
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{CLISessionID: "cli-limited"},
		err:      &domain.QuotaBlock{ResumeAt: resumeAt, CLISessionID: "cli-limited", Detail: "Claude AI usage limit reached"},
	}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
	require.Error(t, err)

	block, ok := domain.QuotaBlockOf(err)
	require.True(t, ok)
	assert.Equal(t, "cli-limited", block.CLISessionID)

	assert.Contains(t, err.Error(), "usage limit")
	assert.Contains(t, err.Error(), resumeAt.Local().Format("15:04"),
		"a limit with no time attached is not actionable")
	assert.NotContains(t, err.Error(), "unsupported protocol scheme")
}

func TestQuotaMessageFollowsTheTenantLanguage(t *testing.T) {
	block := &domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour)}

	tr := domain.NewQuotaNotice(block, "tr").Error()
	assert.Contains(t, tr, "Kullanım limiti doldu")
	assert.Contains(t, tr, "civarında yenilenecek")

	en := domain.NewQuotaNotice(block, "en").Error()
	assert.Contains(t, en, "usage limit is spent")
}

func TestQuotaNoticeIsWrittenToTheTranscriptAsAWarning(t *testing.T) {
	store := &capturingStore{}
	notice := domain.NewQuotaNotice(&domain.QuotaBlock{ResumeAt: time.Now().Add(time.Hour)}, "en")

	session.AppendAssistantErrorForTest(context.Background(), store, uuid.New(), notice)

	require.Len(t, store.appended, 1)
	assert.True(t, strings.HasPrefix(store.appended[0], domain.RateLimitNoticePrefix),
		"the client styles the bubble off this prefix; without it the user sees a red failure")
	assert.NotContains(t, store.appended[0], "**Error:**")
}

func TestChatWithoutAnExecutorFailsWithOneClearSentence(t *testing.T) {
	for name, executor := range map[string]port.ChatExecutor{
		"no executor registered at all":  nil,
		"an executor for another engine": &fakeChatExecutor{supports: false},
	} {
		t.Run(name, func(t *testing.T) {
			svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

			_, err := runTurn(t, svc, chatSession(), port.ChatStream{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "local runner host")
			assert.NotContains(t, err.Error(), "unsupported protocol scheme",
				"the empty-URL failure is exactly what this branch exists to prevent")
		})
	}
}

func TestChatRequiresAWorkspace(t *testing.T) {
	executor := &fakeChatExecutor{supports: true}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := svc.RunHostExecutedTurnForTest(
		context.Background(), chatSession(),
		domain.LLMProviderClaudeCode, "opus", domain.ToolPolicy{},
		"   ", nil, "hi", "en", port.ChatStream{},
	)
	require.Error(t, err)
	assert.Equal(t, 0, executor.calls, "nothing is spawned without a directory to spawn it in")
	assert.Contains(t, err.Error(), "workspace")
}

func TestChatStreamsThroughTheCallersCallback(t *testing.T) {
	var got []string
	executor := &fakeChatExecutor{
		supports: true,
		result:   domain.ChatResult{Response: domain.AgentResponse{Message: domain.Message{Content: "final"}}},
		stream: func(out port.ChatStream) {
			out.Text("thinking out loud")
			out.SegmentBreak()
			out.Text("final")
		},
	}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, executor)

	_, err := runTurn(t, svc, chatSession(), port.ChatStream{
		OnText:         func(s string) { got = append(got, "t:"+s) },
		OnSegmentBreak: func() { got = append(got, "break") },
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"t:thinking out loud", "break", "t:final"}, got)
}

type capturingStore struct {
	port.SessionStore
	appended []string
}

func (s *capturingStore) AppendMessage(_ context.Context, _ uuid.UUID, _ domain.Role, content string, _, _ []byte) (domain.SessionMessage, error) {
	s.appended = append(s.appended, content)
	return domain.SessionMessage{}, nil
}

func TestHostExecutedTurnCarriesTheCLIsQuestionBack(t *testing.T) {
	question := &domain.ClarificationRequest{
		Context: "Routing",
		Questions: []domain.ClarificationQuestion{{
			ID: "router", Prompt: "Which router should the new page use?",
			Options: []domain.ClarificationOption{{ID: "free_text", Label: "Type"}, {ID: "skip", Label: "Skip"}},
		}},
	}
	exec := &fakeChatExecutor{
		supports: true,
		asks:     question,
		result:   domain.ChatResult{Response: domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "Waiting on the router choice."}}},
	}
	svc := session.NewHostExecutedServiceForTest(&recordingSessionStore{}, exec)

	resp, err := runTurn(t, svc, chatSession(), port.ChatStream{})

	require.NoError(t, err)
	require.NotNil(t, resp.Clarification, "the question asked over MCP becomes the turn's clarification card")
	assert.Equal(t, question.Questions[0].Prompt, resp.Clarification.Questions[0].Prompt)
}
