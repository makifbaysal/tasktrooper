package opencode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const DefaultRunTimeout = time.Hour

type Config struct {
	Binary      string
	RunTimeout  time.Duration
	MCP         core.MCPConfig
	MCPProvider core.MCPProvider
	// Usage is where every session's token spend is metered. Nil is safe.
	Usage *usageapp.Meter
}

// Executor runs tasks on opencode. The whole spawn/parse/finish flow, the
// quota-gate internals and the child environment are shared across the CLI
// family (core.Family); what is left here is the argv contract, the stream
// parser, the probe/auth dialog, the stderr rate-limit watcher and the quota
// wording.
type Executor struct {
	family core.Family
	gate   core.Gate
	now    func() time.Time
}

var (
	_ port.TaskExecutor = (*Executor)(nil)
	_ port.ChatExecutor = (*Executor)(nil)
)

var familySpec = core.FamilySpec{
	ProcessName:            "opencode",
	Provider:               domain.LLMProviderOpencode,
	NewError:               "opencode executor: %w",
	NotConfigured:          "opencode executor is not configured",
	NoWorkDir:              "opencode executor: no task workspace to run in",
	StartFmt:               "start opencode: %w",
	StdoutFmt:              "opencode stdout: %w",
	GateLogMsg:             "opencode rate limit gate is armed; parking without spawning",
	GateDetailFmt:          "another OpenCode session hit a provider rate limit: %s",
	QuotaLogMsg:            "opencode provider rate limit reached, parking the task",
	BuildArgs:              buildArgs,
	Parse:                  parseStream,
	WatchStderr:            watchStderr,
	AllowCleanExitAsResult: true,
	TraceStep:              "opencode_session",
	SinceOwnTool:           recordedElsewhere,
	LedgerTool:             ledgerToolName,
	BlockFrom:              quotaBlockFrom,
}

func New(cfg Config) (*Executor, error) {
	resolved, err := ResolveBinary(cfg.Binary)
	if err != nil {
		return nil, fmt.Errorf(familySpec.NewError, err)
	}
	runTimeout := cfg.RunTimeout
	if runTimeout <= 0 {
		runTimeout = DefaultRunTimeout
	}
	spec := familySpec
	spec.ApplyMCP = (&launcher{bin: resolved, gen: &generation{bin: resolved}}).applyMCP
	return &Executor{
		family: *core.NewFamily(spec, resolved, runTimeout, cfg.MCP, cfg.MCPProvider, cfg.Usage),
		now:    time.Now,
	}, nil
}

func (e *Executor) Supports(provider domain.LLMProviderType) bool {
	return e != nil && e.family.Supports(provider)
}

func (e *Executor) armQuotaGate(block *domain.QuotaBlock) { e.gate.Arm(block) }

func (e *Executor) clearQuotaGate() { e.gate.Clear() }

func (e *Executor) QuotaGate() (until time.Time, armed bool) {
	until, _, armed = e.gate.State()
	return until, armed
}

func (e *Executor) quotaGateState() (until time.Time, detail string, armed bool) {
	return e.gate.State()
}

func (e *Executor) gatedQuotaBlock(req domain.TaskExecution) *domain.QuotaBlock {
	until, detail, armed := e.gate.State()
	if !armed || !e.now().Before(until) {
		return nil
	}
	log.Info().
		Str("task_key", req.TaskKey).
		Time("resume_at", until).
		Msg(familySpec.GateLogMsg)
	return &domain.QuotaBlock{
		ResumeAt:     until,
		CLISessionID: req.ResumeSessionID,
		Detail:       fmt.Sprintf(familySpec.GateDetailFmt, detail),
		Provider:     domain.LLMProviderOpencode,
	}
}

func (e *Executor) Execute(ctx context.Context, req domain.TaskExecution) (domain.AgentResponse, error) {
	if e == nil {
		return domain.AgentResponse{}, errors.New(familySpec.NotConfigured)
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.AgentResponse{}, errors.New(familySpec.NoWorkDir)
	}
	if block := e.gatedQuotaBlock(req); block != nil {
		return domain.AgentResponse{}, block
	}

	resp, err := e.family.Execute(ctx, req, e.now)
	if err == nil {
		e.clearQuotaGate()
		return resp, nil
	}
	if block, ok := domain.QuotaBlockOf(err); ok {
		e.armQuotaGate(block)
	}
	return resp, err
}

func (e *Executor) gatedChatQuotaBlock(req domain.ChatExecution) *domain.QuotaBlock {
	until, detail, armed := e.gate.State()
	if !armed || !e.now().Before(until) {
		return nil
	}
	log.Info().
		Str("session_id", req.SessionID).
		Time("resume_at", until).
		Msg(familySpec.GateLogMsg)
	return &domain.QuotaBlock{
		ResumeAt:     until,
		CLISessionID: req.ResumeSessionID,
		Detail:       fmt.Sprintf(familySpec.GateDetailFmt, detail),
		Provider:     domain.LLMProviderOpencode,
	}
}

func (e *Executor) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream) (domain.ChatResult, error) {
	if e == nil {
		return domain.ChatResult{}, errors.New(familySpec.NotConfigured)
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		return domain.ChatResult{}, errors.New(familySpec.NoWorkDir)
	}
	if block := e.gatedChatQuotaBlock(req); block != nil {
		return domain.ChatResult{}, block
	}

	result, err := e.family.ExecuteChat(ctx, req, out, e.now)
	if err == nil {
		e.clearQuotaGate()
		return result, nil
	}
	if block, ok := domain.QuotaBlockOf(err); ok {
		e.armQuotaGate(block)
	}
	return result, err
}

// buildArgs is the invocation contract with opencode: the prompt is a
// positional run argument, and --auto is unconditional — without it a
// print-mode run only proposes edits with nobody at the terminal to approve.
func buildArgs(inv core.Invocation) []string {
	args := []string{
		"run", inv.Prompt,
		"--format", "json",
		"--auto",
	}
	if model := strings.TrimSpace(inv.Model); model != "" {
		args = append(args, "--model", model)
	}
	return args
}

type launcher struct {
	bin string
	gen *generation
}

// applyMCP hands the run's MCP server to opencode as an inline config env var,
// so nothing is ever written into the workspace. On 2.x that env var only
// reaches a server started with it, so the run gets a private one.
func (l *launcher) applyMCP(ctx context.Context, workDir string, cfg core.MCPConfig) (core.Launch, error) {
	if l.gen.serverBased(ctx) {
		return l.applyPrivateServer(ctx, workDir, cfg)
	}
	configContent, err := mcpConfigContentEnv(cfg)
	if err != nil || configContent == "" {
		return core.Launch{}, err
	}
	return core.Launch{Env: []string{"OPENCODE_CONFIG_CONTENT=" + configContent}}, nil
}

func (l *launcher) applyPrivateServer(ctx context.Context, workDir string, cfg core.MCPConfig) (core.Launch, error) {
	configContent, err := mcpServersConfigContent(cfg)
	if err != nil {
		return core.Launch{}, err
	}
	env := core.ChildEnv(ctx, false, nil)
	if configContent != "" {
		env = append(env, "OPENCODE_CONFIG_CONTENT="+configContent)
	}
	srv, err := startPrivateServer(ctx, l.bin, workDir, env)
	if err != nil {
		return core.Launch{}, err
	}
	if cfg.Set() {
		if err := srv.waitForMCP(ctx, locationDirectory(workDir)); err != nil {
			if ctx.Err() != nil {
				srv.stop()
				return core.Launch{}, ctx.Err()
			}
			log.Warn().Err(err).Str("work_dir", workDir).
				Msg("opencode session starts before its tasktrooper tools connected; its first turn may not see them")
		}
	}
	return core.Launch{
		Env:     []string{"OPENCODE_PASSWORD=" + srv.password},
		Args:    []string{"--server", srv.url},
		Cleanup: srv.stop,
	}, nil
}

// watchStderr keeps the stderr tail for the finish messages while feeding raw
// stderr to the rate-limit watcher, whose job is to cancel a session the
// provider has actually rate-limited but that (a known opencode bug) keeps
// running forever instead of exiting.
func watchStderr(cancel context.CancelFunc, tail io.Writer) io.Writer {
	return io.MultiWriter(tail, &rateLimitWatcher{cancel: cancel})
}

type rateLimitWatcher struct {
	cancel context.CancelFunc
	buf    []byte
	fired  bool
}

const watcherScanWindow = 4 << 10

func (w *rateLimitWatcher) Write(p []byte) (int, error) {
	if w.fired {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	if len(w.buf) > watcherScanWindow {
		w.buf = w.buf[len(w.buf)-watcherScanWindow:]
	}
	if rateLimitPattern.Match(w.buf) {
		w.fired = true
		w.cancel()
	}
	return len(p), nil
}