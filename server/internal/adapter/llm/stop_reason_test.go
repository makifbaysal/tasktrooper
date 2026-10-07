package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func cannedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if len(body) > 0 && body[0] != '{' {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

var hiRequest = domain.AgentRequest{Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}}}

func TestAnthropicChatReportsATruncatedTurn(t *testing.T) {
	srv := cannedServer(t, `{"content":[{"type":"tool_use","id":"t1","name":"write_file","input":{}}],"stop_reason":"max_tokens","usage":{}}`)

	resp, err := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second).Chat(context.Background(), hiRequest)

	require.NoError(t, err)
	assert.Equal(t, domain.StopReasonMaxTokens, resp.StopReason)
	assert.Len(t, resp.Message.ToolCalls, 1)
}

func TestAnthropicChatStreamReportsTheStopReasonFromMessageDelta(t *testing.T) {
	srv := cannedServer(t, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{}}}\n\n"+
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"+
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")

	resp, err := NewAnthropicClient(srv.URL, "claude-sonnet-4", "k", 5*time.Second).ChatStream(context.Background(), hiRequest, func(string) {})

	require.NoError(t, err)
	assert.Equal(t, domain.StopReasonEnd, resp.StopReason)
}

func TestOpenAICompatChatReportsALengthFinishAsTruncated(t *testing.T) {
	srv := cannedServer(t, `{"choices":[{"message":{"role":"assistant","content":"par"},"finish_reason":"length"}]}`)

	resp, err := NewOpenAICompatClient(srv.URL, "m", "", 5*time.Second).Chat(context.Background(), hiRequest)

	require.NoError(t, err)
	assert.Equal(t, domain.StopReasonMaxTokens, resp.StopReason)
}

func TestOpenAICompatChatStreamReportsTheLastFinishReason(t *testing.T) {
	srv, _ := streamServer(t, []string{
		`{"choices":[{"delta":{"content":"hi"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	})

	resp, err := NewOpenAICompatClient(srv.URL, "m", "", 5*time.Second).ChatStream(context.Background(), hiRequest, func(string) {})

	require.NoError(t, err)
	assert.Equal(t, domain.StopReasonToolUse, resp.StopReason)
}
