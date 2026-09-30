package cursor

import (
	"context"
	"errors"
	"fmt"
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

// Executor runs tasks on cursor-agent. The whole spawn/parse/finish flow, the
// quota-gate internals and the child environment are shared across the CLI
// family (core.Family); what is left here is the argv contract, the stream
// parser, the probe/auth dialog and the quota wording.
type Executor struct {
	family core.Family
	gate   core.Gate
	now    func() time.Time
}

var _ port.TaskExecutor = (*Executor)(nil)

var familySpec = core.FamilySpec{
	ProcessName:     "cursor-agent",
	Provider:        domain.LLMProviderCursorAgent,
	NewError:        "cursor executor: %w",
	NotConfigured:   "cursor executor is not configured",
	NoWorkDir:       "cursor executor: no task workspace to run in",
	StartFmt:        "start cursor-agent: %w",
	StdoutFmt:       "cursor-agent stdout: %w",
	GateLogMsg:      "cursor usage limit gate is armed; parking without spawning",
	GateDetailFmt:   "another Cursor session hit the usage limit: %s",
	QuotaLogMsg:     "cursor usage limit reached, parking the task",
	BuildArgs:       buildArgs,
	Parse:           parseStream,
	ApplyMCP:        applyMCP,
	TraceStep:       "cursor_session",
	SinceOwnTool:    recordedElsewhere,
	LedgerTool:      ledgerToolName,
	BlockFrom:       quotaBlockFrom,
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
	return &Executor{
		family: *core.NewFamily(familySpec, resolved, runTimeout, cfg.MCP, cfg.MCPProvider, cfg.Usage),
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
		Provider:     domain.LLMProviderCursorAgent,
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

// buildArgs is the invocation contract with cursor-agent: print mode, apply
// everything unguarded, one model flag only when the agent chose one.
func buildArgs(inv core.Invocation) []string {
	args := []string{
		"-p", inv.Prompt,
		"--force",
		"--output-format", "stream-json",
	}
	if model := strings.TrimSpace(inv.Model); model != "" {
		args = append(args, "--model", model)
	}
	return args
}

// applyMCP merges the run's MCP server into the developer's .cursor/mcp.json
// for the run only.
func applyMCP(ctx context.Context, workDir string, cfg core.MCPConfig) (core.Launch, error) {
	cleanup, err := writeMCPConfigFile(workDir, cfg)
	return core.Launch{Cleanup: cleanup}, err
}