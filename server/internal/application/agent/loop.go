package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmretry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxRetries = 2

const minShrinkTokens = 2000

const toolOutputTruncateSuffix = "\n\n[…middle of the output omitted; the end, where the error is, follows…]"

// parallelSafeTools may run side by side within one turn: they read the
// workspace, the index or the web and change none of them, so running them
// together cannot change any result. Everything else — terminal, file writes,
// browser, board — runs alone and in the order the model asked.
var parallelSafeTools = map[string]bool{
	"read_file":             true,
	"grep_code":             true,
	"get_repo_tree":         true,
	"codebase_search":       true,
	"get_symbol_skeleton":   true,
	"expand_symbol_context": true,
	"web_search":            true,
	"fetch_url":             true,
}

const maxParallelToolCalls = 4

// Cache reads are billed at a tenth of an input token, and a long run re-reads
// its whole prefix every turn; counting them at full weight ended runs on a
// budget they had not spent.
const cacheReadTokenWeight = 0.1

// LimitsResolver names the context window and output caps of one model.
type LimitsResolver func(provider domain.LLMProviderType, model string) appcontext.ModelLimits

type Loop struct {
	llm                port.LLMClient
	registry           port.ToolRegistry
	maxIterations      int
	taskMaxIterations  int
	maxToolOutputChars int
	historyBudget      appcontext.Budget
	modelLimits        LimitsResolver
	summarizer         appcontext.Summarizer
	runTokenCap        int
	screenshots        ScreenshotArchiver
}

type runHead struct {
	len        int
	model      string
	provider   domain.LLMProviderType
	lightModel string
	effort     string
	budget     appcontext.Budget
	maxOutput  int
	wrapBudget appcontext.Budget
}

func (h runHead) anchor(n int) int {
	if h.len < 0 {
		return 0
	}
	if h.len > n {
		return n
	}
	return h.len
}

type RunOption func(*runOptions)

type runOptions struct {
	lightModel       string
	maxTurns         int
	effort           string
	cliLabel         string
	cliTitle         string
	scratchWorkspace bool
	stableHead       int
}

func applyRunOptions(opts []RunOption) runOptions {
	var cfg runOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func WithCLILabel(label, title string) RunOption {
	return func(o *runOptions) { o.cliLabel, o.cliTitle = label, title }
}

func WithScratchWorkspace() RunOption {
	return func(o *runOptions) { o.scratchWorkspace = true }
}

func WithLightModel(model string) RunOption {
	return func(o *runOptions) { o.lightModel = model }
}

func WithSessionLimits(maxTurns int, effort string) RunOption {
	return func(o *runOptions) { o.maxTurns, o.effort = maxTurns, effort }
}

// WithStableHead marks the first n messages as the run's opening context when
// the messages handed in are longer than that — a follow-up that continues an
// earlier run's transcript. Without it the whole transcript would count as
// head, which summarisation never touches, so a continued run could only grow.
func WithStableHead(n int) RunOption {
	return func(o *runOptions) { o.stableHead = n }
}

func NewLoop(llm port.LLMClient, registry port.ToolRegistry, maxIterations, taskMaxIterations, maxToolOutputChars int) *Loop {
	if maxToolOutputChars <= 0 {
		maxToolOutputChars = 16000
	}
	if maxIterations <= 0 {
		maxIterations = 30
	}
	if taskMaxIterations <= 0 {
		taskMaxIterations = maxIterations
	}
	return &Loop{
		llm:                llm,
		registry:           registry,
		maxIterations:      maxIterations,
		taskMaxIterations:  taskMaxIterations,
		maxToolOutputChars: maxToolOutputChars,
	}
}

// SetHistoryBudget sets the budget every run trims to. With a limits resolver
// set (SetModelLimits) its non-zero fields are operator overrides on top of
// each model's own limits instead.
func (l *Loop) SetHistoryBudget(budget appcontext.Budget) {
	l.historyBudget = budget
}

// SetModelLimits makes the context window and output cap follow the model a
// run is on rather than one fixed budget.
func (l *Loop) SetModelLimits(resolve LimitsResolver) {
	l.modelLimits = resolve
}

func (l *Loop) SetSummarizer(s appcontext.Summarizer) {
	l.summarizer = s
}

func (l *Loop) SetRunTokenCap(tokens int) {
	l.runTokenCap = tokens
}

func (l *Loop) budgetFor(provider domain.LLMProviderType, model string) (appcontext.Budget, int) {
	if l.modelLimits == nil {
		return l.historyBudget, l.historyBudget.ReserveOutput
	}
	limits := l.modelLimits(provider, model)
	b := appcontext.ResolveBudget(l.historyBudget, limits)
	return b, max(limits.MaxOutput, b.ReserveOutput)
}

func (l *Loop) newRunHead(historyLen int, model string, provider domain.LLMProviderType, opts ...RunOption) runHead {
	cfg := applyRunOptions(opts)
	lightModel := cfg.lightModel
	if lightModel == "" {
		lightModel = model
	}
	headLen := historyLen
	if cfg.stableHead > 0 && cfg.stableHead < historyLen {
		headLen = cfg.stableHead
	}
	budget, maxOutput := l.budgetFor(provider, model)
	wrapBudget, _ := l.budgetFor(provider, lightModel)
	return runHead{
		len: headLen, model: model, provider: provider, lightModel: lightModel, effort: cfg.effort,
		budget: budget, maxOutput: maxOutput, wrapBudget: wrapBudget,
	}
}

func iterationBudget(configured int, opts ...RunOption) int {
	if cfg := applyRunOptions(opts); cfg.maxTurns > 0 {
		return cfg.maxTurns
	}
	return configured
}

func (l *Loop) Run(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	return l.drive(ctx, messages, model, provider, policy, iterationBudget(l.maxIterations, opts...), l.sendQuiet, false, opts...)
}

func (l *Loop) RunTask(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, opts ...RunOption) (domain.AgentResponse, error) {
	return l.drive(ctx, messages, model, provider, policy, iterationBudget(l.taskMaxIterations, opts...), l.sendQuiet, false, opts...)
}

func (l *Loop) RunStream(ctx context.Context, messages []domain.Message, model string, provider domain.LLMProviderType, policy domain.ToolPolicy, onToken func(string), opts ...RunOption) (domain.AgentResponse, error) {
	send := func(ctx context.Context, req domain.AgentRequest, shown string) (domain.AgentResponse, []domain.Message, error) {
		return l.chatStreamWithRetry(ctx, req, withoutShownPrefix(ctx, shown, onToken))
	}
	return l.drive(ctx, messages, model, provider, policy, l.maxIterations, send, true, opts...)
}

// sendFunc's shown is the text of a cut-off turn the user already watched
// stream in, which the retry of that turn must not stream again.
type sendFunc func(ctx context.Context, req domain.AgentRequest, shown string) (domain.AgentResponse, []domain.Message, error)

func (l *Loop) sendQuiet(ctx context.Context, req domain.AgentRequest, _ string) (domain.AgentResponse, []domain.Message, error) {
	return l.chatWithRetry(ctx, req)
}

func (l *Loop) drive(
	ctx context.Context,
	messages []domain.Message,
	model string,
	provider domain.LLMProviderType,
	policy domain.ToolPolicy,
	budget int,
	send sendFunc,
	streaming bool,
	opts ...RunOption,
) (domain.AgentResponse, error) {
	if err := guardHostExecuted(provider); err != nil {
		return domain.AgentResponse{}, err
	}
	if budget <= 0 {
		budget = 1
	}
	tools := l.registry.DefinitionsForPolicy(policy)

	toolTokens := appcontext.CountToolTokens(tools)

	history := make([]domain.Message, len(messages))
	copy(history, messages)

	head := l.newRunHead(len(history), model, provider, opts...)

	tracker := newCallTracker()
	gate := newClarificationGate(tools)
	warned := false
	tokenWarned := false
	emptyTurns := 0
	totalTokens := 0
	calib := newTokenCalibration()
	recorded := 0
	notes := loopNotes{}

	for i := range budget {
		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}

		if remaining := budget - i; remaining <= budgetWarningTurns && !warned {
			warned = true
			history = append(history, notes.add(budgetWarningMessage(remaining)))
			if rec := activity.FromContext(ctx); rec != nil && !streaming {
				rec.Step("budget_warning", map[string]int{"turns_left": remaining})
			}
		}

		if l.runTokenCap > 0 && !tokenWarned && totalTokens >= runTokenWarnThreshold(l.runTokenCap) {
			tokenWarned = true
			history = append(history, notes.add(tokenBudgetWarningMessage(totalTokens, l.runTokenCap)))
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_warning", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap})
			}
		}

		fresh := history[min(recorded, len(history)):]
		history = l.fitHistory(ctx, history, head, effectiveBudget(head.budget, toolTokens, calib.current()))

		log.Debug().Int("iteration", i+1).Int("messages", len(history)).Bool("stream", streaming).Msg("agent loop iteration")

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("iteration_start", map[string]int{"iteration": i + 1, "message_count": len(history)})
		}

		req := domain.AgentRequest{
			Messages:         history,
			Tools:            tools,
			ProviderType:     provider,
			Model:            model,
			ToolPolicy:       policy,
			CacheAnchorIndex: head.anchor(len(history)),
			Effort:           head.effort,
			MaxTokens:        head.budget.ReserveOutput,
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("llm_request", buildLLMRequestPayload(model, len(history), fresh, len(tools)))
		}

		resp, sent, err := send(ctx, req, "")
		history = sent
		recorded = len(history)
		if err != nil {
			return domain.AgentResponse{}, &ChatFailedError{Stats: tracker.snapshot(i + 1), Err: err}
		}

		totalTokens += runTokens(resp.Usage)

		calib.observe(resp.Usage.PromptTokens, appcontext.CountTokens(history)+toolTokens)

		if truncatedToolCalls(resp) {
			if raised := raisedOutputCap(req.MaxTokens, head.maxOutput, head.budget.MaxTokens, resp.Usage.PromptTokens); raised > 0 {
				log.Warn().Int("iteration", i+1).Int("max_tokens", req.MaxTokens).Int("retry_max_tokens", raised).
					Msg("model hit the output limit inside a tool call; asking again with more room")
				if rec := activity.FromContext(ctx); rec != nil {
					rec.Step("output_truncated_retry", map[string]int{"iteration": i + 1, "max_tokens": req.MaxTokens, "retry_max_tokens": raised})
				}
				req.Messages = history
				req.MaxTokens = raised
				resp, sent, err = send(ctx, req, resp.Message.Content)
				history = sent
				recorded = len(history)
				if err != nil {
					return domain.AgentResponse{}, &ChatFailedError{Stats: tracker.snapshot(i + 1), Err: err}
				}
				totalTokens += runTokens(resp.Usage)
			}
			if truncatedToolCalls(resp) {
				log.Warn().Int("iteration", i+1).Int("max_tokens", req.MaxTokens).
					Msg("model hit the output limit inside a tool call at the largest cap; its calls were not run")
				if rec := activity.FromContext(ctx); rec != nil {
					rec.Step("output_truncated", map[string]int{"iteration": i + 1, "max_tokens": req.MaxTokens})
				}
				history = append(history, notes.add(outputTruncatedMessage(req.MaxTokens)))
				if streaming {
					segmentBreak(ctx)
				}
				continue
			}
		}

		if len(resp.Message.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Message.Content) == "" {
				emptyTurns++
				if emptyTurns <= 1 {
					log.Warn().Int("iteration", i+1).Msg("model returned an empty turn; asking for the final answer")
					if rec := activity.FromContext(ctx); rec != nil && !streaming {
						rec.Step("empty_turn_retry", map[string]int{"iteration": i + 1})
					}
					history = append(history, notes.add(emptyTurnPrompt))
					continue
				}
				if !streaming {
					resp.Message.Content = emptyTurnFallback
				}
			}
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("assistant_message", map[string]string{"content": resp.Message.Content})
			}
			log.Debug().Int("iterations", i+1).Msg("agent loop complete")
			resp.Transcript = transcriptOf(history, resp.Message, head.anchor(len(history)), notes)
			return resp, nil
		}
		emptyTurns = 0

		if streaming {
			segmentBreak(ctx)
		}

		if rec := activity.FromContext(ctx); rec != nil {
			rec.Step("tool_calls_planned", map[string]any{
				"content":    resp.Message.Content,
				"tool_calls": toolCallPayloads(resp.Message.ToolCalls),
			})
		}

		history = append(history, resp.Message)

		updated, clarification, resourceBlock, stuck, deadEnd := l.runToolCalls(ctx, history, resp.Message.ToolCalls, policy, tracker, gate)
		history = updated

		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, err
		}
		if clarification != nil {
			return domain.AgentResponse{Clarification: clarification}, nil
		}
		if resourceBlock != nil {
			return domain.AgentResponse{ResourceBlock: resourceBlock}, nil
		}
		if stuck || deadEnd {
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{Stuck: stuck, DeadEnd: deadEnd})
		}

		if l.runTokenCap > 0 && totalTokens >= l.runTokenCap {
			log.Warn().
				Int("total_tokens", totalTokens).
				Int("cap", l.runTokenCap).
				Int("iteration", i+1).
				Msg("agent loop stopped: run token budget exhausted")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("token_budget_exhausted", map[string]int{"total_tokens": totalTokens, "cap": l.runTokenCap, "iteration": i + 1})
			}
			return l.giveUp(ctx, history, head, tracker.snapshot(i+1), budget, giveUpCause{TokenExhausted: true, TokensUsed: totalTokens})
		}
	}

	return l.giveUp(ctx, history, head, tracker.snapshot(budget), budget, giveUpCause{})
}

// runTokens is what one call costs the run's token cap: what the provider
// processed in full, plus cache reads at their billed weight.
func runTokens(u domain.Usage) int {
	cacheRead := min(max(u.CacheReadTokens, 0), max(u.PromptTokens, 0))
	return u.PromptTokens - cacheRead + int(float64(cacheRead)*cacheReadTokenWeight) + u.CompletionTokens
}

// truncatedToolCalls: a turn cut off by the output cap ends inside its last
// tool call, so its arguments are incomplete — a half-written file, a command
// missing its tail. Running them does damage the next turn has to undo.
func truncatedToolCalls(resp domain.AgentResponse) bool {
	return resp.StopReason == domain.StopReasonMaxTokens && len(resp.Message.ToolCalls) > 0
}

// raisedOutputCap is the larger cap a truncated turn is retried with: double,
// within the model's ceiling and what the window has left after the prompt.
// Zero means there is no more room to give.
func raisedOutputCap(current, maxOutput, window, promptTokens int) int {
	if current <= 0 || maxOutput <= current {
		return 0
	}
	next := min(current*2, maxOutput)
	if window > 0 && promptTokens > 0 {
		next = min(next, window-promptTokens)
	}
	if next <= current {
		return 0
	}
	return next
}

// transcriptOf leaves out the loop's own notes after the head: they speak to
// this run's turn and token budget, and a follow-up continuing the transcript
// runs on a fresh one.
func transcriptOf(history []domain.Message, final domain.Message, headLen int, notes loopNotes) []domain.Message {
	out := make([]domain.Message, 0, len(history)+1)
	for i, m := range history {
		if i >= headLen && notes.holds(m) {
			continue
		}
		out = append(out, m)
	}
	return append(out, final)
}

// loopNotes are the system messages the loop injects itself (turn and token
// warnings, the truncation and empty-turn nudges), told apart from the
// caller's by content.
type loopNotes map[string]struct{}

func (n loopNotes) add(content string) domain.Message {
	n[content] = struct{}{}
	return domain.Message{Role: domain.RoleSystem, Content: content}
}

func (n loopNotes) holds(m domain.Message) bool {
	if m.Role != domain.RoleSystem {
		return false
	}
	_, ok := n[m.Content]
	return ok
}

func guardHostExecuted(provider domain.LLMProviderType) error {
	if domain.RequiresHostExecutor(provider) {
		return domain.ErrHostExecutedProvider(provider)
	}
	return nil
}

func (l *Loop) runToolCalls(
	ctx context.Context,
	history []domain.Message,
	calls []domain.ToolCall,
	policy domain.ToolPolicy,
	tracker *callTracker,
	gate *clarificationGate,
) ([]domain.Message, *domain.ClarificationRequest, *domain.ResourceBlock, bool, bool) {
	stuck := false
	deadEnd := false
	prefetched := map[int]domain.ToolResult{}

	for idx, tc := range calls {
		if ctx.Err() != nil {
			return history, nil, nil, stuck, deadEnd
		}

		if _, done := prefetched[idx]; !done && parallelSafeTools[tc.Function.Name] {
			for i, result := range l.prefetchReadOnlyRun(ctx, calls, idx, policy, tracker.lastKey, prefetched) {
				prefetched[i] = result
			}
		}

		if tracker.canSkip(tc.Function.Name, tc.Function.Arguments) {
			out := tracker.observeSkipped(tc.Function.Name, tc.Function.Arguments)
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("repeats", out.Repeats+1).
				Msg("loop guard: identical back-to-back call answered without executing")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("tool_call_skipped", map[string]any{
					"tool": tc.Function.Name, "call_id": tc.ID,
					"arguments": tc.Function.Arguments, "repeats": out.Repeats + 1,
				})
			}
			history = append(history, domain.Message{
				Role:       domain.RoleTool,
				Content:    repeatNudgeMessage(tc.Function.Name, out.Repeats),
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
			if out.Repeats >= repeatAbortThreshold {
				stuck = true
			}
			continue
		}

		result, ok := prefetched[idx]
		if !ok {
			log.Debug().
				Str("tool", tc.Function.Name).
				Str("call_id", tc.ID).
				Msg("executing tool call")

			recordToolCallStart(ctx, tc)
			result = l.registry.ExecuteWithPolicy(ctx, tc, policy)
		}

		if result.Clarification != nil {

			if gate.refuse(ctx) {
				log.Warn().Str("tool", tc.Function.Name).Msg("clarification refused: run has not read the repository yet")
				if rec := activity.FromContext(ctx); rec != nil {
					rec.Step("clarification_refused", map[string]string{
						"reason": "no_code_exploration", "context": result.Clarification.Context,
					})
				}
				history = append(history, domain.Message{
					Role:       domain.RoleTool,
					Content:    groundClarificationNote,
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
				})
				continue
			}
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("clarification_requested", domain.ClarificationStepPayload(*result.Clarification, "ask_user"))
			}
			reqCopy := *result.Clarification
			return history, &reqCopy, nil, false, false
		}

		if result.ResourceBlock != nil {
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("resource_blocked", map[string]string{
					"tool": tc.Function.Name, "resource": result.ResourceBlock.Resource,
					"detail": result.ResourceBlock.Detail,
				})
			}
			blockCopy := *result.ResourceBlock
			return history, nil, &blockCopy, false, false
		}

		out := tracker.observe(tc.Function.Name, tc.Function.Arguments, result.Content, result.IsError)

		// Archived unconditionally (archiveImages itself is a no-op without
		// images or an archiver) so the ids below do not depend on whether
		// this run happens to carry an activity recorder.
		imageIDs := l.archiveImages(ctx, result.Name, result.Images)

		if rec := activity.FromContext(ctx); rec != nil {
			preview := result.Content
			if len(preview) > 500 {
				preview = domain.TruncateHead(preview, 500)
			}

			rec.Step("tool_call_result", map[string]any{
				"tool": result.Name, "call_id": result.ToolCallID,
				"image_attachment_ids": imageIDs,
				"content":              preview, "is_error": result.IsError, "repeats": out.Repeats,
				"error_streak": out.ErrStreak, "tool_errors": out.ToolErrors,
				"executions": out.Execs,
			})
		}

		content := truncateToolOutput(result.Content, l.maxToolOutputChars)
		images := result.Images

		if strings.TrimSpace(content) == "" && len(images) == 0 {
			content = emptyResultNote(tc.Function.Name)
		}
		// The archive ids are otherwise visible only in the activity log, not
		// to the model — so evidence (record_test_cases, review notes) could
		// never cite a real attachment id, only describe the image from memory.
		if len(imageIDs) > 0 {
			content += attachmentIDsNote(imageIDs)
		}
		switch {
		case out.Repeats >= repeatNoteThreshold:
			content = repeatNudgeMessage(tc.Function.Name, out.Repeats)

			images = nil
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("repeats", out.Repeats+1).
				Msg("loop guard: identical tool call and result repeated")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_repeat", map[string]any{
					"tool": tc.Function.Name, "repeats": out.Repeats + 1, "arguments": tc.Function.Arguments,
				})
			}
		case out.ErrStreak >= errStreakNoteThreshold:

			content = errorStreakMessage(out.ErrStreak) + content
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("error_streak", out.ErrStreak).
				Msg("loop guard: consecutive tool failures")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_error_streak", map[string]any{
					"tool": tc.Function.Name, "error_streak": out.ErrStreak,
				})
			}
		}

		if out.Repeats == 0 && out.Execs >= sameCallNoteThreshold {
			content += sameCallMessage(tc.Function.Name, out.Execs)
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("executions", out.Execs).
				Msg("loop guard: identical call executed repeatedly with a changing result")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_same_call", map[string]any{
					"tool": tc.Function.Name, "executions": out.Execs, "arguments": tc.Function.Arguments,
				})
			}
		}

		if result.IsError && out.ToolErrors >= toolErrorNoteThreshold {
			content += toolErrorMessage(tc.Function.Name, out.ToolErrors)
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("loop_guard_tool_errors", map[string]any{
					"tool": tc.Function.Name, "tool_errors": out.ToolErrors,
				})
			}
		}

		if out.Repeats >= repeatAbortThreshold {
			stuck = true
		}

		if out.Execs >= sameCallAbortThreshold {
			log.Warn().
				Str("tool", tc.Function.Name).
				Int("executions", out.Execs).
				Msg("loop guard: identical call executed past the abort threshold")
			stuck = true
		}
		if out.ErrStreak >= errStreakAbortThreshold {
			deadEnd = true
		}

		history = append(history, domain.Message{
			Role:       domain.RoleTool,
			Content:    content,
			ToolCallID: result.ToolCallID,
			Name:       result.Name,
			Images:     images,
		})
	}

	return history, nil, nil, stuck, deadEnd
}

func recordToolCallStart(ctx context.Context, tc domain.ToolCall) {
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("tool_call_start", map[string]string{
			"tool": tc.Function.Name, "call_id": tc.ID, "arguments": tc.Function.Arguments,
		})
	}
}

// prefetchReadOnlyRun executes, side by side, the parallel-safe calls in the
// unbroken run of them that starts at from; the caller still walks every call
// in order, so results land in the history exactly where they were asked for.
// A call identical to the one before it is left out: the loop guard may
// answer it without running it, and only the in-order walk can tell. So is a
// call an earlier batch already ran: a left-out duplicate starts a new batch
// over the same run, which must not execute its calls a second time.
func (l *Loop) prefetchReadOnlyRun(ctx context.Context, calls []domain.ToolCall, from int, policy domain.ToolPolicy, lastKey string, done map[int]domain.ToolResult) map[int]domain.ToolResult {
	var batch []int
	prev := lastKey
	for i := from; i < len(calls) && parallelSafeTools[calls[i].Function.Name]; i++ {
		key := callKey(calls[i].Function.Name, calls[i].Function.Arguments)
		if _, ran := done[i]; !ran && key != prev {
			batch = append(batch, i)
		}
		prev = key
	}
	if len(batch) < 2 {
		return nil
	}

	for _, i := range batch {
		recordToolCallStart(ctx, calls[i])
	}
	results := make([]domain.ToolResult, len(batch))
	var g errgroup.Group
	g.SetLimit(maxParallelToolCalls)
	for n, i := range batch {
		g.Go(func() error {
			results[n] = l.registry.ExecuteWithPolicy(ctx, calls[i], policy)
			return nil
		})
	}
	_ = g.Wait()

	log.Debug().Int("calls", len(batch)).Msg("executed read-only tool calls in parallel")
	out := make(map[int]domain.ToolResult, len(batch))
	for n, i := range batch {
		out[i] = results[n]
	}
	return out
}

func (l *Loop) giveUp(
	ctx context.Context,
	history []domain.Message,
	head runHead,
	stats RunStats,
	budget int,
	cause giveUpCause,
) (domain.AgentResponse, error) {
	partial := l.wrapUp(ctx, history, head)

	err := &BudgetExhaustedError{
		Budget: budget, Stats: stats, Partial: partial,
		Stuck: cause.Stuck, DeadEnd: cause.DeadEnd,
		TokenExhausted: cause.TokenExhausted, TokensUsed: cause.TokensUsed,
	}

	log.Warn().
		Int("budget", budget).
		Int("iterations", stats.Iterations).
		Int("tool_calls", stats.ToolCalls).
		Int("repeated_no_progress", stats.RepeatedNoProgress).
		Int("tool_errors", stats.ToolErrors).
		Int("max_error_streak", stats.MaxErrorStreak).
		Bool("stuck", cause.Stuck).
		Bool("dead_end", cause.DeadEnd).
		Bool("token_exhausted", cause.TokenExhausted).
		Int("tokens_used", cause.TokensUsed).
		Str("stats", stats.Summary()).
		Msg("agent loop out of budget")

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("budget_exhausted", map[string]any{
			"budget": budget, "stuck": cause.Stuck, "dead_end": cause.DeadEnd,
			"token_exhausted": cause.TokenExhausted, "tokens_used": cause.TokensUsed,
			"stats": stats.Summary(), "summary": partial,
		})
	}

	return domain.AgentResponse{}, err
}

func effectiveBudget(base appcontext.Budget, toolTokens int, ratio float64) appcontext.Budget {
	if base.MaxTokens <= 0 {
		return base
	}
	if ratio <= 0 {
		ratio = 1
	}
	shrunk := max(int(float64(base.MaxTokens-toolTokens)/ratio), minShrinkTokens)
	adjusted := base
	adjusted.MaxTokens = min(base.MaxTokens, shrunk)
	return adjusted
}

func (l *Loop) fitHistory(ctx context.Context, history []domain.Message, head runHead, budget appcontext.Budget) []domain.Message {
	if budget.MaxTokens <= 0 {
		return history
	}
	if appcontext.CountTokens(history) <= budget.TokenLimit() {
		return history
	}
	if summarized, ok := l.summarizeHistory(ctx, history, head, budget); ok {
		history = summarized
		if appcontext.CountTokens(history) <= budget.TokenLimit() {
			return history
		}
	}
	trimmed := budget.Apply(history)
	dropped := len(history) - len(trimmed)
	if dropped <= 0 {
		return history
	}
	log.Info().
		Int("dropped_messages", dropped).
		Int("kept_messages", len(trimmed)).
		Int("tokens", appcontext.CountTokens(trimmed)).
		Msg("agent loop trimmed history to the context budget")
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("history_trimmed", map[string]int{
			"dropped_messages": dropped,
			"kept_messages":    len(trimmed),
			"kept_tokens":      appcontext.CountTokens(trimmed),
		})
	}
	return trimmed
}

func (l *Loop) summarizeHistory(ctx context.Context, history []domain.Message, head runHead, budget appcontext.Budget) ([]domain.Message, bool) {
	if l.summarizer == nil {
		return nil, false
	}
	summarized, ok, err := appcontext.StableTrim(ctx, budget, l.summarizer, history, appcontext.StableTrimOptions{
		HeadLen:  head.len,
		Model:    head.lightModel,
		Provider: head.provider,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("provider", string(head.provider)).
			Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
			Msg("agent loop history summary skipped; falling back to dropping the oldest messages")
		return nil, false
	}
	if !ok {
		return nil, false
	}
	dropped := len(history) - len(summarized)
	log.Info().
		Int("dropped_messages", dropped).
		Int("kept_messages", len(summarized)).
		Int("tokens", appcontext.CountTokens(summarized)).
		Msg("agent loop summarised the middle of its history")
	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("history_summarized", map[string]int{
			"dropped_messages": dropped,
			"kept_messages":    len(summarized),
			"kept_tokens":      appcontext.CountTokens(summarized),
		})
	}
	return summarized, true
}

func (l *Loop) shrinkHistory(messages []domain.Message) ([]domain.Message, bool) {
	budget := l.historyBudget
	budget.ReserveOutput = 0
	budget.MaxTokens = max(appcontext.CountTokens(messages)/2, minShrinkTokens)
	if budget.KeepRecentMessages <= 0 {
		budget.KeepRecentMessages = 6
	}
	trimmed := budget.Apply(messages)
	return trimmed, len(trimmed) < len(messages)
}

func (l *Loop) chatWithRetry(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, []domain.Message, error) {
	return l.sendWithRetry(ctx, req, l.llm.Chat)
}

func (l *Loop) chatStreamWithRetry(ctx context.Context, req domain.AgentRequest, onToken func(string)) (domain.AgentResponse, []domain.Message, error) {
	return l.sendWithRetry(ctx, req, func(ctx context.Context, req domain.AgentRequest) (domain.AgentResponse, error) {
		return l.llm.ChatStream(ctx, req, onToken)
	})
}

func (l *Loop) sendWithRetry(
	ctx context.Context,
	req domain.AgentRequest,
	send func(context.Context, domain.AgentRequest) (domain.AgentResponse, error),
) (domain.AgentResponse, []domain.Message, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {

		if err := ctx.Err(); err != nil {
			return domain.AgentResponse{}, req.Messages, err
		}

		resp, err := send(ctx, req)
		if err == nil {
			return resp, req.Messages, nil
		}
		lastErr = err

		action := llmretry.Classify(ctx, err)
		log.Warn().Err(err).
			Int("attempt", attempt+1).
			Str("action", action.String()).
			Msg("llm chat attempt failed")

		if attempt == maxRetries {
			break
		}

		switch action {
		case llmretry.Stop:
			return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
		case llmretry.Shrink:
			shrunk, ok := l.shrinkHistory(req.Messages)
			if !ok {
				return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
			}
			log.Warn().
				Int("dropped_messages", len(req.Messages)-len(shrunk)).
				Msg("provider refused the request as too long; retrying it smaller")
			req.Messages = shrunk
		case llmretry.Backoff:
			if sleepErr := llmretry.Wait(ctx, err, attempt); sleepErr != nil {
				return domain.AgentResponse{}, req.Messages, retryExhausted(attempt+1, lastErr)
			}
		}
	}

	return domain.AgentResponse{}, req.Messages, retryExhausted(maxRetries+1, lastErr)
}

func retryExhausted(attempts int, err error) error {
	if attempts == 1 {
		return fmt.Errorf("llm failed after 1 attempt: %w", err)
	}
	return fmt.Errorf("llm failed after %d attempts: %w", attempts, err)
}

func (l *Loop) wrapUp(ctx context.Context, history []domain.Message, head runHead) string {
	messages := make([]domain.Message, len(history), len(history)+1)
	copy(messages, history)
	messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: wrapUpPrompt})

	messages = l.fitHistory(ctx, messages, head, head.wrapBudget)

	resp, _, err := l.chatWithRetry(ctx, domain.AgentRequest{
		Messages:     messages,
		ProviderType: head.provider,

		Model:            head.lightModel,
		CacheAnchorIndex: head.anchor(len(messages)),
		MaxTokens:        head.wrapBudget.ReserveOutput,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("provider", string(head.provider)).
			Bool("permanent", errors.Is(err, domain.ErrHostExecutedUnservable)).
			Msg("agent loop wrap-up summary skipped; returning the run's own last message")
		return ""
	}
	return strings.TrimSpace(resp.Message.Content)
}

func truncateToolOutput(content string, maxChars int) string {
	if maxChars <= 0 || len(content) <= maxChars {
		return content
	}
	head := domain.TruncateHead(content, maxChars/4)
	tail := domain.TruncateTail(content, maxChars-len(head))
	return head + toolOutputTruncateSuffix + "\n" + tail
}

func toolCallPayloads(calls []domain.ToolCall) []map[string]string {
	payloads := make([]map[string]string, 0, len(calls))
	for _, tc := range calls {
		payloads = append(payloads, map[string]string{
			"id": tc.ID, "name": tc.Function.Name, "arguments": tc.Function.Arguments,
		})
	}
	return payloads
}

const previewLimit = 300

// buildLLMRequestPayload previews only the messages added since the previous
// request: each request resends the whole history, so previewing all of it on
// every step stored the run quadratically. The first request's fresh set is
// the opening context; message_count is still the full request's size.
func buildLLMRequestPayload(model string, total int, fresh []domain.Message, toolCount int) map[string]any {
	type callPreview struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type msgPreview struct {
		Role      string        `json:"role"`
		Content   string        `json:"content"`
		ToolCalls []callPreview `json:"tool_calls,omitempty"`
	}
	previews := make([]msgPreview, 0, len(fresh))
	for _, m := range fresh {
		content := domain.TruncateHead(m.Content, previewLimit)
		if len(content) < len(m.Content) {
			content += "…"
		}
		if m.Role == domain.RoleSystem && content == "" {
			continue
		}
		role := string(m.Role)
		if m.Role == domain.RoleTool {
			role = "tool:" + m.Name
		}
		calls := make([]callPreview, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			args := domain.TruncateHead(tc.Function.Arguments, previewLimit)
			if len(args) < len(tc.Function.Arguments) {
				args += "…"
			}
			calls = append(calls, callPreview{Name: tc.Function.Name, Arguments: args})
		}
		if len(calls) == 0 {
			calls = nil
		}
		previews = append(previews, msgPreview{Role: role, Content: content, ToolCalls: calls})
	}
	return map[string]any{
		"model":             model,
		"message_count":     total,
		"new_message_count": len(fresh),
		"tool_count":        toolCount,
		"messages":          previews,
	}
}
