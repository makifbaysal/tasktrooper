package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type LoopEfficiencySuite struct {
	suite.Suite
}

func TestLoopEfficiencySuite(t *testing.T) {
	suite.Run(t, new(LoopEfficiencySuite))
}

// scriptedLLM answers each request with the next scripted response and keeps
// every request it was sent.
type scriptedLLM struct {
	mu       sync.Mutex
	script   []domain.AgentResponse
	requests []domain.AgentRequest
}

func (f *scriptedLLM) Chat(_ context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := req
	stored.Messages = append([]domain.Message(nil), req.Messages...)
	f.requests = append(f.requests, stored)
	if len(f.script) == 0 {
		return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: "done"}}, nil
	}
	next := f.script[0]
	f.script = f.script[1:]
	return next, nil
}

func (f *scriptedLLM) ChatStream(ctx context.Context, req domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return f.Chat(ctx, req)
}

func (f *scriptedLLM) Models(context.Context) ([]string, error)                 { return nil, nil }
func (f *scriptedLLM) Embed(context.Context, string, string) ([]float32, error) { return nil, nil }

// orderedRegistry records when each call starts and ends, and holds every
// read-only call until gate calls have started, so a sequential loop would
// time out on it.
type orderedRegistry struct {
	mu      sync.Mutex
	events  []string
	gate    int
	started atomic.Int32
	release chan struct{}
	once    sync.Once
}

func newOrderedRegistry(gate int) *orderedRegistry {
	return &orderedRegistry{gate: gate, release: make(chan struct{})}
}

func (r *orderedRegistry) Register(port.ToolExecutor) {}
func (r *orderedRegistry) Definitions() []domain.ToolDefinition {
	return r.DefinitionsForPolicy(domain.ToolPolicy{})
}
func (r *orderedRegistry) AllToolNames() []string {
	return []string{"read_file", "grep_code", "write_file"}
}

func (r *orderedRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition {
	defs := make([]domain.ToolDefinition, 0, 3)
	for _, name := range r.AllToolNames() {
		defs = append(defs, domain.ToolDefinition{Type: "function", Function: domain.FunctionDefinition{Name: name}})
	}
	return defs
}

func (r *orderedRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}

func (r *orderedRegistry) log(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *orderedRegistry) ExecuteWithPolicy(_ context.Context, call domain.ToolCall, _ domain.ToolPolicy) domain.ToolResult {
	r.log("start " + call.ID)
	if call.Function.Name != "write_file" && r.gate > 0 {
		if int(r.started.Add(1)) >= r.gate {
			r.once.Do(func() { close(r.release) })
		}
		select {
		case <-r.release:
		case <-time.After(2 * time.Second):
			r.log("timeout " + call.ID)
		}
	}
	r.log("end " + call.ID)
	return domain.ToolResult{ToolCallID: call.ID, Name: call.Function.Name, Content: "result of " + call.ID}
}

func (r *orderedRegistry) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func toolCall(id, name string) domain.ToolCall {
	return domain.ToolCall{ID: id, Type: "function", Function: domain.FunctionCall{Name: name, Arguments: fmt.Sprintf(`{"path":%q}`, id)}}
}

func toolTurn(calls ...domain.ToolCall) domain.AgentResponse {
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, ToolCalls: calls}, StopReason: domain.StopReasonToolUse}
}

func truncatedTurn() domain.AgentResponse {
	resp := toolTurn(domain.ToolCall{ID: "w1", Type: "function", Function: domain.FunctionCall{Name: "write_file", Arguments: `{"path":"big.go","content":"package`}})
	resp.StopReason = domain.StopReasonMaxTokens
	return resp
}

func finalTurn(text string) domain.AgentResponse {
	return domain.AgentResponse{Message: domain.Message{Role: domain.RoleAssistant, Content: text}, StopReason: domain.StopReasonEnd}
}

func limits(window, def, maxOut int) agent.LimitsResolver {
	return func(domain.LLMProviderType, string) appcontext.ModelLimits {
		return appcontext.ModelLimits{ContextWindow: window, DefaultOutput: def, MaxOutput: maxOut}
	}
}

func toolResults(messages []domain.Message) []string {
	var ids []string
	for _, m := range messages {
		if m.Role == domain.RoleTool {
			ids = append(ids, m.ToolCallID)
		}
	}
	return ids
}

func (s *LoopEfficiencySuite) TestEachTurnAsksForTheModelsDefaultOutput() {
	llm := &scriptedLLM{script: []domain.AgentResponse{finalTurn("hi")}}
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 5, 5, 16000)
	loop.SetModelLimits(limits(200_000, 16_384, 64_000))

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "go"}}, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Equal(16_384, llm.requests[0].MaxTokens)
}

func (s *LoopEfficiencySuite) TestATruncatedToolCallIsRetriedWithADoubledCapAndNeverRun() {
	llm := &scriptedLLM{script: []domain.AgentResponse{truncatedTurn(), finalTurn("written")}}
	reg := newOrderedRegistry(0)
	loop := agent.NewLoop(llm, reg, 5, 5, 16000)
	loop.SetModelLimits(limits(200_000, 1_000, 64_000))

	resp, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "write big.go"}}, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Equal("written", resp.Message.Content)
	s.Require().Len(llm.requests, 2)
	s.Equal(1_000, llm.requests[0].MaxTokens)
	s.Equal(2_000, llm.requests[1].MaxTokens)
	s.Equal(llm.requests[0].Messages, llm.requests[1].Messages, "the retry resends the same turn, nothing appended")
	s.Empty(reg.snapshot(), "the cut-off call must not be executed")
}

func (s *LoopEfficiencySuite) TestATruncatedToolCallAtTheCeilingTellsTheModelToWriteInSmallerParts() {
	llm := &scriptedLLM{script: []domain.AgentResponse{truncatedTurn(), finalTurn("split it")}}
	reg := newOrderedRegistry(0)
	loop := agent.NewLoop(llm, reg, 5, 5, 16000)
	loop.SetModelLimits(limits(200_000, 1_000, 1_000))

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "write big.go"}}, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Require().Len(llm.requests, 2)
	second := llm.requests[1].Messages
	s.Equal(domain.Message{Role: domain.RoleSystem, Content: agent.OutputTruncatedMessageForTest(1_000)}, second[len(second)-1])
	for _, m := range second {
		s.Empty(m.ToolCalls, "the cut-off assistant turn must not enter the history")
	}
	s.Empty(reg.snapshot())
}

func (s *LoopEfficiencySuite) TestARetryThatIsStillTruncatedFallsBackToTheInstruction() {
	llm := &scriptedLLM{script: []domain.AgentResponse{truncatedTurn(), truncatedTurn(), finalTurn("ok")}}
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 5, 5, 16000)
	loop.SetModelLimits(limits(200_000, 1_000, 64_000))

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "write big.go"}}, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Require().Len(llm.requests, 3)
	third := llm.requests[2].Messages
	s.Equal(agent.OutputTruncatedMessageForTest(2_000), third[len(third)-1].Content)
	s.Equal(1_000, llm.requests[2].MaxTokens, "the next turn goes back to the default cap")
}

func (s *LoopEfficiencySuite) TestReadOnlyCallsRunTogetherAndTheirResultsKeepTheCallOrder() {
	llm := &scriptedLLM{script: []domain.AgentResponse{
		toolTurn(toolCall("a", "read_file"), toolCall("b", "grep_code"), toolCall("c", "read_file"), toolCall("d", "grep_code")),
		finalTurn("read"),
	}}
	reg := newOrderedRegistry(4)
	loop := agent.NewLoop(llm, reg, 5, 5, 16000)

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "look"}}, "m", "", domain.ToolPolicy{})

	s.Require().NoError(err)
	s.NotContains(fmt.Sprint(reg.snapshot()), "timeout", "four read-only calls must be in flight at once")
	s.Equal([]string{"a", "b", "c", "d"}, toolResults(llm.requests[1].Messages))
}

func (s *LoopEfficiencySuite) TestAWriteBetweenReadsSplitsTheBatchAndRunsInOrder() {
	llm := &scriptedLLM{script: []domain.AgentResponse{
		toolTurn(toolCall("a", "read_file"), toolCall("b", "read_file"), toolCall("w", "write_file"), toolCall("c", "read_file"), toolCall("d", "read_file")),
		finalTurn("done"),
	}}
	reg := newOrderedRegistry(2)
	loop := agent.NewLoop(llm, reg, 5, 5, 16000)

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "edit"}}, "m", "", domain.ToolPolicy{})

	s.Require().NoError(err)
	events := reg.snapshot()
	index := func(e string) int {
		for i, ev := range events {
			if ev == e {
				return i
			}
		}
		s.Failf("missing event", "%s not in %v", e, events)
		return -1
	}
	s.Less(index("end a"), index("start w"))
	s.Less(index("end b"), index("start w"))
	s.Less(index("end w"), index("start c"))
	s.Less(index("end w"), index("start d"))
	s.Equal([]string{"a", "b", "w", "c", "d"}, toolResults(llm.requests[1].Messages))
}

func (s *LoopEfficiencySuite) TestCacheReadsCountAtATenthAgainstTheRunTokenCap() {
	usage := domain.Usage{PromptTokens: 1000, CacheReadTokens: 900, CompletionTokens: 100}
	llm := &usageLLM{usage: usage}
	loop := agent.NewLoop(llm, &echoRegistry{payload: "x"}, 30, 30, 16000)
	loop.SetRunTokenCap(1000)

	_, err := loop.RunTask(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "work"}}, "m", "", domain.ToolPolicy{})

	var budgetErr *agent.BudgetExhaustedError
	s.Require().ErrorAs(err, &budgetErr)
	s.Equal(1160, budgetErr.TokensUsed, "290 per turn (100 uncached + 90 for 900 cached + 100 out) over 4 turns")
}

func (s *LoopEfficiencySuite) TestTheFinalAnswerCarriesTheWholeTranscript() {
	llm := &scriptedLLM{script: []domain.AgentResponse{toolTurn(toolCall("a", "read_file")), finalTurn("answer")}}
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 5, 5, 16000)
	opening := []domain.Message{{Role: domain.RoleSystem, Content: "persona"}, {Role: domain.RoleUser, Content: "go"}}

	resp, err := loop.RunTask(context.Background(), opening, "m", "", domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Require().Len(resp.Transcript, 5)
	s.Equal(opening, resp.Transcript[:2])
	s.Equal([]string{"a"}, toolResults(resp.Transcript))
	s.Equal(resp.Message, resp.Transcript[4])
}

func (s *LoopEfficiencySuite) TestAContinuationKeepsTheOpeningContextAsItsCacheAnchor() {
	llm := &scriptedLLM{script: []domain.AgentResponse{finalTurn("swept")}}
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 5, 5, 16000)
	transcript := []domain.Message{
		{Role: domain.RoleSystem, Content: "persona"},
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{toolCall("a", "read_file")}},
		{Role: domain.RoleTool, ToolCallID: "a", Name: "read_file", Content: "x"},
		{Role: domain.RoleAssistant, Content: "answer"},
		{Role: domain.RoleUser, Content: "now tick the criteria"},
	}

	_, err := loop.RunTask(context.Background(), transcript, "m", "", domain.ToolPolicy{}, agent.WithStableHead(2))

	s.Require().NoError(err)
	s.Equal(2, llm.requests[0].CacheAnchorIndex)
	s.Equal(transcript, llm.requests[0].Messages)
}

type stepStore struct {
	port.ActivityStore
	mu    sync.Mutex
	steps []domain.SessionStep
}

func (s *stepStore) CreateRun(context.Context, *uuid.UUID, string, string) (domain.SessionRun, error) {
	return domain.SessionRun{ID: uuid.New()}, nil
}

func (s *stepStore) AppendStep(_ context.Context, _ uuid.UUID, kind string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, domain.SessionStep{StepType: kind, Payload: payload})
	return nil
}

func (s *LoopEfficiencySuite) TestEachLLMRequestStepPreviewsOnlyWhatIsNewSinceTheLastOne() {
	llm := &scriptedLLM{script: []domain.AgentResponse{toolTurn(toolCall("a", "read_file")), toolTurn(toolCall("b", "read_file")), finalTurn("done")}}
	store := &stepStore{}
	ctx, _, err := activity.StartRun(context.Background(), store, nil, "req", "m")
	s.Require().NoError(err)
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 30, 30, 16000)
	opening := []domain.Message{{Role: domain.RoleSystem, Content: "persona"}, {Role: domain.RoleUser, Content: "go"}}

	_, err = loop.RunTask(ctx, opening, "m", "", domain.ToolPolicy{})
	s.Require().NoError(err)

	type payload struct {
		MessageCount    int `json:"message_count"`
		NewMessageCount int `json:"new_message_count"`
		Messages        []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	var got []payload
	for _, step := range store.steps {
		if step.StepType != "llm_request" {
			continue
		}
		var p payload
		s.Require().NoError(json.Unmarshal(step.Payload, &p))
		got = append(got, p)
	}
	s.Require().Len(got, 3)
	s.Equal([]int{2, 4, 6}, []int{got[0].MessageCount, got[1].MessageCount, got[2].MessageCount})
	s.Equal([]int{2, 2, 2}, []int{got[0].NewMessageCount, got[1].NewMessageCount, got[2].NewMessageCount})
	s.Equal("assistant", got[1].Messages[0].Role)
	s.Equal("tool:read_file", got[1].Messages[1].Role)
}

type countingRegistry struct {
	mu    sync.Mutex
	execs map[string]int
}

func (r *countingRegistry) Register(port.ToolExecutor) {}
func (r *countingRegistry) Definitions() []domain.ToolDefinition {
	return r.DefinitionsForPolicy(domain.ToolPolicy{})
}
func (r *countingRegistry) AllToolNames() []string { return []string{"read_file"} }
func (r *countingRegistry) DefinitionsForPolicy(domain.ToolPolicy) []domain.ToolDefinition {
	return []domain.ToolDefinition{{Type: "function", Function: domain.FunctionDefinition{Name: "read_file"}}}
}
func (r *countingRegistry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	return r.ExecuteWithPolicy(ctx, call, domain.ToolPolicy{})
}
func (r *countingRegistry) ExecuteWithPolicy(_ context.Context, call domain.ToolCall, _ domain.ToolPolicy) domain.ToolResult {
	r.mu.Lock()
	r.execs[call.ID]++
	r.mu.Unlock()
	return domain.ToolResult{ToolCallID: call.ID, Name: call.Function.Name, Content: "content " + call.Function.Arguments}
}

func (s *LoopEfficiencySuite) TestADuplicateReadInABatchRunsNoOtherCallTwice() {
	read := func(id, path string) domain.ToolCall {
		return domain.ToolCall{ID: id, Type: "function", Function: domain.FunctionCall{Name: "read_file", Arguments: fmt.Sprintf(`{"path":%q}`, path)}}
	}
	llm := &scriptedLLM{script: []domain.AgentResponse{
		toolTurn(read("c1", "a"), read("c2", "a"), read("c3", "b"), read("c4", "c")),
		finalTurn("done"),
	}}
	reg := &countingRegistry{execs: map[string]int{}}
	store := &stepStore{}
	ctx, _, err := activity.StartRun(context.Background(), store, nil, "req", "m")
	s.Require().NoError(err)
	loop := agent.NewLoop(llm, reg, 10, 10, 10000)

	_, err = loop.Run(ctx, []domain.Message{{Role: domain.RoleUser, Content: "go"}}, "m", "", domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Equal(map[string]int{"c1": 1, "c2": 1, "c3": 1, "c4": 1}, reg.execs)
	starts := map[string]int{}
	for _, step := range store.steps {
		if step.StepType != "tool_call_start" {
			continue
		}
		var p struct {
			CallID string `json:"call_id"`
		}
		s.Require().NoError(json.Unmarshal(step.Payload, &p))
		starts[p.CallID]++
	}
	s.Equal(map[string]int{"c1": 1, "c2": 1, "c3": 1, "c4": 1}, starts)
	s.Equal([]string{"c1", "c2", "c3", "c4"}, toolResults(llm.requests[1].Messages))
}

func (s *LoopEfficiencySuite) TestTheTranscriptLeavesOutTheLoopsOwnNotes() {
	llm := &scriptedLLM{script: []domain.AgentResponse{toolTurn(toolCall("a", "read_file")), truncatedTurn(), finalTurn("answer")}}
	loop := agent.NewLoop(llm, newOrderedRegistry(0), 3, 3, 16000)
	loop.SetModelLimits(limits(200_000, 1_000, 1_000))
	opening := []domain.Message{{Role: domain.RoleSystem, Content: "persona"}, {Role: domain.RoleUser, Content: "go"}}

	resp, err := loop.RunTask(context.Background(), opening, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{})

	s.Require().NoError(err)
	s.Len(llm.requests[0].Messages, 3, "the turns-left warning went out with the first turn")
	sent := llm.requests[2].Messages
	s.Equal(agent.OutputTruncatedMessageForTest(1_000), sent[len(sent)-1].Content, "the run itself was told")
	var system []string
	for _, m := range resp.Transcript {
		if m.Role == domain.RoleSystem {
			system = append(system, m.Content)
		}
	}
	s.Equal([]string{"persona"}, system, "a follow-up on a fresh budget must not inherit this run's warnings")
	s.Equal([]string{"a"}, toolResults(resp.Transcript))
	s.Equal(resp.Message, resp.Transcript[len(resp.Transcript)-1])
}

// streamingLLM streams each scripted response's tokens before returning it.
type streamingLLM struct {
	*scriptedLLM
	tokens [][]string
}

func (f *streamingLLM) ChatStream(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, error) {
	resp, err := f.Chat(ctx, req)
	f.mu.Lock()
	turn := f.tokens[0]
	f.tokens = f.tokens[1:]
	f.mu.Unlock()
	for _, tok := range turn {
		onToken(tok)
	}
	return resp, err
}

func (s *LoopEfficiencySuite) TestARetriedTruncatedTurnStreamsOnlyWhatTheUserHasNotSeen() {
	tests := []struct {
		name  string
		retry []string
		want  string
	}{
		{"the retry repeats the shown text and goes on", []string{"Writing", " the", " file."}, "Writing the file."},
		{"the retry splits its tokens differently", []string{"Writ", "ing the f", "ile."}, "Writing the file."},
		{"the retry departs from the shown text", []string{"Let me", " split it."}, "Writing the|Let me split it."},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			cut := truncatedTurn()
			cut.Message.Content = "Writing the"
			llm := &streamingLLM{
				scriptedLLM: &scriptedLLM{script: []domain.AgentResponse{cut, finalTurn(strings.Join(tt.retry, ""))}},
				tokens:      [][]string{{"Writing", " the"}, tt.retry},
			}
			loop := agent.NewLoop(llm, newOrderedRegistry(0), 5, 5, 16000)
			loop.SetModelLimits(limits(200_000, 1_000, 64_000))
			var stream strings.Builder
			ctx := agent.WithSegmentBreak(context.Background(), func() { stream.WriteString("|") })

			_, err := loop.RunStream(ctx, []domain.Message{{Role: domain.RoleUser, Content: "write big.go"}}, "m", domain.LLMProviderAnthropic, domain.ToolPolicy{}, func(tok string) { stream.WriteString(tok) })

			s.Require().NoError(err)
			s.Require().Len(llm.requests, 2)
			s.Equal(2_000, llm.requests[1].MaxTokens, "the retry is streamed with the raised cap")
			s.Equal(tt.want, stream.String())
		})
	}
}
