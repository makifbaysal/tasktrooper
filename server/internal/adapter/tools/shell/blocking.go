package shell

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
)

// A dev server never returns. Run in the foreground it can only ever end one
// way: the tool's timeout fires, the agent gets "command timed out after 1m0s"
// plus a partial log, and — because that reads like a flaky failure rather than
// a category error — it tends to try again. Two or three attempts is the whole
// iteration budget spent on a command that was never going to finish.
//
// So the ones we can recognise are refused immediately, with the answer the
// agent actually needs: to prove the code works, build it; to look at a running
// server, background it into a log file and read the file.
//
// Deliberately narrow. A false positive blocks real work, so only invocations
// whose entire purpose is to run until interrupted are listed, and any command
// that already backgrounds itself is allowed through untouched.
var blockingCommands = []struct {
	pattern *regexp.Regexp
	what    string
}{
	{regexp.MustCompile(`^(npm|pnpm|yarn|bun)\s+(run\s+)?(dev|start|serve|watch)\b`), "a dev server"},
	{regexp.MustCompile(`^(npx\s+)?(next|vite|nuxt|remix|astro)\s+(dev|start|preview)\b`), "a dev server"},
	// Bare `vite` is the dev server; `vite build` terminates, so only the
	// argument-less form is refused.
	{regexp.MustCompile(`^(npx\s+)?vite\s*$`), "a dev server"},
	{regexp.MustCompile(`^(npx\s+)?(nodemon|webpack-dev-server|http-server|serve|live-server)\b`), "a dev server"},
	{regexp.MustCompile(`^(npx\s+)?webpack\s+serve\b`), "a dev server"},
	{regexp.MustCompile(`^(python3?|py)\s+-m\s+http\.server\b`), "a dev server"},
	{regexp.MustCompile(`^(flask|uvicorn|gunicorn|rails\s+server|php\s+-S)\b`), "a dev server"},
	{regexp.MustCompile(`^docker(\s+compose|-compose)\s+up\b`), "a container stack in the foreground"},
	{regexp.MustCompile(`^tail\s+(-\S*f|--follow)`), "a follow-the-file tail"},
	{regexp.MustCompile(`^(watch|entr)\b`), "a watch loop"},
	{regexp.MustCompile(`^kubectl\s+(port-forward|logs\s+(-\S*f|--follow))\b`), "a streaming kubectl command"},
	{regexp.MustCompile(`^journalctl\s+(-\S*f|--follow)`), "a follow-the-log stream"},
}

// backgroundsAProcess reports whether command leaves a process running after
// the shell returns, which is the supported way to run a server. Only a
// trailing `&` used to count, so `(npm run dev > log 2>&1 & echo $! > pid)` —
// the shape the skills teach for keeping a PID — had its server killed with
// the call's process tree the moment the subshell exited. PowerShell and
// cmd.exe detach with Start-Process/Start-Job and `start`.
func backgroundsAProcess(command string, kind hostshell.Kind) bool {
	switch kind {
	case hostshell.PowerShell:
		return powershellDetach.MatchString(command)
	case hostshell.Cmd:
		return cmdDetach.MatchString(command)
	}
	return posixBackgrounds(command)
}

var (
	powershellDetach = regexp.MustCompile(`(?i)(^|[\s;(|&{])(start-process|saps|start-job|start-threadjob)\b|&\s*$`)
	cmdDetach        = regexp.MustCompile(`(?i)(^|[&|(])\s*start\s`)
)

// posixBackgrounds reports a background `&` (not `&&`, `|&`, a redirection
// like `2>&1`/`&>`/`>&2`, or quoted) that detaches on purpose: one that ends
// the line, possibly inside a subshell (`(… &)`), or one followed by
// `echo $!` to record the PID. A mid-line `&` that the same line then waits
// on is part of a foreground command, and its child dies with it.
func posixBackgrounds(command string) bool {
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				i++
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '&':
			var prev, next byte
			if i > 0 {
				prev = command[i-1]
			}
			if i+1 < len(command) {
				next = command[i+1]
			}
			if next == '&' {
				i++
				continue
			}
			if prev == '>' || prev == '<' || prev == '|' || next == '>' {
				continue
			}
			rest := command[i+1:]
			if strings.TrimRight(rest, " \t)") == "" || strings.HasPrefix(strings.TrimSpace(rest), "echo $!") {
				return true
			}
		}
	}
	return false
}

// blockingCommandReason returns why the command cannot be run in the foreground,
// or "" when it is fine to execute. Every segment of a compound command is
// checked: `cd /src && npm run dev` is exactly the shape that was burning the
// timeout.
func blockingCommandReason(command string) string {
	trimmed := strings.TrimSpace(command)
	kind := hostshell.Default().Kind
	if trimmed == "" || backgroundsAProcess(trimmed, kind) {
		return ""
	}
	for _, segment := range splitSegments(trimmed) {
		for _, b := range blockingCommands {
			if b.pattern.MatchString(segment) {
				return prompt.ShellBlockingCommandText(segment, b.what, string(kind))
			}
		}
	}
	return ""
}

// segmentSplitter breaks a shell line on the operators that start a new command.
var segmentSplitter = regexp.MustCompile(`\|\||&&|[;|]`)

// splitSegments yields each command in a compound line, normalised enough for
// the patterns above: leading `sudo`, environment assignments (`PORT=3000 npm
// run dev`) and surrounding whitespace are stripped, since none of them change
// what the command does.
func splitSegments(command string) []string {
	raw := segmentSplitter.Split(command, -1)
	out := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		seg = strings.TrimPrefix(seg, "(")
		for {
			fields := strings.Fields(seg)
			if len(fields) == 0 {
				break
			}
			head := fields[0]
			if head == "sudo" || head == "env" || (strings.Contains(head, "=") && !strings.HasPrefix(head, "-")) {
				seg = strings.TrimSpace(strings.TrimPrefix(seg, head))
				continue
			}
			break
		}
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}
