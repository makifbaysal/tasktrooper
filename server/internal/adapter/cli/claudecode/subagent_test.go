package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const subagentStream = `{"type":"system","subtype":"init","session_id":"s1","model":"m"}
{"type":"assistant","parent_tool_use_id":null,"message":{"content":[{"type":"text","text":"Delegating the survey."},{"type":"tool_use","id":"toolu_task","name":"Task","input":{"description":"survey","prompt":"p","subagent_type":"general-purpose"}}]}}
{"type":"assistant","parent_tool_use_id":"toolu_task","message":{"content":[{"type":"text","text":"SUBAGENT narration"},{"type":"tool_use","id":"toolu_sub_read","name":"Read","input":{"file_path":"a.go"}}]}}
{"type":"user","parent_tool_use_id":"toolu_task","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_sub_read","content":"package a"}]}}
{"type":"assistant","parent_tool_use_id":"toolu_task","message":{"content":[{"type":"text","text":"SUBAGENT final"}]}}
{"type":"user","parent_tool_use_id":null,"message":{"content":[{"type":"tool_result","tool_use_id":"toolu_task","content":"survey done"}]}}
{"type":"assistant","parent_tool_use_id":null,"message":{"content":[{"type":"text","text":"All done."}]}}
{"type":"result","subtype":"success","is_error":false,"num_turns":4,"session_id":"s1"}
`

func TestSubAgentStepsCarryParentCallID(t *testing.T) {
	store := &traceStore{}
	ctx, _, err := activity.StartRun(context.Background(), store, nil, "req-sub", "m")
	require.NoError(t, err)

	var streamed []string
	breaks := 0
	trace := core.NewTrace(ctx, "task-1", "claude_code_session", recordedElsewhere, ledgerToolName)
	s := newStreamingSink(trace, port.ChatStream{
		OnText:         func(text string) { streamed = append(streamed, text) },
		OnSegmentBreak: func() { breaks++ },
	})

	out, err := parseStream(strings.NewReader(subagentStream), s)
	require.NoError(t, err)

	assert.Equal(t, "All done.", out.Text, "a sub-agent's closing text must not become the parent's answer")
	assert.Equal(t, []string{"Delegating the survey.", "All done."}, streamed,
		"the live chat stream carries only the main agent's text")
	assert.Equal(t, 1, breaks, "only the main agent's tool_use breaks the segment")

	starts := store.payloads("tool_call_start")
	require.Len(t, starts, 2)
	assert.Equal(t, "subagent", starts[0]["tool"], "Task is recorded under the subagent ledger name")
	assert.NotContains(t, starts[0], "parent_call_id")
	assert.Equal(t, "read_file", starts[1]["tool"])
	assert.Equal(t, "toolu_task", starts[1]["parent_call_id"])

	results := store.payloads("tool_call_result")
	require.Len(t, results, 2)
	assert.Equal(t, "toolu_sub_read", results[0]["call_id"])
	assert.Equal(t, "toolu_task", results[0]["parent_call_id"])
	assert.Equal(t, "subagent", results[1]["tool"])
	assert.NotContains(t, results[1], "parent_call_id")

	msgs := store.payloads("assistant_message")
	require.Len(t, msgs, 4)
	assert.NotContains(t, msgs[0], "parent_call_id")
	assert.Equal(t, "toolu_task", msgs[1]["parent_call_id"])
	assert.Equal(t, "toolu_task", msgs[2]["parent_call_id"])
	assert.NotContains(t, msgs[3], "parent_call_id")

	iters := store.payloads("iteration_start")
	require.Len(t, iters, 4)
	assert.Equal(t, "toolu_task", iters[1]["parent_call_id"])
	assert.NotContains(t, iters[3], "parent_call_id")
}

func TestAgentToolAlsoMapsToSubagent(t *testing.T) {
	assert.Equal(t, "subagent", ledgerToolName("Agent"))
	assert.Equal(t, "subagent", ledgerToolName("Task"))
}
