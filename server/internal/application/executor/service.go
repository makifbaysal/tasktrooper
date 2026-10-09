package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxRunIDLength = 128

const processGrace = 3 * time.Second

var (
	errCancelRequested = errors.New("the run was cancelled by its caller")
	errCallerGone      = errors.New("the caller stopped reading the run's stream")
	errShuttingDown    = errors.New("the executor is shutting down")
	errRunTimeout      = errors.New("the run reached its timeout")
)

// Provider is one configured model provider: the wire format its client
// speaks, and the model a request that names none runs on.
type Provider struct {
	Type         domain.LLMProviderType
	DefaultModel string
}

type Limits struct {
	MaxIterations      int
	TaskMaxIterations  int
	MaxToolOutputChars int
	RunTokenCap        int
	History            appcontext.Budget
}

type Deps struct {
	// LLM routes a request by its ProviderType, which here is the provider's
	// configured id rather than its wire type.
	LLM       port.LLMClient
	Providers map[string]Provider
	// WorkspaceRoot is where every run's workspace lives; a run names its own
	// relative to it.
	WorkspaceRoot string
	// WorkspaceTools need a run workspace (files, terminal, search); HostTools
	// run without one (web, browser).
	WorkspaceTools []port.ToolExecutor
	HostTools      []port.ToolExecutor
	Remote         port.RemoteToolConnector
	// Index is this computer's code index; nil answers index calls not_ready
	// and runs that name an index run without it.
	Index port.LocalCodeIndex
	// Embeddings moves the index's embedding engine; nil answers not_ready.
	Embeddings port.EmbeddingsEndpoint
	// Git answers the post-run routes (git.*, commit_push); nil answers
	// not_ready.
	Git    port.CheckoutGit
	Limits Limits
}

type Service struct {
	deps Deps

	// life outlives every run and ends at Shutdown: an index pass a run
	// stopped waiting for goes on under it.
	life    context.Context
	endLife context.CancelCauseFunc

	mu       sync.Mutex
	runs     map[string]*PreparedRun
	ensures  map[string]*PreparedEnsure
	verifies map[string]*PreparedVerify
	closing  bool
}

func NewService(deps Deps) *Service {
	life, endLife := context.WithCancelCause(context.Background())
	return &Service{
		deps: deps, life: life, endLife: endLife,
		runs: make(map[string]*PreparedRun), ensures: make(map[string]*PreparedEnsure),
		verifies: make(map[string]*PreparedVerify),
	}
}

// PreparedRun is a validated run holding its run id. Execute runs it; Release
// gives the id back for a run the caller abandoned before executing.
type PreparedRun struct {
	svc      *Service
	spec     AgentRun
	model    string
	workDir  string
	env      []string
	messages []domain.Message
	ctx      context.Context
	cancel   context.CancelCauseFunc
	once     sync.Once
}

func badRequest(format string, args ...any) *Failure {
	return &Failure{Code: CodeBadRequest, Message: fmt.Sprintf(format, args...)}
}

// Prepare validates run and claims its run id; ctx bounds the run's whole
// life, and Cancel or Shutdown end it early.
func (s *Service) Prepare(ctx context.Context, run AgentRun) (*PreparedRun, *Failure) {
	run.RunID = strings.TrimSpace(run.RunID)
	if run.RunID == "" {
		return nil, badRequest("run_id is required")
	}
	if len(run.RunID) > maxRunIDLength {
		return nil, badRequest("run_id is longer than %d characters", maxRunIDLength)
	}
	switch run.Kind {
	case "":
		run.Kind = KindBoard
	case KindBoard, KindChat:
	default:
		return nil, badRequest("kind %q is neither %q nor %q", run.Kind, KindBoard, KindChat)
	}
	if run.TimeoutMS < 0 {
		return nil, badRequest("timeout_ms cannot be negative")
	}
	_, model, failure := s.resolveModel(run.Agent.ProviderID, run.Agent.Model)
	if failure != nil {
		return nil, failure
	}
	messages, failure := runMessages(run)
	if failure != nil {
		return nil, failure
	}
	workDir, failure := s.resolveWorkspace(run.Workspace)
	if failure != nil {
		return nil, failure
	}
	if run.MCP != nil && strings.TrimSpace(run.MCP.URL) == "" {
		run.MCP = nil
	}
	if run.Index != nil {
		run.Index.RepoKey = strings.TrimSpace(run.Index.RepoKey)
		run.Index.Branch = strings.TrimSpace(run.Index.Branch)
		if run.Index.RepoKey == "" {
			return nil, badRequest("index.repo_key is required when a run names an index")
		}
		if run.Index.WaitMS < 0 {
			return nil, badRequest("index.wait_ms cannot be negative")
		}
	}
	env, failure := runEnv(run.Env)
	if failure != nil {
		return nil, failure
	}

	runCtx, cancel := context.WithCancelCause(ctx)
	prepared := &PreparedRun{
		svc: s, spec: run, model: model, workDir: workDir, env: env,
		messages: messages, ctx: runCtx, cancel: cancel,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		cancel(nil)
		return nil, &Failure{Code: CodeCancelled, Message: errShuttingDown.Error()}
	}
	if _, busy := s.runs[run.RunID]; busy {
		cancel(nil)
		return nil, &Failure{Code: CodeConflict, Message: fmt.Sprintf("run %q is already running", run.RunID)}
	}
	s.runs[run.RunID] = prepared
	return prepared, nil
}

func (s *Service) resolveModel(providerID, model string) (Provider, string, *Failure) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return Provider{}, "", badRequest("provider_id is required")
	}
	provider, ok := s.deps.Providers[providerID]
	if !ok {
		return Provider{}, "", badRequest("provider %q is not configured on this computer", providerID)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = provider.DefaultModel
	}
	if model == "" {
		return Provider{}, "", badRequest("no model was named and provider %q has none configured", providerID)
	}
	return provider, model, nil
}

func validRole(role domain.Role) bool {
	switch role {
	case domain.RoleSystem, domain.RoleUser, domain.RoleAssistant, domain.RoleTool:
		return true
	}
	return false
}

func runMessages(run AgentRun) ([]domain.Message, *Failure) {
	messages := make([]domain.Message, 0, len(run.Messages)+2)
	if system := strings.TrimSpace(run.Agent.SystemPrompt); system != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: run.Agent.SystemPrompt})
	}
	for i, m := range run.Messages {
		if !validRole(m.Role) {
			return nil, badRequest("messages[%d] has role %q", i, m.Role)
		}
		messages = append(messages, m)
	}
	if strings.TrimSpace(run.Prompt) != "" {
		messages = append(messages, domain.Message{Role: domain.RoleUser, Content: run.Prompt})
	}
	if len(run.Messages) == 0 && strings.TrimSpace(run.Prompt) == "" {
		return nil, badRequest("a run needs a prompt or messages")
	}
	return messages, nil
}

func (s *Service) resolveWorkspace(rel string) (string, *Failure) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", nil
	}
	dir, err := workspace.ResolveWithinRoot(s.deps.WorkspaceRoot, rel)
	if err != nil {
		return "", badRequest("workspace %q: %v", rel, err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", badRequest("workspace %q does not exist under the workspace root; prepare it first", rel)
	}
	return dir, nil
}

// Cancel reports whether runID was running. A run that already finished is
// not an error: the cancel and the end of the run race as a matter of course.
func (s *Service) Cancel(runID string) bool {
	s.mu.Lock()
	run, ok := s.runs[strings.TrimSpace(runID)]
	s.mu.Unlock()
	if ok {
		run.cancel(errCancelRequested)
	}
	return ok
}

// Shutdown cancels every run; each still answers its caller with a terminal
// frame before its stream closes.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closing = true
	runs := make([]*PreparedRun, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	ensures := make([]*PreparedEnsure, 0, len(s.ensures))
	for _, ensure := range s.ensures {
		ensures = append(ensures, ensure)
	}
	verifies := make([]*PreparedVerify, 0, len(s.verifies))
	for _, verify := range s.verifies {
		verifies = append(verifies, verify)
	}
	s.mu.Unlock()
	for _, run := range runs {
		run.cancel(errShuttingDown)
	}
	for _, ensure := range ensures {
		ensure.cancel(errShuttingDown)
	}
	for _, verify := range verifies {
		verify.cancel(errShuttingDown)
	}
	s.endLife(errShuttingDown)
}

func (s *Service) ActiveRuns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runs)
}

func (r *PreparedRun) RunID() string { return r.spec.RunID }

// Release gives the run id back without executing; Execute calls it itself.
func (r *PreparedRun) Release() {
	r.once.Do(func() {
		r.cancel(nil)
		r.svc.mu.Lock()
		if r.svc.runs[r.spec.RunID] == r {
			delete(r.svc.runs, r.spec.RunID)
		}
		r.svc.mu.Unlock()
	})
}

func (r *PreparedRun) Execute(sink Sink) (*RunResult, *Failure) {
	defer r.Release()
	started := time.Now()
	ctx := r.ctx
	if r.spec.TimeoutMS > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeoutCause(ctx, time.Duration(r.spec.TimeoutMS)*time.Millisecond, errRunTimeout)
		defer stop()
	}
	em := newEmitter(sink, func() { r.cancel(errCallerGone) })
	meter := &usageMeter{}
	ctx, toolUsage := registry.ContextWithToolUsage(ctx)
	summary := func() *RunSummary { return runSummary(meter, toolUsage, started) }

	var remote []port.ToolExecutor
	if r.spec.MCP != nil {
		if r.svc.deps.Remote == nil {
			return nil, &Failure{Code: CodeInternal, Message: "this executor has no client for coordination endpoints", Run: summary()}
		}
		tools, closeSession, err := r.svc.deps.Remote.Connect(ctx, port.RemoteToolEndpoint{
			URL: r.spec.MCP.URL, Token: r.spec.MCP.Token, ServerName: r.spec.MCP.ServerName,
		})
		if err != nil {
			if failure := r.interrupted(ctx, summary()); failure != nil {
				return nil, failure
			}
			return nil, &Failure{Code: CodeUpstream, Message: "the coordination tools could not be reached: " + err.Error(), Run: summary()}
		}
		defer closeSession()
		remote = tools
	}

	llm := &meteredClient{inner: r.svc.deps.LLM, meter: meter, em: em}

	ctx = registry.ContextWithWorkspaceDir(ctx, r.workDir)
	ctx = r.withRunEnv(ctx)
	scope := "exec:" + r.spec.RunID
	ctx = proctree.WithScope(ctx, scope)
	defer proctree.Default.KillScope(scope, processGrace)
	ctx, _, _ = activity.StartRun(ctx, stepStore{em: em}, nil, r.spec.RunID, r.model)

	r.ensureIndex(ctx, em)
	index := r.attachIndex(ctx, llm)
	loop := r.loop(llm, r.registry(remote, index, em))
	ctx = indexContext(ctx, index)
	messages := r.withIndexContext(ctx, index, r.messages)

	providerRef := domain.LLMProviderType(r.spec.Agent.ProviderID)
	opts := []agent.RunOption{agent.WithSessionLimits(r.spec.Agent.MaxTurns, r.spec.Agent.Effort)}
	var resp domain.AgentResponse
	var err error
	if r.spec.Kind == KindChat {
		ctx = agent.WithSegmentBreak(ctx, em.segmentBreak)
		resp, err = loop.RunStream(ctx, messages, r.model, providerRef, r.spec.Agent.ToolPolicy, em.textDelta, opts...)
	} else {
		resp, err = loop.RunTask(ctx, messages, r.model, providerRef, r.spec.Agent.ToolPolicy, opts...)
	}
	if err != nil {
		return nil, r.failureFor(ctx, err, summary())
	}
	return &RunResult{
		FinalText:     resp.Message.Content,
		RunSummary:    *summary(),
		StopReason:    resp.StopReason,
		Clarification: resp.Clarification,
		ResourceBlock: resp.ResourceBlock,
	}, nil
}

func (r *PreparedRun) loop(llm port.LLMClient, tools port.ToolRegistry) *agent.Loop {
	limits := r.svc.deps.Limits
	maxIterations := limits.MaxIterations
	if r.spec.Agent.MaxTurns > 0 {
		// A streamed turn takes no RunOption turn cap, so the run's own loop
		// carries it.
		maxIterations = r.spec.Agent.MaxTurns
	}
	loop := agent.NewLoop(llm, tools, maxIterations, limits.TaskMaxIterations, limits.MaxToolOutputChars)
	loop.SetHistoryBudget(limits.History)
	loop.SetModelLimits(r.svc.limitsFor)
	loop.SetSummarizer(appcontext.NewLLMSummarizer(llm))
	loop.SetRunTokenCap(limits.RunTokenCap)
	return loop
}

// limitsFor resolves a provider id to its wire type: the per-model limits
// table keys some defaults on the type, and a run names its provider by id.
func (s *Service) limitsFor(ref domain.LLMProviderType, model string) appcontext.ModelLimits {
	if provider, ok := s.deps.Providers[string(ref)]; ok {
		return appcontext.LimitsFor(provider.Type, model)
	}
	return appcontext.LimitsFor(ref, model)
}

func (r *PreparedRun) registry(remote []port.ToolExecutor, index *port.LocalIndexAttachment, em *emitter) port.ToolRegistry {
	inner := registry.New()
	sources := make(map[string]string)
	for _, tool := range remote {
		if remoteWithheld(tool.Name()) {
			continue
		}
		inner.Register(tool)
		sources[tool.Name()] = SourceRemote
	}
	local := append([]port.ToolExecutor(nil), r.svc.deps.HostTools...)
	if r.workDir != "" {
		local = append(local, r.svc.deps.WorkspaceTools...)
	}
	// Last, so the index-backed get_symbol_skeleton replaces the by-file one.
	if index != nil {
		local = append(local, index.Tools...)
	}
	for _, tool := range local {
		inner.Register(tool)
		sources[tool.Name()] = SourceLocal
	}
	return &eventingRegistry{
		ToolRegistry: registry.NewWorkspaceRegistry(inner, nil),
		sources:      sources,
		em:           em,
	}
}

// remoteWithheldTools act on the coordination server's own disk, processes or
// index. Served from there they would build, preview, commit or search a copy
// of the work that is not the one this run is editing; where this computer has
// its own version, it is registered locally instead. The index-backed code
// tools are this computer's only when the run names a local index; a run
// without one goes without them rather than search the server's.
var remoteWithheldTools = []string{
	"download_file",
	"start_task_preview",
	"commit_task_changes",
	domain.DeployReleaseToolName,
	"codebase_search",
	"expand_symbol_context",
	"get_symbol_skeleton",
	"browser_*",
	"mcp_filesystem_*",
}

func remoteWithheld(name string) bool {
	for _, pattern := range remoteWithheldTools {
		if prefix, wildcard := strings.CutSuffix(pattern, "*"); wildcard {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		} else if name == pattern {
			return true
		}
	}
	return false
}

func runSummary(meter *usageMeter, usage *registry.ToolUsage, started time.Time) *RunSummary {
	counts := usage.Snapshot()
	names := make([]string, 0, len(counts))
	for name, n := range counts {
		if n > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	failures := usage.Failures()
	if len(failures) == 0 {
		failures = nil
	}
	if len(counts) == 0 {
		counts = nil
	}
	return &RunSummary{
		Usage:      meter.snapshot(),
		ToolUsage:  names,
		ToolCounts: counts,
		ToolErrors: failures,
		DurationMS: time.Since(started).Milliseconds(),
	}
}

// interrupted names why ctx ended, or nil while it is live. A run stopped by
// its caller, a timeout or shutdown usually surfaces as some incidental error
// from wherever it was; reporting that instead would make every cancellation
// look like a different fault.
func (r *PreparedRun) interrupted(ctx context.Context, summary *RunSummary) *Failure {
	if ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errRunTimeout):
		return &Failure{Code: CodeTimeout, Message: fmt.Sprintf("the run reached its timeout of %dms", r.spec.TimeoutMS), Run: summary}
	case errors.Is(cause, errCancelRequested), errors.Is(cause, errShuttingDown), errors.Is(cause, errCallerGone):
		return &Failure{Code: CodeCancelled, Message: cause.Error(), Run: summary}
	default:
		return &Failure{Code: CodeCancelled, Message: "the caller went away", Run: summary}
	}
}

func (r *PreparedRun) failureFor(ctx context.Context, err error, summary *RunSummary) *Failure {
	if failure := r.interrupted(ctx, summary); failure != nil {
		return failure
	}
	var budget *agent.BudgetExhaustedError
	if errors.As(err, &budget) {
		return &Failure{Code: CodeBudgetExhausted, Message: budget.Error(), Run: summary, Partial: budget.Partial}
	}
	failure := classifyLLMError(err, CodeInternal)
	failure.Run = summary
	if failure.Code == CodeInternal {
		log.Warn().Err(err).Str("run_id", r.spec.RunID).Msg("executor run failed")
	}
	return failure
}

// classifyLLMError names a failed model call; fallback is the code for an
// error that says nothing about where it came from. A one-shot call has only
// the provider to blame, a run also has its own code.
func classifyLLMError(err error, fallback string) *Failure {
	if rl, ok := domain.RateLimitOf(err); ok {
		return &Failure{Code: CodeRateLimited, Message: rl.UserMessage(), RetryAfterMS: rl.RetryAfter.Milliseconds()}
	}
	var chatFailed *agent.ChatFailedError
	var httpErr *domain.LLMHTTPError
	if errors.As(err, &chatFailed) || errors.As(err, &httpErr) {
		return &Failure{Code: CodeUpstream, Message: err.Error()}
	}
	if errors.Is(err, domain.ErrHostExecutedUnservable) {
		return &Failure{Code: CodeBadRequest, Message: err.Error()}
	}
	return &Failure{Code: fallback, Message: err.Error()}
}

func (s *Service) Complete(ctx context.Context, req CompletionRequest) (*Completion, *Failure) {
	_, model, failure := s.resolveModel(req.ProviderID, req.Model)
	if failure != nil {
		return nil, failure
	}
	if req.TimeoutMS < 0 {
		return nil, badRequest("timeout_ms cannot be negative")
	}
	messages := make([]domain.Message, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: req.System})
	}
	for i, m := range req.Messages {
		if m.Role == domain.RoleTool || !validRole(m.Role) {
			return nil, badRequest("messages[%d] has role %q", i, m.Role)
		}
		messages = append(messages, m)
	}
	if len(req.Messages) == 0 {
		return nil, badRequest("messages is required")
	}
	if req.TimeoutMS > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeoutCause(ctx, time.Duration(req.TimeoutMS)*time.Millisecond, errRunTimeout)
		defer stop()
	}
	resp, err := s.deps.LLM.Chat(ctx, domain.AgentRequest{
		Messages:       messages,
		ProviderType:   domain.LLMProviderType(strings.TrimSpace(req.ProviderID)),
		Model:          model,
		MaxTokens:      req.MaxTokens,
		ResponseFormat: req.ResponseFormat,
	})
	if err != nil {
		if ctx.Err() != nil {
			if errors.Is(context.Cause(ctx), errRunTimeout) {
				return nil, &Failure{Code: CodeTimeout, Message: fmt.Sprintf("the call reached its timeout of %dms", req.TimeoutMS)}
			}
			return nil, &Failure{Code: CodeCancelled, Message: "the caller went away"}
		}
		return nil, classifyLLMError(err, CodeUpstream)
	}
	return &Completion{Text: resp.Message.Content, Usage: resp.Usage, StopReason: resp.StopReason}, nil
}
