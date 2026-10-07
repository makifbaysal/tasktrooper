package mcpserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var deployQuestion = domain.ClarificationRequest{
	Context: "Deploy target",
	Questions: []domain.ClarificationQuestion{{
		ID: "host", Prompt: "Where should it be hosted?",
		Options: []domain.ClarificationOption{{ID: "free_text", Label: "Type"}, {ID: "skip", Label: "Skip"}},
	}},
}

func TestAskUserIsServedToARunThatWaitsForTheAnswer(t *testing.T) {
	sink := &domain.ClarificationSink{}
	run := Run{Ctx: domain.WithClarificationSink(context.Background(), sink)}
	app, _, token := newTestServer(t, &fakeRegistry{defs: fullCatalog()}, run)

	_, body := call(t, app, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	assert.Contains(t, toolNames(t, body), domain.AskUserToolName)
}

func TestAskUserHandsTheQuestionToTheRunAndEndsTheTurn(t *testing.T) {
	sink := &domain.ClarificationSink{}
	reg := &fakeRegistry{
		defs:    fullCatalog(),
		results: map[string]domain.ToolResult{domain.AskUserToolName: {Name: domain.AskUserToolName, Clarification: &deployQuestion}},
	}
	app, _, token := newTestServer(t, reg, Run{Ctx: domain.WithClarificationSink(context.Background(), sink)})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_user","arguments":{}}}`)
	blocks, isError := callResultOf(t, body)

	assert.False(t, isError, "a recorded question is a success the CLI should stop on, not an error to retry")
	assert.Contains(t, blocks[0].(map[string]any)["text"], "End your turn now")
	got := sink.Request()
	require.NotNil(t, got)
	assert.Equal(t, deployQuestion.Questions[0].Prompt, got.Questions[0].Prompt)
}

func TestAskUserStaysRefusedWithoutASink(t *testing.T) {
	reg := &fakeRegistry{defs: fullCatalog()}
	app, _, token := newTestServer(t, reg, Run{})

	_, body := call(t, app, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_user","arguments":{}}}`)
	blocks, isError := callResultOf(t, body)

	assert.True(t, isError)
	assert.Contains(t, blocks[0].(map[string]any)["text"], "parks the run")
	assert.Empty(t, reg.calls())
}
