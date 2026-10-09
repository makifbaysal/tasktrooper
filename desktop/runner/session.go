package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// claude.run — a headless Claude Code session, streamed.
//
// This is the call the rest of this program exists to serve, and the one whose
// failure modes are worth being explicit about. The WHOLE task is delegated to
// the session: it clones, edits, builds, commits and pushes. So it runs for
// minutes, it produces output the entire time, and it can be asked to stop.
//
// The three things that must be true, in the order they matter:
//
//  1. Output is forwarded as it arrives, line by line. Buffering it would make
//     the cloud blind for the length of the task, which is the experience this
//     rework exists to end.
//  2. Cancellation kills the PROCESS GROUP, not the process. `claude` spawns
//     git, node, compilers and test runners; signalling only the parent leaves
//     a tree of them running against a checkout nobody is watching. A stopped
//     task that left a live session behind is a bug this project has already
//     shipped once, and `setpgid` plus a negative pid is what stops it coming
//     back.
//  3. The prompt goes on STDIN, never in argv. Partly because argv on macOS is
//     world-readable and a prompt carries the task's content, and partly
//     because a prompt beginning with `-` would otherwise be parsed as a flag.
//     Nothing a caller can influence reaches argv as free text. The MCP
//     token is the same rule wearing different clothes: it goes in a file
//     (mcp.go), and only that file's path is an argument.

// claudeGrace is how long the process group gets between SIGTERM and SIGKILL.
//
// Long enough for `claude` to write its session file and let a git command
// finish a write, short enough that a Stop button feels like one. It is a
// grace period, not a negotiation: SIGKILL follows whether or not anything
// answered.
const claudeGrace = 5 * time.Second

// claudeReapTimeout bounds the wait AFTER the group has been killed. Reaching
// it means a process is unkillable — a wedged kernel call, an uninterruptible
// filesystem — and reporting that is better than a call that never returns.
const claudeReapTimeout = 20 * time.Second

// drainBudget is the WORST-CASE time this program needs between receiving
// SIGTERM and being safe to kill: every in-flight run gets claudeGrace to
// answer SIGTERM, then up to claudeReapTimeout to be reaped after SIGKILL.
// Runs drain concurrently, so this is a sum over one run and not over all of
// them.
//
// It exists to be READ FROM OUTSIDE. The supervisor that starts this process
// has to wait at least this long before escalating to SIGKILL itself, and the
// two numbers were previously chosen independently — 8s there against 25s here
// — which meant quitting the app mid-task killed the runner before it had
// reaped its process groups, orphaning exactly the `claude` tree the ordered
// teardown exists to take down and leaving a bearer token on disk until the
// next startup sweep. `../src/main/supervisor/child.ts` derives its own budget
// from this value; THIS constant is the authoritative one, and
// `rules_test.go` fails if the two drift.
const drainBudget = claudeGrace + claudeReapTimeout

// outputLineLimit is the largest single line forwarded from the child in full.
//
// `--output-format stream-json` emits one JSON document per line, and a tool
// result is inside that document: a Read of a real source file, a diff, or a
// noisy test run all produce one legitimately enormous line. Eight megabytes is
// chosen to make that rare rather than to make it impossible — the limit is
// still a limit, and what matters far more than its value is what happens when
// it is reached. See forward: the rest of the line is CONSUMED and counted, and
// the caller is told how much was cut.
//
// It was one megabyte, and the comment here claimed an over-long line was
// "split rather than dropped". It was neither. `bufio.Scanner` stops at
// ErrTooLong, so forwarding of that stream ended for the rest of the run — and
// because nothing then drained the pipe, the child blocked on its next write,
// which meant `streams.Wait()` below never returned and the whole call hung
// with a live `claude` in it. A comment describing behaviour the code does not
// have is how that survived review.
const outputLineLimit = 8 * 1024 * 1024

// outputReadBuffer is how much of a line is held in the reader at once. Lines
// are assembled across as many of these as they need, so this bounds a read
// rather than a line.
const outputReadBuffer = 64 * 1024

// permissionModes is the CLI's own list, copied rather than passed through.
// These become argv, and an allowlist is what keeps a caller from turning this
// field into "any flag you like".
var permissionModes = map[string]bool{
	"acceptEdits":       true,
	"auto":              true,
	"bypassPermissions": true,
	"manual":            true,
	"dontAsk":           true,
	"plan":              true,
}

// modelName is deliberately narrow: an alias ("opus", "sonnet") or a full model
// id. No slashes, no spaces, and it cannot begin with a dash.
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var uuidLike = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// toolName is one entry of the tool policy. A plain identifier, which is both
// what these names are on either side — `Bash`, `Read`, and the MCP form
// `mcp__tasktrooper__update_criterion` — and what keeps one from becoming a
// flag when the policy is handed to the CLI.
var toolName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)

// effortLevel is the same grammar, shorter. The CLI owns which values mean
// anything; this side owns only the guarantee that whatever arrives is a word
// and not an option.
var effortLevel = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// envName is POSIX's own shape for an environment variable name. A name with
// an `=` in it would be two variables, and one with a dash is not a variable
// at all in any shell the session starts.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// The bounds on the two collection-shaped parameters. Neither is a limit
// anybody should ever meet; they are here because a map and a slice off the
// wire are otherwise an allocation primitive with a grammar in front of it.
const (
	maxToolPolicy  = 256
	maxEnvEntries  = 64
	maxEnvValueLen = 32 * 1024
)

type claudeRunParams struct {
	// Workspace is a path relative to the workspace folder — usually a
	// repository prepared by workspace.prepare. It must already exist: a run
	// that silently created its own directory would be a run against an empty
	// checkout.
	Workspace string `json:"workspace"`
	// Prompt is the whole task. It travels on stdin; see the header.
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
	// PermissionMode is one of the CLI's own modes. Unset means the CLI's
	// default, which is what an interactive user would get.
	PermissionMode string `json:"permission_mode,omitempty"`
	MaxTurns       int    `json:"max_turns,omitempty"`
	// SessionID fixes the id so the caller can find the transcript afterwards;
	// Resume continues one. They are mutually exclusive.
	SessionID string `json:"session_id,omitempty"`
	Resume    string `json:"resume,omitempty"`
	// TimeoutMS is a ceiling on the whole run. Zero means none, which is the
	// normal case: the caller cancels, and a task that legitimately takes an
	// hour must not be killed for it.
	TimeoutMS int64 `json:"timeout_ms,omitempty"`
	// MCP is how this session reaches TaskTrooper's own tools — ticking a
	// criterion, recording a verdict, moving a card. Optional, and absent means
	// the session gets none of them; see mcp.go for why that is a valid run and
	// not a default this side fills in.
	MCP *mcpParams `json:"mcp,omitempty"`
	// Tools is the agent's tool policy: one flat array of names, native and
	// `mcp__`-prefixed together, exactly as the policy states them. Splitting
	// them across the CLI's two flags is this side's job, because which flag
	// understands which name is a fact about the CLI and not about the policy —
	// see claudeArgs.
	//
	// What it buys is the NATIVE half. Without it an agent whose policy denies
	// `run_terminal` still gets Bash on this Mac: the MCP half is enforced by
	// the server the run's token authorises, and the native half by nothing at
	// all. Absent means no flag, which is the CLI's own default.
	Tools []string `json:"tools,omitempty"`
	// Effort is the CLI's own knob, passed through under a grammar.
	Effort string `json:"effort,omitempty"`
	// Env is extra environment for the child, so a repository that pins its own
	// Go or Node version is honoured here the way it is when a person runs the
	// same task locally. It EXTENDS this process's environment; it does not
	// replace it, and it cannot reshape the child — see checkEnv.
	Env map[string]string `json:"env,omitempty"`
}

type claudeRunResult struct {
	Workspace  string `json:"workspace"`
	ExitCode   int    `json:"exit_code"`
	Signal     string `json:"signal,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// preparedRun is everything `claude.run` needs, worked out and validated
// BEFORE a byte of the response has been written.
//
// The split exists because of HTTP. Once the 200 is on the wire the only way
// left to report a failure is a `done` frame the caller has to read a stream to
// reach, so anything that can be refused has to be refused first — a bad
// workspace, a model name that is really a flag, a prompt that is not there.
// What is left after this function is a process to spawn.
type preparedRun struct {
	dir    string
	args   []string
	prompt string
	// mcp is the validated MCP configuration, or nil when the caller sent
	// none. Validated here and written to disk later, once a session slot is
	// free: a call queued behind fifteen others has no business holding a
	// bearer token on disk while it waits.
	mcp *mcpConfig
	// env is the extra environment for the child, already validated and in
	// "NAME=value" form. Appended to this process's own environment at spawn,
	// never substituted for it.
	env []string
	// timeout is a ceiling on the whole run, or zero for none — which is the
	// normal case: the caller cancels, and a task that legitimately takes an
	// hour must not be killed for it.
	timeout time.Duration
}

func (s *runnerServer) prepareRun(p claudeRunParams) (preparedRun, *rpcError) {
	if p.Prompt == "" {
		return preparedRun{}, failure(codeBadRequest, "prompt is required")
	}

	dir, err := resolveInWorkspace(s.cfg.workspaceDir, p.Workspace)
	if err != nil {
		return preparedRun{}, failure(codeBadRequest, "workspace: %v", err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return preparedRun{}, failure(codeBadRequest, "workspace %q is not a directory on this machine; call workspace.prepare first", p.Workspace)
	}
	if _, launchErr := launcherFor(s.cfg.claudeBin); launchErr != nil {
		return preparedRun{}, failure(codeNotReady, "%v", launchErr)
	}

	args, argErr := claudeArgs(p)
	if argErr != nil {
		return preparedRun{}, argErr
	}

	// The `mcp` object ends up in a file the CLI parses, which makes it the
	// same kind of input as the fields that end up in argv: refused here, in
	// front of the 200, or not at all.
	mcp, mcpErr := checkMCP(p.MCP)
	if mcpErr != nil {
		return preparedRun{}, mcpErr
	}

	env, envErr := checkEnv(p.Env)
	if envErr != nil {
		return preparedRun{}, envErr
	}

	return preparedRun{
		dir:     dir,
		args:    args,
		prompt:  p.Prompt,
		mcp:     mcp,
		env:     env,
		timeout: time.Duration(p.TimeoutMS) * time.Millisecond,
	}, nil
}

// claudeArgs builds the argv, and every value in it has been through a
// grammar. The prompt is not here on purpose — see the file header.
func claudeArgs(p claudeRunParams) ([]string, *rpcError) {
	// --print is the headless mode; stream-json is one JSON document per line,
	// which is what makes forwarding line by line meaningful rather than
	// forwarding fragments of a pretty-printed transcript. --verbose is not
	// optional with it: without it the CLI collapses the stream to the final
	// message, which is the buffering this call exists to avoid.
	//
	// --setting-sources project,local, on EVERY run and deliberately not a
	// parameter. Left off, the CLI's default also loads the user's own
	// ~/.claude — so a `SessionStart` hook or a plugin somebody installed for
	// themselves would execute inside a board task on their Mac. That is the
	// same problem as a repository's `.mcp.json`, which --strict-mcp-config
	// already excludes, and it gets the same answer for the same reason. It is
	// unconditional because a security property the caller has to remember to
	// ask for is not a security property: the cloud forgetting the field once
	// would silently run somebody's hooks.
	//
	// --disallowedTools AskUserQuestion, on every run too: in --print nobody
	// answers the CLI's own question card and the pod never sees its answer, so
	// a question asked through it is lost. The MCP ask_user is the one the pod
	// carries back to the human, and with this it is the only way to ask.
	args := []string{
		"--print", "--output-format", "stream-json", "--verbose",
		"--setting-sources", "project,local",
		"--disallowedTools", "AskUserQuestion",
	}

	if p.Model != "" {
		if !modelName.MatchString(p.Model) {
			return nil, failure(codeBadRequest, "model %q is not a model name", p.Model)
		}
		args = append(args, "--model", p.Model)
	}
	if p.PermissionMode != "" {
		if !permissionModes[p.PermissionMode] {
			return nil, failure(codeBadRequest, "permission_mode %q is not one the CLI has", p.PermissionMode)
		}
		args = append(args, "--permission-mode", p.PermissionMode)
	}
	if p.MaxTurns != 0 {
		if p.MaxTurns < 1 || p.MaxTurns > 1000 {
			return nil, failure(codeBadRequest, "max_turns %d is outside 1..1000", p.MaxTurns)
		}
		args = append(args, "--max-turns", strconv.Itoa(p.MaxTurns))
	}
	// The tool policy, SPLIT BY PREFIX, because the CLI's two flags mean two
	// different things and only one of them understands each kind of name.
	// Measured against claude 2.1.220 rather than read off `--help`:
	//
	//	--tools Read,Bash                    → built-ins Read and Bash, and
	//	                                       EVERY mcp__ tool still offered
	//	--tools Read,mcp__tt__update         → built-in Read only; the mcp__
	//	                                       entry contributes nothing at all
	//	--tools ""                           → no built-ins, mcp__ tools intact
	//	--allowedTools mcp__tt__update       → nothing removed; it is a
	//	                                       permission grant, not a filter
	//	--disallowedTools mcp__tt__record    → that one tool removed
	//
	// So `--tools` filters the BUILT-IN surface and silently ignores mcp__
	// names, which is why sending them to it was a no-op dressed as a policy.
	// Native names go there; mcp__ names go to `--allowedTools`, where they at
	// least pre-approve exactly the tools the policy names under a permission
	// mode that would otherwise stop to ask — with nobody to ask, in --print.
	//
	// What this does NOT do is narrow the MCP surface, and no arrangement of
	// these flags can from here: the only flag that removes an mcp__ tool is
	// --disallowedTools, which needs the COMPLEMENT of the allow-list, and this
	// side never learns the server's full tool list. The MCP half stays enforced
	// by the server the per-run token authorises. See runner/CLAUDE.md.
	//
	// Each name is joined into ONE argument with commas, which is unambiguous
	// precisely because the grammar above forbids a comma, a space and a leading
	// dash: no entry can end the value early or start a second flag.
	if p.Tools != nil {
		if len(p.Tools) == 0 {
			return nil, failure(codeBadRequest, "tools is present but empty; omit it to send no policy, because an empty policy would read as \"every tool\"")
		}
		if len(p.Tools) > maxToolPolicy {
			return nil, failure(codeBadRequest, "tools has %d entries, more than the %d this runner will pass", len(p.Tools), maxToolPolicy)
		}
		var native, mcpTools []string
		for _, tool := range p.Tools {
			if !toolName.MatchString(tool) {
				return nil, failure(codeBadRequest, "tools entry %q is not a tool name", tool)
			}
			if strings.HasPrefix(tool, mcpToolPrefix) {
				mcpTools = append(mcpTools, tool)
			} else {
				native = append(native, tool)
			}
		}
		// The empty string, not the omitted flag, when a policy names no
		// built-in at all. Omitting it would mean the CLI's whole built-in
		// surface — Bash included — which is the exact accidental widening this
		// parameter exists to prevent, and it is what a naive split would have
		// produced for an MCP-only policy.
		args = append(args, "--tools", strings.Join(native, ","))
		if len(mcpTools) > 0 {
			args = append(args, "--allowedTools", strings.Join(mcpTools, ","))
		}
	}
	if p.Effort != "" {
		if !effortLevel.MatchString(p.Effort) {
			return nil, failure(codeBadRequest, "effort %q is not an effort level", p.Effort)
		}
		args = append(args, "--effort", p.Effort)
	}
	if p.SessionID != "" && p.Resume != "" {
		return nil, failure(codeBadRequest, "session_id and resume cannot both be set: one starts a session and the other continues one")
	}
	if p.SessionID != "" {
		if !uuidLike.MatchString(p.SessionID) {
			return nil, failure(codeBadRequest, "session_id must be a uuid")
		}
		args = append(args, "--session-id", p.SessionID)
	}
	if p.Resume != "" {
		if !uuidLike.MatchString(p.Resume) {
			return nil, failure(codeBadRequest, "resume must be a session uuid")
		}
		args = append(args, "--resume", p.Resume)
	}
	return args, nil
}

// `env` is for CONFIGURING the child, never for reshaping it — and the only
// way to hold that line is an ALLOWLIST.
//
// This was a denylist: PATH, HOME, the git variables that name a command, the
// DYLD_/LD_/CLAUDE_/ANTHROPIC_ families. It read as thorough and it was not
// close. Everything in this list got through it, and every one of them decides
// what the session executes or where it executes it:
//
//	GIT_EXEC_PATH      git runs its git-* helpers from here AND prepends it to
//	                   PATH for them, so it redirects every git in the session
//	GIT_TEMPLATE_DIR   its hooks/ are copied into every repository git clones
//	GIT_DIR            points every git command at a repository outside the
//	GIT_WORK_TREE      workspace root, which is the containment rule undone
//	XDG_CONFIG_HOME    reinstates git/config — core.sshCommand, aliases,
//	                   core.pager — after HOME and GIT_CONFIG* were refused
//	HTTPS_PROXY        repoints ALL traffic including the model's, which is
//	HTTP_PROXY         what ANTHROPIC_BASE_URL was refused for
//	ALL_PROXY
//	NODE_PATH          decides which module `require` resolves
//	PYTHONPATH         decides which module `import` executes
//	PERL5OPT           `-Mevil` runs code before the script
//	RUBYOPT            `-rfoo` does the same
//	JAVA_TOOL_OPTIONS  `-javaagent:` loads a jar into every JVM
//	_JAVA_OPTIONS
//	GOFLAGS            `-toolexec=` runs a program for every compile step
//	CC / CXX           cgo and every build system run what they name
//	LESSOPEN           `|cmd %s` runs a command through less
//	BROWSER / MANPAGER / SSL_CERT_FILE / NODE_EXTRA_CA_CERTS / …
//
// The list above is not the fix. It is the evidence that a denylist cannot be
// the fix: every language runtime and every tool a session shells out to ships
// its own "load this file" or "run this program" variable, and a repository
// that adds a tool adds names nobody here has heard of. A denylist is a claim
// that the set is closed, and it is not — `CLAUDE.md` states the rule as an
// absolute ("a caller may configure a process but not choose what it executes")
// while the old comment admitted the list was incomplete, and of those two the
// rule is the one worth keeping.
//
// So: **this parameter exists for version pins and nothing else.** That is the
// use it was added for — a repository pinning its own Go or Node toolchain,
// honoured on this Mac the way it is when a person runs the same task in the
// same checkout — and it is the whole of what is allowed. A name outside the
// allowlist is refused with the list in the message, so the failure names its
// own remedy instead of sending somebody to read this file.
//
// Two ways in, both narrow:
//
//   - envAllowed, the exact names. Version pins and the two build-cache
//     locations that are genuinely just locations.
//   - envAllowedPrefixes, one reserved prefix. `TT_` belongs to TaskTrooper, so
//     a task can be handed its own values without anybody widening this list
//     again. Nothing on a Mac reads `TT_*` for anything but what a task puts
//     there.
//
// Adding a name here is a security decision, not a convenience one. The
// question to answer is not "is this useful" but "can a value for this name
// change what the session RUNS" — and for anything that names a path to a
// program, a library, a config file or a proxy, the answer is yes.
var envAllowed = map[string]bool{
	// Toolchain pins: the reason this parameter exists. Each is a VERSION,
	// resolved by a version manager the session already trusts, not a location.
	"GOTOOLCHAIN":      true,
	"NODE_VERSION":     true,
	"PYTHON_VERSION":   true,
	"RUBY_VERSION":     true,
	"JAVA_VERSION":     true,
	"FLUTTER_VERSION":  true,
	"RUST_TOOLCHAIN":   true,
	"RUSTUP_TOOLCHAIN": true,

	// Non-interactivity and output shape. These change how a tool talks, never
	// what it runs, and a session with no terminal wants them.
	"CI":                  true,
	"TERM":                true,
	"NO_COLOR":            true,
	"FORCE_COLOR":         true,
	"DEBIAN_FRONTEND":     true,
	"LANG":                true,
	"LC_ALL":              true,
	"TZ":                  true,
	"npm_config_loglevel": true,
}

// envAllowedPrefixes is the reserved family. One entry, deliberately: a second
// would be a second thing to reason about, and the point of a prefix is that
// nothing outside this product reads it.
var envAllowedPrefixes = []string{"TT_"}

// envNameAllowed is the whole of the rule: on the list, or under the reserved
// prefix. Nothing is inferred from the shape of a name.
func envNameAllowed(name string) bool {
	if envAllowed[name] {
		return true
	}
	for _, prefix := range envAllowedPrefixes {
		// A bare "TT_" is the prefix and not a name, so it is not allowed by
		// being one character short of meaning anything.
		if len(name) > len(prefix) && strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// allowedEnvNames is the sorted allowlist, for the refusal message.
func allowedEnvNames() []string {
	names := make([]string, 0, len(envAllowed))
	for name := range envAllowed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkEnv validates the extra environment and returns it in exec's own
// "NAME=value" form, sorted — so two identical requests produce an identical
// child and a log line about one is stable.
//
// Absent is the normal case and is not an error.
func checkEnv(env map[string]string) ([]string, *rpcError) {
	if len(env) == 0 {
		return nil, nil
	}
	if len(env) > maxEnvEntries {
		return nil, failure(codeBadRequest, "env has %d entries, more than the %d this runner will pass", len(env), maxEnvEntries)
	}

	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		if !envName.MatchString(name) {
			return nil, failure(codeBadRequest, "env name %q is not an environment variable name", name)
		}
		if !envNameAllowed(name) {
			// The message carries the whole allowlist. A refusal that only says
			// "not allowed" sends somebody to read this file to find out what
			// is; one that lists the set answers the question it raises.
			return nil, failure(codeBadRequest,
				"env may not set %s: this parameter is for version pins only, and the allowed names are %s (or a %s* name of your own). "+
					"A name that decides what the session runs — a program, a library, a config file, a proxy — is not configuration",
				name, strings.Join(allowedEnvNames(), ", "), envAllowedPrefixes[0])
		}
		value := env[name]
		if len(value) > maxEnvValueLen {
			return nil, failure(codeBadRequest, "env value for %s is longer than %d bytes", name, maxEnvValueLen)
		}
		// A NUL cannot be carried through exec at all — the child would see a
		// truncated value, or Go would refuse the spawn with an error that
		// names nothing useful.
		if strings.ContainsRune(value, 0) {
			return nil, failure(codeBadRequest, "env value for %s contains a NUL byte", name)
		}
		out = append(out, name+"="+value)
	}
	return out, nil
}

// spawnClaude runs the session and streams it. Split out from runClaudeSession
// so the test can drive a real process with a real stream and no JSON around
// it.
func spawnClaude(ctx context.Context, c *call, dir string, args []string, prompt string, env []string) (any, *rpcError) {
	// Deliberately not exec.CommandContext: its own cancellation kills the
	// process and not the group, which is the exact bug this code exists to
	// avoid. The context is honoured below, by killing the group.
	cmd, err := commandFor(c.cfg.claudeBin, args...)
	if err != nil {
		return nil, failure(codeNotReady, "%v", err)
	}
	cmd.Dir = dir
	// The caller's extra environment EXTENDS this process's and comes last:
	// os/exec keeps the FINAL occurrence of a repeated name, so a value the
	// control plane sent wins over an inherited one deterministically instead of
	// depending on what a given libc does with a duplicate. What may be in it at
	// all is checkEnv's business, not this function's.
	cmd.Env = append(os.Environ(), env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, failure(codeInternal, "could not open the session's stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, failure(codeInternal, "could not open the session's stdout: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, failure(codeInternal, "could not open the session's stderr: %v", err)
	}

	started := time.Now()
	group, err := startProcessGroup(cmd)
	if err != nil {
		return nil, failure(codeInternal, "could not start %s: %v", c.cfg.claudeBin, err)
	}
	defer group.release()
	log.Info().Str("call", c.id).Int("pid", cmd.Process.Pid).Str("dir", dir).Msg("claude session started")

	// The prompt, then EOF. `claude --print` with no positional prompt reads
	// stdin, so this is the CLI's own documented pipe usage rather than a trick.
	go func() {
		defer func() { _ = stdin.Close() }()
		if _, err := io.WriteString(stdin, prompt); err != nil {
			log.Warn().Str("call", c.id).Err(err).Msg("could not write the prompt")
		}
	}()

	// A write to the caller that fails means the far end is gone. Killing the
	// session at that moment is the whole point: nobody can receive its output,
	// and a session running for nobody is the leak.
	forwardCtx, stopForwarding := context.WithCancel(ctx)
	defer stopForwarding()

	var streams sync.WaitGroup
	streams.Add(2)
	go func() { defer streams.Done(); forward(c, "stdout", stdout, stopForwarding) }()
	go func() { defer streams.Done(); forward(c, "stderr", stderr, stopForwarding) }()

	exited := make(chan struct{})
	killed := make(chan struct{})
	go func() {
		select {
		case <-forwardCtx.Done():
			// Cancelled, timed out, the tunnel dropped, or the caller went
			// away. All four mean the same thing to the process tree.
			close(killed)
			killProcessGroup(group, claudeGrace, exited)
		case <-exited:
		}
	}()

	// Both pipes to EOF before Wait, which is what os/exec requires — and which
	// the group kill above guarantees will happen, because every process that
	// could be holding the write end is in that group.
	streams.Wait()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-waitErr:
	case <-time.After(claudeReapTimeout):
		close(exited)
		return nil, failure(codeInternal, "the Claude Code session would not exit %s after being killed", claudeReapTimeout)
	}
	close(exited)

	result := claudeRunResult{Workspace: dir, DurationMS: time.Since(started).Milliseconds()}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		result.ExitCode = 0
	case errors.As(runErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			result.Signal = status.Signal().String()
		}
	default:
		return nil, failure(codeInternal, "waiting for the Claude Code session: %v", runErr)
	}

	select {
	case <-killed:
		// The exit code of a process we killed describes the killing, not the
		// task. The claude.run handler turns a cancelled call into `cancelled`; a
		// timeout is the one case that has no other reporter.
		if ctx.Err() != nil && c.ctx.Err() == nil {
			return nil, failure(codeCancelled, "the session passed its %dms limit and was stopped", time.Since(started).Milliseconds())
		}
	default:
	}

	log.Info().Str("call", c.id).Int("exit", result.ExitCode).Dur("took", time.Since(started)).Msg("claude session finished")
	return result, nil
}

// forward reads one of the child's streams line by line and emits each line.
//
// Two rules, and the second one is load-bearing in a way that is not obvious:
//
//  1. A failed emit stops the forwarding AND cancels the run, because the only
//     reason a write fails here is that the caller's stream is gone.
//  2. **This function never stops reading while the child is alive.** The pipe
//     between them is 64 KiB; a forwarder that returns early leaves the child
//     blocked on its next write, and a blocked child never reaches EOF, never
//     lets `streams.Wait()` return, and never gets as far as the code that
//     would kill it. Every path out of the loop below therefore either drains
//     the rest of the stream or is an actual end-of-stream.
func forward(c *call, name string, r io.Reader, onWriteFailure context.CancelFunc) {
	reader := bufio.NewReaderSize(r, outputReadBuffer)
	for {
		line, dropped, err := readLimitedLine(reader, outputLineLimit)

		if len(line) > 0 || dropped > 0 {
			text := string(line)
			if dropped > 0 {
				// Said in the transcript, not only in this program's log. The
				// caller is the one reading the output, and a line that just
				// stops is indistinguishable from a session that went quiet.
				text += fmt.Sprintf("… [tasktrooper: this line was %d bytes over the %d-byte limit and was cut here]",
					dropped, outputLineLimit)
				log.Warn().Str("call", c.id).Str("stream", name).Int64("dropped", dropped).
					Msg("a line of output was longer than the limit and was truncated")
			}
			if writeErr := c.output(name, text); writeErr != nil {
				log.Debug().Str("call", c.id).Err(writeErr).Msg("caller stopped reading; stopping the session")
				onWriteFailure()
				// Keep draining so the child never blocks on a full pipe while
				// it is being killed — a blocked writer is a process that
				// cannot handle its own SIGTERM.
				_, _ = io.Copy(io.Discard, r)
				return
			}
		}

		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				// The stream broke rather than ended. Worth one line: the
				// caller is missing transcript and cannot tell from its side.
				log.Warn().Str("call", c.id).Str("stream", name).Err(err).Msg("stopped reading the session's output")
			}
			return
		}
	}
}

// readLimitedLine reads one newline-terminated line, keeps at most limit bytes
// of it, and CONSUMES the remainder rather than leaving it in the pipe.
//
// That last part is the whole point of the function. `bufio.Scanner` cannot do
// it: it reports ErrTooLong and refuses to advance, so the only way to keep
// reading is to abandon the scanner, and abandoning it mid-stream is what
// wedged the child. Here an over-long line costs its tail and nothing else —
// the next line is still read, and the stream still reaches EOF.
//
// Returns the kept bytes, how many were dropped, and the read error (io.EOF at
// the end of the stream, which may still carry a final unterminated line).
func readLimitedLine(r *bufio.Reader, limit int) ([]byte, int64, error) {
	var kept []byte
	var dropped int64
	for {
		// ReadSlice returns what it has plus ErrBufferFull when a line is
		// longer than the reader's buffer, which is how a long line arrives
		// here in pieces instead of as an allocation this side cannot bound.
		chunk, err := r.ReadSlice('\n')
		body := chunk
		if err == nil {
			// The terminator is the frame boundary, not content. \r\n as well,
			// so a child that writes DOS line endings does not put a stray
			// carriage return in every event.
			body = bytes.TrimSuffix(body, []byte("\n"))
			body = bytes.TrimSuffix(body, []byte("\r"))
		}
		if room := limit - len(kept); room > 0 {
			if len(body) <= room {
				// append copies: chunk aliases the reader's buffer, which the
				// next read overwrites.
				kept = append(kept, body...)
			} else {
				kept = append(kept, body[:room]...)
				dropped += int64(len(body) - room)
			}
		} else {
			dropped += int64(len(body))
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return kept, dropped, err
	}
}
