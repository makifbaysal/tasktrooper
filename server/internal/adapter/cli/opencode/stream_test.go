package opencode

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// recordingSink captures every callback parseStream makes, in order, so a
// test can assert on both the outcome AND what a live chat stream would have
// seen.
type recordingSink struct {
	sessions  []string
	turns     int
	texts     []string
	toolUses  []string
	callIDs   []string
	toolDone  []string
	toolFails []bool
}

func (r *recordingSink) OnSession(sessionID, model string) {
	r.sessions = append(r.sessions, sessionID)
}
func (r *recordingSink) OnTurn()                     { r.turns++ }
func (r *recordingSink) OnAssistantText(text string) { r.texts = append(r.texts, text) }
func (r *recordingSink) OnToolUse(callID, name, arguments string) {
	r.toolUses = append(r.toolUses, name)
	r.callIDs = append(r.callIDs, callID)
}
func (r *recordingSink) OnToolResult(callID, name, content string, isError bool) {
	r.toolDone = append(r.toolDone, name)
	r.toolFails = append(r.toolFails, isError)
}

func TestParseStreamReadsAFinishedSession(t *testing.T) {
	body := `{"type":"step_start","sessionID":"ses_1"}
{"type":"text","sessionID":"ses_1","part":{"id":"prt_1","text":"Hello"}}
{"type":"step_finish","sessionID":"ses_1","part":{"reason":"stop","cost":0.02,"tokens":{"input":100,"output":20,"reasoning":5,"cache":{"read":10,"write":1}}}}
`
	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.True(t, out.SawResult)
	assert.False(t, out.IsError)
	assert.Equal(t, "ses_1", out.SessionID)
	assert.Equal(t, "Hello", out.Text)
	assert.Equal(t, "stop", out.Status)
	assert.Equal(t, 0.02, out.CostUSD)
	assert.Equal(t, domain.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120, CacheReadTokens: 10}, out.Usage)

	assert.Equal(t, []string{"ses_1"}, sink.sessions)
	assert.Equal(t, 1, sink.turns)
	assert.Equal(t, []string{"Hello"}, sink.texts)
}

// A "text" part carries the FULL text seen so far for that id, not a delta —
// parseStream has to turn two snapshots of the same part into the incremental
// chunk a live chat stream needs, the way textAccum.update documents.
func TestTextPartsAreSnapshotsNotDeltas(t *testing.T) {
	body := `{"type":"step_start","sessionID":"ses_2"}
{"type":"text","sessionID":"ses_2","part":{"id":"prt_1","text":"Reading the"}}
{"type":"text","sessionID":"ses_2","part":{"id":"prt_1","text":"Reading the file now."}}
`
	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.Equal(t, []string{"Reading the", " file now."}, sink.texts,
		"the second callback must be only the NEW suffix, not the whole snapshot again")
	assert.Equal(t, "Reading the file now.", out.Text, "with no step_finish/error, out.Text falls back to the joined parts")
}

// A part whose new snapshot does not extend the previous one (an edit, not an
// append) is reported whole rather than guessing at a diff.
func TestTextPartThatDoesNotExtendThePreviousOneIsReportedWhole(t *testing.T) {
	body := `{"type":"step_start","sessionID":"ses_3"}
{"type":"text","sessionID":"ses_3","part":{"id":"prt_1","text":"Draft one."}}
{"type":"text","sessionID":"ses_3","part":{"id":"prt_1","text":"Completely different."}}
`
	sink := &recordingSink{}
	_, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)
	assert.Equal(t, []string{"Draft one.", "Completely different."}, sink.texts)
}

func TestParseStreamHandlesToolCompletionAndError(t *testing.T) {
	body := `{"type":"step_start","sessionID":"ses_4"}
{"type":"tool_use","sessionID":"ses_4","part":{"callID":"c1","tool":"read","state":{"status":"running","input":{"path":"a.go"}}}}
{"type":"tool_use","sessionID":"ses_4","part":{"callID":"c1","tool":"read","state":{"status":"completed","input":{"path":"a.go"},"output":"package a"}}}
{"type":"tool_use","sessionID":"ses_4","part":{"callID":"c2","tool":"bash","state":{"status":"running","input":{"command":"go build"}}}}
{"type":"tool_use","sessionID":"ses_4","part":{"callID":"c2","tool":"bash","state":{"status":"error","input":{"command":"go build"},"output":"boom"}}}
{"type":"step_finish","sessionID":"ses_4","part":{"reason":"stop","cost":0,"tokens":{}}}
`
	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.Equal(t, 2, out.ToolCalls)
	assert.Equal(t, 1, out.ToolFailures)
	// OnToolUse fires on BOTH the "running" state and the terminal
	// "completed"/"error" state (parseStream's tool_use case calls it again
	// alongside OnToolResult), so each tool appears twice here.
	assert.Equal(t, []string{"read", "read", "bash", "bash"}, sink.toolUses)
	assert.Equal(t, []string{"read", "bash"}, sink.toolDone)
	assert.Equal(t, []bool{false, true}, sink.toolFails)
}

// An "error" event is a terminal event — SawResult true, IsError true — and
// its message is the CLI's own words, not a paraphrase.
func TestParseStreamReadsAnErrorEvent(t *testing.T) {
	body := `{"type":"step_start","sessionID":"ses_5"}
{"type":"error","sessionID":"ses_5","error":{"name":"rate_limit_exceeded","data":{"message":"Rate limit exceeded. Please try again later."}}}
`
	out, err := parseStream(strings.NewReader(body), &recordingSink{})
	require.NoError(t, err)

	assert.True(t, out.SawResult)
	assert.True(t, out.IsError)
	assert.Equal(t, "rate_limit_exceeded", out.Status)
	assert.Equal(t, "Rate limit exceeded. Please try again later.", out.Text)
}

// Malformed or non-event lines (a stray log line that slipped onto stdout, an
// empty line) are skipped rather than aborting the whole stream.
func TestParseStreamSkipsLinesThatAreNotEvents(t *testing.T) {
	body := "not json at all\n" +
		"\n" +
		`{"type":"step_start","sessionID":"ses_6"}` + "\n" +
		"{ this is not valid json\n" +
		`{"type":"text","sessionID":"ses_6","part":{"id":"prt_1","text":"still works"}}` + "\n"

	sink := &recordingSink{}
	out, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)
	assert.Equal(t, "ses_6", out.SessionID)
	assert.Equal(t, []string{"still works"}, sink.texts)
}

func TestParseStreamReportsNoTerminalEventAsSuchWhenNothingHappened(t *testing.T) {
	out, err := parseStream(strings.NewReader(""), &recordingSink{})
	require.NoError(t, err)
	assert.False(t, out.SawResult)
	assert.Empty(t, out.Text)
}

// 2.x dropped callID from a tool part and carries the call id as "id"; 1.x's
// "id" is the part id and must not win over its callID.
func TestParseStreamReadsTheCallIDOfBothGenerations(t *testing.T) {
	body := `{"type":"tool_use","sessionID":"ses_1","part":{"id":"prt_1","callID":"call_v1","tool":"read","state":{"status":"completed","input":{},"output":"x"}}}
{"type":"tool_use","sessionID":"ses_1","part":{"partID":"prt_2","id":"call_v2","tool":"tasktrooper_list_board_tasks","state":{"status":"completed","input":{},"output":"ok"}}}
`
	sink := &recordingSink{}
	_, err := parseStream(strings.NewReader(body), sink)
	require.NoError(t, err)

	assert.Equal(t, []string{"call_v1", "call_v2"}, sink.callIDs)
	assert.Equal(t, []string{"read", "tasktrooper_list_board_tasks"}, sink.toolUses)
}
