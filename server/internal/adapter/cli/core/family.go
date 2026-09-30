package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	usageapp "github.com/makifbaysal/tasktrooper/server/internal/application/usage"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// StderrTailMax is how much of a child's stderr is kept for quoting when the
// session ends badly; the head is usually noise anyway.
const StderrTailMax = 8 << 10

// FamilySpec is everything a "role CLI" executor's run differs in that can be
// expressed as data plus hooks. Family implements the whole spawn+parse+finish
// flow against the spec; a flavor package is left with the two surfaces that
// genuinely cannot be shared — its event parser and its arg builder — plus its
// own probe/auth and its own mcp wiring.
type FamilySpec struct {
	// ProcessName is what logs and errors call the CLI.
	ProcessName string
	Provider    domain.LLMProviderType
	// Message scaffolding. NewError is the fmt for New's resolve failure.
	NewError      string
	NotConfigured string
	NoWorkDir     string
	StartFmt      string
	StdoutFmt     string
	GateLogMsg    string
	GateDetailFmt string
	QuotaLogMsg   string

	// BuildArgs is the CLI's invocation contract (its only real flag surface).
	BuildArgs func(inv Invocation) []string
	// Parse turns the CLI's stream format into the common Outcome.
	Parse func(r io.Reader, s Sink) (Outcome, error)
	// TraceStep is the activity step name for the CLI's session, and the two
	// predicates translate its native tool names into ledger names and filter
	// TaskTrooper's own tools back out of the ledger.
	TraceStep    string
	SinceOwnTool func(string) bool
	LedgerTool   func(string) string
	// ApplyMCP hands the run's MCP config to the CLI the way only it can: a
	// config file in the workspace (cursor, antigravity), an inline env var
	// (opencode 1.x) or a private server the run attaches to (opencode 2.x).
	ApplyMCP func(ctx context.Context, workDir string, cfg MCPConfig) (Launch, error)
	// WatchStderr wraps the stderr tail with anything that needs raw stderr
	// (opencode's rate-limit watcher). nil means the tail is all there is.
	WatchStderr func(cancel context.CancelFunc, tail io.Writer) io.Writer
	// AllowCleanExitAsResult lets a session that ended cleanly with real text
	// but no terminal result event count as an answer anyway (a known
	// opencode misbehaviour). Cursor and AGY have no such carve-out.
	AllowCleanExitAsResult bool
	// BlockFrom detects a spent-subscription outcome for this CLI.
	BlockFrom func(out Outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock
}

// Launch is what ApplyMCP adds to one run: env entries, args appended after
// BuildArgs', and a cleanup that runs once the session has ended.
type Launch struct {
	Env     []string
	Args    []string
	Cleanup func()
}

func (l Launch) cleanup() {
	if l.Cleanup != nil {
		l.Cleanup()
	}
}

// Family is the shared role-CLI executor. A flavor package embeds it as a
// value and forwards its port.TaskExecutor calls, so one implementation of the
// spawn/parse/finish flow serves cursor-agent, agy and opencode.
type Family struct {
	spec        FamilySpec
	bin         string
	runTimeout  time.Duration
	mcp         MCPConfig
	mcpProvider MCPProvider
	usage       *usageapp.Meter
}

func NewFamily(spec FamilySpec, bin string, runTimeout time.Duration, mcp MCPConfig, mcpProvider MCPProvider, usage *usageapp.Meter) *Family {
	return &Family{spec: spec, bin: bin, runTimeout: runTimeout, mcp: mcp, mcpProvider: mcpProvider, usage: usage}
}

func (f *Family) Spec() FamilySpec { return f.spec }

func (f *Family) Bin() string { return f.bin }

func (f *Family) Supports(provider domain.LLMProviderType) bool {
	return f != nil && provider == f.spec.Provider
}

// Execute resolves and applies the run's MCP config, spawns the CLI, and
// turns the outcome through the shared finish decision tree.
func (f *Family) Execute(ctx context.Context, req domain.TaskExecution, now func() time.Time) (domain.AgentResponse, error) {
	mcpCfg, releaseMCP, err := ResolveMCP(ctx, f.mcpProvider, f.mcp, MCPRun{Policy: req.Policy, Label: req.TaskKey})
	defer releaseMCP()
	if err != nil {
		return domain.AgentResponse{}, err
	}
	launch, err := f.spec.ApplyMCP(ctx, req.WorkDir, mcpCfg)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	defer launch.cleanup()

	inv := Invocation{
		Trace:   NewTrace(ctx, req.TaskKey, f.spec.TraceStep, f.spec.SinceOwnTool, f.spec.LedgerTool),
		WorkDir: req.WorkDir,
		Prompt:  FlattenHistory(req.History),
		Model:   req.Model,
		Label:   req.TaskKey,
	}
	s, err := f.spawn(ctx, inv, launch)
	if err != nil {
		return domain.AgentResponse{}, err
	}
	return f.finishInner(ctx, req.TaskKey, s, now)
}

// ExecuteChat runs one chat turn. None of these CLIs has a session-resume
// flag wired into BuildArgs (see Execute), so — like a board task — every
// turn resends the full flattened history instead of continuing a live CLI
// session; req.ResumeSessionID is therefore unused here.
func (f *Family) ExecuteChat(ctx context.Context, req domain.ChatExecution, out port.ChatStream, now func() time.Time) (domain.ChatResult, error) {
	mcpCfg, releaseMCP, err := ResolveMCP(ctx, f.mcpProvider, f.mcp, MCPRun{Policy: req.Policy, Label: req.SessionID})
	defer releaseMCP()
	if err != nil {
		return domain.ChatResult{}, err
	}
	launch, err := f.spec.ApplyMCP(ctx, req.WorkDir, mcpCfg)
	if err != nil {
		return domain.ChatResult{}, err
	}
	defer launch.cleanup()

	inv := Invocation{
		Trace:   NewTrace(ctx, req.SessionID, f.spec.TraceStep, f.spec.SinceOwnTool, f.spec.LedgerTool),
		WorkDir: req.WorkDir,
		Prompt:  FlattenHistory(req.History),
		Model:   req.Model,
		Label:   req.SessionID,
		Stream:  out,
	}
	s, err := f.spawn(ctx, inv, launch)
	if err != nil {
		return domain.ChatResult{}, err
	}
	resp, err := f.finishInner(ctx, req.SessionID, s, now)
	return domain.ChatResult{Response: resp}, err
}

func (f *Family) spawn(ctx context.Context, inv Invocation, launch Launch) (Session, error) {
	runCtx, cancel := context.WithTimeout(ctx, f.runTimeout)
	defer cancel()

	args := append(f.spec.BuildArgs(inv), launch.Args...)
	cmd := exec.CommandContext(runCtx, f.bin, args...)
	cmd.Dir = inv.WorkDir
	cmd.Env = append(ChildEnv(ctx, false, nil), launch.Env...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Session{}, fmt.Errorf(f.spec.StdoutFmt, err)
	}
	tail := &TailWriter{max: StderrTailMax}
	cmd.Stderr = tail
	if f.spec.WatchStderr != nil {
		if w := f.spec.WatchStderr(cancel, tail); w != nil {
			cmd.Stderr = w
		}
	}

	if err := cmd.Start(); err != nil {
		return Session{}, fmt.Errorf(f.spec.StartFmt, err)
	}

	out, parseErr := f.spec.Parse(stdout, NewStreamingSink(inv.Trace, inv.Stream))
	if parseErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil

	return Session{
		Out:             out,
		StderrTail:      tail.String(),
		ParseErr:        parseErr,
		WaitErr:         waitErr,
		TimedOut:        timedOut,
		InvocationModel: inv.Model,
	}, nil
}

func (f *Family) finishInner(ctx context.Context, label string, s Session, now func() time.Time) (domain.AgentResponse, error) {
	out := s.Out
	// At the top, ahead of every failure branch below: a session that timed
	// out or hit its quota still burned the tokens the CLI reports, and those
	// must still count against the run and the dashboard.
	f.usage.Record(ctx, domain.LLMUsageRecord{
		Kind:             domain.LLMUsageKindCLI,
		Provider:         string(f.spec.Provider),
		Model:            FirstNonEmpty(out.Model, out.Init.Model, s.InvocationModel),
		PromptTokens:     out.Usage.PromptTokens,
		CompletionTokens: out.Usage.CompletionTokens,
		CacheReadTokens:  out.Usage.CacheReadTokens,
		CacheWriteTokens: out.Usage.CacheWriteTokens,
	})

	if s.TimedOut {
		return domain.AgentResponse{}, fmt.Errorf("%s did not finish within %s and was stopped: %s",
			f.spec.ProcessName, f.runTimeout, domain.TruncateHead(strings.TrimSpace(s.StderrTail), 500))
	}
	if s.ParseErr != nil {
		return domain.AgentResponse{}, s.ParseErr
	}

	if !out.SawResult || out.IsError {
		if block := f.spec.BlockFrom(out, s.StderrTail, out.SessionID, now()); block != nil {
			log.Warn().
				Str("task_key", label).
				Str("cli_session_id", out.SessionID).
				Time("resume_at", block.ResumeAt).
				Msg(f.spec.QuotaLogMsg)
			return domain.AgentResponse{}, block
		}
	}

	if !out.SawResult {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.AgentResponse{}, ctxErr
		}
		noOutput := s.WaitErr != nil || strings.TrimSpace(out.Text) == ""
		if noOutput || !f.spec.AllowCleanExitAsResult {
			if sig, ok := domain.ExitSignal(s.WaitErr); ok {
				return domain.AgentResponse{}, fmt.Errorf(
					"%s was killed by signal %s from outside this run: %s",
					f.spec.ProcessName, sig, domain.TruncateHead(strings.TrimSpace(s.StderrTail), 500))
			}
			return domain.AgentResponse{}, fmt.Errorf("%s ended without a result (%v): %s",
				f.spec.ProcessName, s.WaitErr, domain.TruncateHead(strings.TrimSpace(s.StderrTail), 500))
		}
	}
	if out.IsError {
		return domain.AgentResponse{}, fmt.Errorf("%s failed (%s): %s",
			f.spec.ProcessName, out.Status, domain.TruncateHead(FirstNonEmpty(out.Text, strings.TrimSpace(s.StderrTail)), 1000))
	}
	if s.WaitErr != nil {
		return domain.AgentResponse{}, fmt.Errorf("%s exited with an error after reporting success (%v): %s",
			f.spec.ProcessName, s.WaitErr, domain.TruncateHead(strings.TrimSpace(s.StderrTail), 500))
	}
	if strings.TrimSpace(out.Text) == "" {
		return domain.AgentResponse{}, errors.New(f.spec.ProcessName + " finished without producing any answer")
	}
	return domain.AgentResponse{
		Message: domain.Message{Role: domain.RoleAssistant, Content: out.Text},
		Usage:   out.Usage,
	}, nil
}

// FlattenHistory folds a task's message history into the one prose prompt
// these CLIs take as their positional prompt argument. Assistant turns are
// marked so the model still sees them as prior answers, not new instructions.
func FlattenHistory(history []domain.Message) string {
	var parts []string
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if msg.Role == domain.RoleAssistant {
			content = earlierAssistantTurnLabel() + "\n" + content
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n")
}

// earlierAssistantTurnLabel renders cli.claude_earlier_turn_label by key
// rather than through a local prompt.Define: that key is already Defined in
// adapter/cli/claudecode (Define panics on a second registration of the same
// name), and the label text is identical across every role-CLI family, so
// this package borrows the rendered string instead of duplicating it.
func earlierAssistantTurnLabel() string {
	return prompt.MustRender("cli.claude_earlier_turn_label", struct{}{})
}