package core

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Outcome is the normalized result of one CLI session, regardless of which
// CLI produced it. Per-flavor stream parsers map their own event schema onto
// these fields; a flavor leaves untouched the ones its CLI never reports.
type Outcome struct {
	SessionID string
	Init      Init
	Text      string
	Usage     domain.Usage
	CostUSD   float64
	NumTurns  int
	Status    string
	Subtype   string
	Model     string
	IsError   bool
	ToolCalls int
	ToolFailures int
	SawResult bool
	// RecoveredError is an error the CLI reported mid-session and then
	// carried on past; the session still answered.
	RecoveredError string
	// ChildSessions are subagent sessions the CLI ran on the parent's behalf
	// whose usage its own stream never reports.
	ChildSessions []string
}

// Init is a CLI session's self-description (model, tools, MCP servers), used
// by the MCP init guard and the session trace step.
type Init struct {
	SessionID string
	Model     string
	Tools     []string
	Servers   []MCPServerState
	ServersReported bool
}

// MCPServerState is one MCP server a CLI announced it loaded.
type MCPServerState struct {
	Name   string
	Status string
}

func (i Init) Server(name string) (MCPServerState, bool) {
	for _, s := range i.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return MCPServerState{}, false
}

func (i Init) ServerNames() []string {
	names := make([]string, 0, len(i.Servers))
	for _, s := range i.Servers {
		names = append(names, s.Name+"="+s.Status)
	}
	return names
}

// Invocation is the union of everything a spawn needs that a CLI's BuildArgs
// or spawn-time wiring might read. Per-flavor preparers fill only the fields
// their CLI understands.
type Invocation struct {
	WorkDir    string
	Prompt     string
	SystemPrompt string
	Model      string
	ResumeSessionID string
	MaxTurns   int
	Effort     string
	Tools      []string
	Label      string
	MCPPath     string
	ConfigContent string
	Env         []string
	Trace       *Trace
	Stream      port.ChatStream
}

// Session is everything a finished CLI run produced, for the finisher.
type Session struct {
	Out        Outcome
	Trace      *Trace
	StderrTail string
	ParseErr   error
	WaitErr    error
	TimedOut   bool
	InitFault  error
	// InvocationModel is the model the invocation requested, used when the CLI
	// never reports one back (e.g. the session died before any output).
	InvocationModel string
}

func (s Session) SessionID() string {
	return FirstNonEmpty(s.Out.SessionID, s.Trace.SessionID())
}

// exitErr is WaitErr unless the only thing it says is the error the session
// already recovered from: opencode run exits 1 once any session error was
// reported, even when the session carried on and answered.
func (s Session) exitErr() error {
	var exitErr *exec.ExitError
	if s.Out.RecoveredError != "" && errors.As(s.WaitErr, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}
	return s.WaitErr
}

func (s Session) Failed() bool {
	if s.TimedOut || s.ParseErr != nil || s.exitErr() != nil || !s.Out.SawResult || s.Out.IsError {
		return true
	}
	switch s.Out.Subtype {
	case "", "success", "error_max_turns":
		return false
	default:
		return true
	}
}

func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}