package session_test

import (
	gocontext "context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type capturingTitleChatClient struct {
	seen  domain.AgentRequest
	reply string
	err   error
}

func (c *capturingTitleChatClient) Chat(_ gocontext.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	c.seen = req
	if c.err != nil {
		return domain.AgentResponse{}, c.err
	}
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: c.reply}}, nil
}

func (c *capturingTitleChatClient) ChatStream(ctx gocontext.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return c.Chat(ctx, req)
}

func (c *capturingTitleChatClient) Models(gocontext.Context) ([]string, error) { return nil, nil }

func (c *capturingTitleChatClient) Embed(gocontext.Context, string, string) ([]float32, error) {
	return nil, nil
}

func TestGenerateTitleCapsItsOwnRequestAtTitleMaxTokens(t *testing.T) {
	client := &capturingTitleChatClient{reply: "  Postgres migration rollback  "}
	g := session.NewLLMTitleGenerator(client)

	title, err := g.GenerateTitle(gocontext.Background(), "how do I roll back a migration?", "run the .down.sql file", "test-model", domain.LLMProviderAnthropic)
	require.NoError(t, err)

	assert.Equal(t, "Postgres migration rollback", title, "GenerateTitle must trim the model's own whitespace")
	assert.Equal(t, "test-model", client.seen.Model)
	assert.Equal(t, domain.LLMProviderAnthropic, client.seen.ProviderType)
	assert.LessOrEqual(t, client.seen.MaxTokens, 24)
}

func TestFallbackTitleTrimsAndCapsAtSixtyBytes(t *testing.T) {
	assert.Equal(t, "merhaba dünya", session.FallbackTitle("  merhaba dünya  "))

	long := "bu çok uzun bir kullanıcı mesajı, altmış bayttan kesinlikle daha uzun olacak şekilde yazıldı"
	got := session.FallbackTitle(long)
	assert.LessOrEqual(t, len(got), 60)
	assert.True(t, len(got) > 0)
}
