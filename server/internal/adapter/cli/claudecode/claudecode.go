package claudecode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/application/activity"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const DefaultBinary = "claude"

const DefaultMaxTurns = 100

const DefaultRunTimeout = time.Hour

const DefaultMaxConcurrentSessions = 3

const DefaultSettingSources = "project,local"

var knownSettingSources = map[string]bool{"user": true, "project": true, "local": true}

func normalizeSettingSources(raw string) string {
	seen := map[string]bool{}
	kept := make([]string, 0, len(knownSettingSources))
	for _, part := range strings.Split(raw, ",") {
		source := strings.ToLower(strings.TrimSpace(part))
		if !knownSettingSources[source] || seen[source] {
			continue
		}
		seen[source] = true
		kept = append(kept, source)
	}
	if len(kept) == 0 {
		return DefaultSettingSources
	}
	return strings.Join(kept, ",")
}

type Config struct {
	Binary                string
	MaxTurns              int
	RunTimeout            time.Duration
	SettingSources        string
	MCP                   MCPConfig
	MCPProvider           MCPProvider
	MaxConcurrentSessions int
	// Usage is where every session's token spend is metered, both into the
	// run's context accumulator and (kind=cli) the usage dashboard. Nil is
	// safe — Meter.Record is nil-receiver safe.
	Usage *usageapp.Meter
}

type Executor struct {
	bin            string
	maxTurns       int
	runTimeout     time.Duration
	settingSources string
	mcp            MCPConfig
	mcpProvider    MCPProvider
	usage          *usageapp.Meter
	now            func() time.Time
	sem            chan struct{}
	slotCap        int
	active         int64
	gateMu         sync.Mutex
	quotaUntil     time.Time
	quotaDetail    string
	quotaNextRetry time.Time
}

const quotaRetryInterval = 15 * time.Minute

var (
	_ port.TaskExecutor = (*Executor)(nil)
	_ port.ChatExecutor = (*Executor)(nil)
)

func ResolveBinary(configured string) (string, error) {
	bin := strings.TrimSpace(configured)
	if bin == "" {
		bin = DefaultBinary
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("agent cli binary %q not found on PATH: %w", bin, err)
	}
	return resolved, nil
}

func New(cfg Config) (*Executor, error) {
	resolved, err := ResolveBinary(cfg.Binary)
	if err != nil {
		return nil, err
	}
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	runTimeout := cfg.RunTimeout
	if runTimeout <= 0 {
		runTimeout = DefaultRunTimeout
	}
	slotCap := cfg.MaxConcurrentSessions
	if slotCap == 0 {
		slotCap = DefaultMaxConcurrentSessions
	} else if slotCap < 0 {
		slotCap = -1
	}
	var sem chan struct{}
	if slotCap > 0 {
		sem = make(chan struct{}, slotCap)
	}
	return &Executor{
		bin:            resolved,
		maxTurns:       maxTurns,
		runTimeout:     runTimeout,
		settingSources: normalizeSettingSources(cfg.SettingSources),
		mcp:            cfg.MCP,
		mcpProvider:    cfg.MCPProvider,
		usage:          cfg.Usage,
		now:            time.Now,
		sem:            sem,
		slotCap:        slotCap,
	}, nil
}

func (e *Executor) Supports(provider domain.LLMProviderType) bool {
	return e != nil && provider == domain.LLMProviderClaudeCode
}

func (e *Executor) armQuotaGate(block *domain.QuotaBlock) {
	if block == nil {
		return
	}
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	if block.ResumeAt.After(e.quotaUntil) {
		e.quotaUntil = block.ResumeAt
		e.quotaDetail = block.Detail
	}
	e.quotaNextRetry = e.now().Add(quotaRetryInterval)
}

func (e *Executor) clearQuotaGate() {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	e.quotaUntil = time.Time{}
	e.quotaDetail = ""
	e.quotaNextRetry = time.Time{}
}

func (e *Executor) QuotaGate() (until time.Time, armed bool) {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	return e.quotaUntil, !e.quotaUntil.IsZero()
}

func (e *Executor) quotaGateState() (until time.Time, detail string, nextRetry time.Time, armed bool) {
	e.gateMu.Lock()
	defer e.gateMu.Unlock()
	return e.quotaUntil, e.quotaDetail, e.quotaNextRetry, !e.quotaUntil.IsZero()
}

func (e *Executor) gatedQuotaBlock(req domain.TaskExecution) *domain.QuotaBlock {
	until, detail, nextRetry, armed := e.quotaGateState()
	if !armed || !e.now().Before(until) {
		return nil
	}
	if !nextRetry.IsZero() && !e.now().Before(nextRetry) {
		return nil
	}
	log.Info().
		Str("task_key", req.TaskKey).
		Time("resume_at", until).
		Msg("agent cli usage limit gate is armed; parking without spawning")
	return &domain.QuotaBlock{
		ResumeAt:     until,
		CLISessionID: req.ResumeSessionID,
		Detail:       "another agent cli session hit the usage limit: " + detail,
		Provider:     domain.LLMProviderClaudeCode,
	}
}

func (e *Executor) SlotsInUse() (used, cap int) {
	return int(atomic.LoadInt64(&e.active)), e.slotCap
}

func (e *Executor) acquireSlot(ctx context.Context, taskKey string) error {
	if e.sem != nil {
		start := e.now()
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		if waited := e.now().Sub(start); waited > time.Second {
			log.Info().
				Str("task_key", taskKey).
				Dur("waited", waited).
				Int("cap", e.slotCap).
				Msg("agent cli session waited for a concurrency slot")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("claude_code_slot_wait", map[string]any{
					"waited_ms":      waited.Milliseconds(),
					"max_concurrent": e.slotCap,
				})
			}
		}
	}
	atomic.AddInt64(&e.active, 1)
	return nil
}

func (e *Executor) releaseSlot() {
	atomic.AddInt64(&e.active, -1)
	if e.sem != nil {
		<-e.sem
	}
}

func (e *Executor) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	if e == nil {
		return domain.AgentResponse{}, errors.New("agent cli executor is not configured")
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.AgentResponse{}, errors.New("agent cli executor: no task workspace to run in")
	}

	if block := e.gatedQuotaBlock(req); block != nil {
		return domain.AgentResponse{}, block
	}

	if err := e.acquireSlot(ctx, req.TaskKey); err != nil {
		return domain.AgentResponse{}, err
	}
	defer e.releaseSlot()

	if block := e.gatedQuotaBlock(req); block != nil {
		return domain.AgentResponse{}, block
	}

	mcpCfg, releaseMCP, err := core.ResolveMCP(ctx, e.mcpProvider, e.mcp, MCPRun{
		Policy:        req.Policy,
		Label:         req.TaskKey,
		RequiresTools: true,
		SkillsOnDisk:  req.SkillsOnDisk,
	})
	defer releaseMCP()
	if err != nil {
		return domain.AgentResponse{}, err
	}

	mcpPath, cleanupMCP, err := writeMCPConfigFile(mcpCfg)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	defer cleanupMCP()

	sessionEnv, refusedEnv := domain.SessionEnv(req.Env)
	if len(refusedEnv) > 0 {
		log.Warn().Strs("names", refusedEnv).Str("task", req.TaskKey).
			Msg("agent cli: dropped session environment outside the allowlist")
	}

	fresh := func() invocation {
		systemPrompt, prompt := flattenHistory(req.History)

		systemPrompt = withToolManifest(systemPrompt, mcpCfg.Tools)
		return invocation{
			workDir:      req.WorkDir,
			systemPrompt: systemPrompt,
			prompt:       prompt,
			model:        req.Model,
			maxTurns:     req.MaxTurns,
			effort:       req.Effort,
			tools:        domain.NativeToolsForPolicy(req.Policy),
			label:        req.TaskKey,
			mcpPath:      mcpPath,
			env:          sessionEnv,
		}
	}

	inv := fresh()
	resumeSessionID := strings.TrimSpace(req.ResumeSessionID)
	if resumeSessionID != "" {
		inv.systemPrompt = ""
		if followUp := strings.TrimSpace(req.Prompt); followUp != "" {
			inv.prompt = followUp
		} else {
			inv.prompt = continuePrompt(req)
		}
		inv.resumeSessionID = req.ResumeSessionID
	}

	s, err := e.spawn(ctx, inv)
	if err != nil {
		return domain.AgentResponse{}, err
	}

	if resumeSessionID != "" && resumeRefused(s) {
		log.Info().
			Str("task_key", req.TaskKey).
			Str("cli_session_id", resumeSessionID).
			Msg("agent cli could not resume this task's cli session; starting a fresh one from the stored history")
		retry := fresh()
		retry.trace = s.trace
		if s, err = e.spawn(ctx, retry); err != nil {
			return domain.AgentResponse{}, err
		}
	}

	return e.finish(ctx, req.TaskKey, s)
}

type invocation struct {
	workDir         string
	env             []string
	systemPrompt    string
	prompt          string
	model           string
	resumeSessionID string
	maxTurns        int
	effort          string
	tools           []string
	label           string
	mcpPath         string
	trace           *core.Trace
	stream          port.ChatStream
}

func (e *Executor) spawn(ctx context.Context, inv invocation) (session, error) {
	runCtx, cancelRun := context.WithTimeout(ctx, e.runTimeout)
	defer cancelRun()

	systemPromptPath, cleanupSystemPrompt, err := core.WriteSystemPromptFile(inv.systemPrompt)
	if err != nil {
		return session{}, err
	}
	defer cleanupSystemPrompt()

	args := e.buildArgs(inv, systemPromptPath)
	cmd := winshim.Command(runCtx, e.bin, args...)
	cmd.Dir = inv.workDir
	cmd.Env = append(claudeChildEnv(ctx), inv.env...)
	cmd.Stdin = strings.NewReader(inv.prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return session{}, fmt.Errorf("agent cli stdout: %w", err)
	}
	stderr := core.NewTailWriter(core.StderrTailMax)
	cmd.Stderr = stderr

	tree, err := proctree.Start(cmd)
	if err != nil {
		return session{}, fmt.Errorf("start agent cli: %w", err)
	}
	defer func() {
		tree.Terminate(3 * time.Second)
		tree.Close()
	}()

	trace := inv.trace
	if trace == nil {
		trace = core.NewTrace(ctx, inv.label, "claude_code_session", recordedElsewhere, ledgerToolName)
	}

	guard := &initGuard{
		sink:    newStreamingSink(trace, inv.stream),
		label:   inv.label,
		require: inv.mcpPath != "",
		cancel:  cancelRun,
	}
	out, parseErr := parseStream(stdout, guard)

	if parseErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}

	waitErr := cmd.Wait()

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil

	return session{
		out:            out,
		trace:          trace,
		stderrTail:     stderr.String(),
		parseErr:       parseErr,
		waitErr:        waitErr,
		timedOut:       timedOut,
		initFault:      guard.fault,
		requestedModel: inv.model,
	}, nil
}

type initGuard struct {
	sink
	label   string
	require bool
	cancel  context.CancelFunc
	fault   error
}

func (g *initGuard) OnInit(init sessionInit) {
	log.Info().
		Str("task_key", g.label).
		Str("cli_session_id", init.SessionID).
		Int("native_tools", len(init.Tools)).
		Strs("mcp_servers", init.serverNames()).
		Msg("agent cli session started")

	if !g.require || !init.ServersReported {
		return
	}
	if _, listed := init.server(mcpServerName); listed {
		return
	}
	g.fault = fmt.Errorf(
		"the agent cli session started without the %s tool server (it loaded %v), so it could not move its card, "+
			"tick an acceptance criterion or record a verdict; the run was stopped instead of being left to finish blind",
		mcpServerName, init.serverNames())
	log.Error().
		Str("task_key", g.label).
		Str("cli_session_id", init.SessionID).
		Strs("mcp_servers", init.serverNames()).
		Msg("agent cli session did not load the tasktrooper tool server; stopping the run")
	g.cancel()
}

type session struct {
	out        outcome
	trace      *core.Trace
	stderrTail string
	parseErr   error
	waitErr    error
	timedOut   bool
	initFault  error
	// requestedModel is the invocation's own model flag, used when the CLI's
	// init event never reports one (e.g. the session died before init).
	requestedModel string
}

func (s session) sessionID() string {
	return firstNonEmpty(s.out.SessionID, s.trace.SessionID())
}

func (s session) failed() bool {
	if s.timedOut || s.parseErr != nil || s.waitErr != nil || !s.out.SawResult || s.out.IsError {
		return true
	}
	switch s.out.Subtype {
	case "", "success", "error_max_turns":
		return false
	default:
		return true
	}
}

type sessionFinisher struct {
	runTimeout time.Duration
	maxTurns   int
	now        func() time.Time
	usage      *usageapp.Meter
}

func (e *Executor) finish(ctx context.Context, label string, s session) (domain.AgentResponse, error) {
	resp, err := sessionFinisher{runTimeout: e.runTimeout, maxTurns: e.maxTurns, now: e.now, usage: e.usage}.finish(ctx, label, s)
	if err == nil {
		e.clearQuotaGate()
		return resp, nil
	}
	if block, ok := domain.QuotaBlockOf(err); ok {
		e.armQuotaGate(block)
	}
	return resp, err
}

func (f sessionFinisher) finish(ctx context.Context, label string, s session) (domain.AgentResponse, error) {
	out, stderrTail := s.out, s.stderrTail
	// At the top, ahead of every failure branch below: a session that timed
	// out, hit its quota or otherwise failed still burned the tokens the CLI
	// reports, and those must still count against the run and the dashboard.
	f.usage.Record(ctx, domain.LLMUsageRecord{
		Kind:             domain.LLMUsageKindCLI,
		Provider:         string(domain.LLMProviderClaudeCode),
		Model:            firstNonEmpty(out.Init.Model, s.requestedModel),
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
		CacheReadTokens:  out.Usage.CacheReadTokens,
		CacheWriteTokens: out.Usage.CacheWriteTokens,
	})

	sessionID := s.sessionID()

	if s.initFault != nil {
		return domain.AgentResponse{}, s.initFault
	}

	if s.timedOut {
		return domain.AgentResponse{}, fmt.Errorf(
			"agent cli did not finish within %s and was stopped (cli session %s): %s",
			f.runTimeout, sessionID, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}

	if s.failed() {
		if block := quotaBlockFrom(out, stderrTail, sessionID, f.now()); block != nil {
			log.Warn().
				Str("task_key", label).
				Str("cli_session_id", sessionID).
				Time("resume_at", block.ResumeAt).
				Msg("agent cli usage limit reached, parking the task")
			if rec := activity.FromContext(ctx); rec != nil {
				rec.Step("llm_provider_code_quota_park", map[string]any{
					"resume_at":      block.ResumeAt.UTC().Format(time.RFC3339),
					"cli_session_id": sessionID,
					"detail":         block.Detail,
				})
			}
			return domain.AgentResponse{}, block
		}
	}
	if s.parseErr != nil {
		return domain.AgentResponse{}, s.parseErr
	}

	if !out.SawResult {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.AgentResponse{}, ctxErr
		}
		if sig, ok := domain.ExitSignal(s.waitErr); ok {
			log.Warn().
				Str("task_key", label).
				Str("cli_session_id", sessionID).
				Str("signal", sig.String()).
				Msg("agent cli session was killed by an external signal")
			return domain.AgentResponse{}, fmt.Errorf(
				"agent cli was killed by signal %s from outside this run (cli session %s): %s",
				sig, sessionID, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
		}
		return domain.AgentResponse{}, fmt.Errorf("agent cli ended without a result (%v): %s",
			s.waitErr, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}

	if rec := activity.FromContext(ctx); rec != nil {
		rec.Step("claude_code_result", map[string]any{
			"subtype":        out.Subtype,
			"num_turns":      out.NumTurns,
			"turns":          s.trace.Turns(),
			"cost_usd":       out.CostUSD,
			"tool_calls":     out.ToolCalls,
			"tool_failures":  out.ToolFailures,
			"cli_session_id": sessionID,
			"model":          s.trace.Model(),
		})
	}
	log.Info().
		Str("task_key", label).
		Str("cli_session_id", sessionID).
		Str("subtype", out.Subtype).
		Int("turns", out.NumTurns).
		Float64("cost_usd", out.CostUSD).
		Msg("agent cli session finished")

	resp := domain.AgentResponse{
		Message:      domain.Message{Role: domain.RoleAssistant, Content: out.Text},
		Usage:        out.Usage,
		CLISessionID: sessionID,
	}

	switch {
	case out.Subtype == "error_max_turns":
		resp.Message.Content = strings.TrimSpace(out.Text + "\n\n" + maxTurnsNote(f.maxTurns))
		return resp, nil
	case out.IsError || (out.Subtype != "" && out.Subtype != "success"):
		return domain.AgentResponse{}, fmt.Errorf("agent cli failed (%s): %s",
			out.Subtype, domain.TruncateHead(firstNonEmpty(out.Text, strings.TrimSpace(stderrTail)), 1000))
	case s.waitErr != nil:
		return domain.AgentResponse{}, fmt.Errorf("agent cli exited with an error after reporting success (%v): %s",
			s.waitErr, domain.TruncateHead(strings.TrimSpace(stderrTail), 500))
	}
	if strings.TrimSpace(resp.Message.Content) == "" {
		return domain.AgentResponse{}, errors.New("agent cli finished without producing any answer")
	}
	return resp, nil
}

func maxTurnsNote(maxTurns int) string {
	return maxTurnsNoteKey.Render(maxTurnsNoteInput{MaxTurns: maxTurns})
}

func (e *Executor) buildArgs(inv invocation, systemPromptPath string) []string {
	args := []string{"-p"}
	if sid := strings.TrimSpace(inv.resumeSessionID); sid != "" {
		args = append(args, "--resume", sid)
	}

	maxTurns := inv.maxTurns
	if maxTurns <= 0 {
		maxTurns = e.maxTurns
	}
	args = append(args,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--max-turns", strconv.Itoa(maxTurns),
		"--disallowedTools", disallowedBashCommands+","+nativeQuestionTool,
		"--setting-sources", e.settingSources,
	)
	if systemPromptPath != "" {
		args = append(args, "--append-system-prompt-file", systemPromptPath)
	}
	if inv.effort != "" {
		args = append(args, "--effort", inv.effort)
	}

	if len(inv.tools) > 0 {
		args = append(args, "--tools", strings.Join(inv.tools, ","))
	}

	if model := strings.TrimSpace(inv.model); model != "" {
		args = append(args, "--model", model)
	}
	if inv.mcpPath != "" {
		args = append(args, "--mcp-config", inv.mcpPath,
			"--strict-mcp-config")
	}
	return args
}

// taskkill /IM is Windows' pkill: it stops every process with that image name,
// other tasks' servers and the user's own included. Git Bash spells the
// switches //IM and //F.
const disallowedBashCommands = "Bash(pkill:*),Bash(killall:*),Bash(taskkill /IM:*),Bash(taskkill //IM:*),Bash(taskkill /F /IM:*),Bash(taskkill //F //IM:*),Bash(gh pr merge:*)"

// The CLI's own question card has nobody to answer it in -p mode, and nothing
// here turns its call into a TaskTrooper question, so a session that reached
// for it lost the question. Withheld everywhere (a chat passes no --tools, so
// the built-in set alone does not keep it out), it leaves the MCP ask_user as
// the one way to ask — the one the board and the chat carry back to the human.
const nativeQuestionTool = "AskUserQuestion"

func continuePrompt(req domain.TaskExecution) string {
	task := strings.TrimSpace(req.TaskKey + " " + req.TaskTitle)
	if task == "" {
		task = "this task"
	}
	return continuePromptKey.Render(continuePromptInput{Task: task})
}

func flattenHistory(history []domain.Message) (systemPrompt, promptText string) {
	earlierTurnLabel := prompt.Text(earlierTurnLabelKey)
	var system, user []string
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case domain.RoleSystem:
			system = append(system, content)
		case domain.RoleAssistant:
			user = append(user, earlierTurnLabel+"\n"+content)
		default:
			user = append(user, content)
		}
	}
	return strings.Join(system, "\n\n"), strings.Join(user, "\n\n")
}

// claudeChildEnv is the environment Claude Code runs with. On Windows it
// refuses to start without Git Bash and finds one only in the default install
// locations unless CLAUDE_CODE_GIT_BASH_PATH names it, so when the user has
// not set that, the Git Bash run_terminal found on this host is named for it.
func claudeChildEnv(ctx context.Context) []string {
	env := core.ChildEnv(ctx, true, claudeEnvPassthrough)
	if runtime.GOOS == "windows" && os.Getenv("CLAUDE_CODE_GIT_BASH_PATH") == "" {
		if sh := hostshell.Default(); sh.Name == hostshell.GitBashName {
			env = append(env, "CLAUDE_CODE_GIT_BASH_PATH="+sh.Path)
		}
	}
	return env
}

var claudeEnvPassthrough = []string{
	"CLAUDE_CONFIG_DIR",
	"CLAUDE_CODE_GIT_BASH_PATH",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",
}
