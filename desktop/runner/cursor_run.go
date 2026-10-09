package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// cursor.run — a headless cursor-agent session, streamed.
//
// The shape is claude.run's and opencode.run's: refuse everything refusable
// before the 200, stream one JSON object per line as it arrives, kill the
// process GROUP on cancellation. Argv and MCP handling are cursor-agent's own,
// mirrored from the local executor this runner has no other copy of
// (server/internal/adapter/cli/cursor/{executor.go,mcp.go}) —
// verified directly against the installed cursor-agent 2026 binary rather than
// read off --help alone:
//
//   - **The prompt is an ARGV element, not stdin — the one place this file
//     departs from claude.run and opencode.run, and not by choice.**
//     cursor-agent has no stdin-prompt mode: `agent [prompt...]` is a
//     positional argument, confirmed against the installed binary and against
//     agent-server's own fixture (testdata/fake-cursor-agent.sh: "cursor-agent
//     takes its prompt in argv, never on stdin"). What this file adds that the
//     local executor's buildArgs does not need is a literal `--` immediately
//     before the prompt: without it, a prompt beginning with `-` is parsed as
//     an unknown OPTION by cursor-agent's own arg parser — confirmed directly
//     (`cursor-agent -p --force --output-format text "--not-a-flag hello"`
//     answers "error: unknown option '--not-a-flag hello'"; the same call with
//     `--` before the string reaches authentication instead) — which is
//     exactly the "an operand that becomes a flag is a command" hazard
//     CLAUDE.md's hard rules name, landing on the one field here that cannot
//     be run through a grammar first because it is the task's own text.
//   - **No `--mcp-config` flag exists for cursor-agent.** The only place it
//     reads MCP servers from is `<workspace>/.cursor/mcp.json`, so that is
//     where this file writes one — merged into whatever the developer already
//     has there, restored to exactly that afterward, the same as the local
//     executor's mcp.go. This is the one on-disk credential in this package
//     that is NOT kept outside the workspace root (contrast mcp.go's run
//     directory in the user's temp dir): cursor-agent gives this file no other
//     place to put it, and the four-endings discipline that protects claude's
//     token file protects this one instead of an exception to it.
//   - **`--resume <chatId>`, confirmed against the installed binary's --help**
//     (`--resume [chatId]  Select a session to resume`), unlike the local
//     executor's own use of cursor-agent — which flattens history into a fresh
//     prompt every turn and, per family.go, never drives this flag at all.
//     That is a decision agent-server made for ITS calling convention; nothing
//     stops this runner's caller from resuming a real cursor-agent session.

// cursorModelName is looser than claude's alias-only grammar and opencode's
// slash-only one: cursor-agent's own --help documents parameterized overrides
// in brackets — `claude-opus-4-8[context=1m,effort=high,fast=false]` — so `[`,
// `]`, `=` and `,` are real syntax here, not an attempt to smuggle something
// through. None of it is a shell hazard: this becomes one argv element via
// exec.Command, never text a parser re-reads. Only the leading-dash rule that
// applies everywhere in this package still applies.
var cursorModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-\[\]=,]{0,127}$`)

// cursorSessionID is deliberately permissive — cursor-agent documents no fixed
// shape for a chatId — short of the two things that matter: it cannot begin
// with a dash (an operand that becomes a flag), and it is bounded.
var cursorSessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// cursorMCPRelPath is the one place cursor-agent reads MCP servers from,
// relative to the workspace — see the file header.
const cursorMCPRelPath = ".cursor/mcp.json"

type cursorRunParams struct {
	// Workspace is relative to the workspace folder, exactly as claude.run's
	// and opencode.run's are.
	Workspace string `json:"workspace"`
	// Prompt is an argv element for this call alone — see the file header.
	Prompt    string            `json:"prompt"`
	Model     string            `json:"model,omitempty"`
	Resume    string            `json:"resume,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
	MCP       *mcpParams        `json:"mcp,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type cursorRunRequest struct {
	cursorRunParams
	ID string `json:"id,omitempty"`
}

type cursorRunResult struct {
	Workspace  string `json:"workspace"`
	ExitCode   int    `json:"exit_code"`
	Signal     string `json:"signal,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type preparedCursorRun struct {
	dir     string
	args    []string
	prompt  string
	env     []string
	mcp     *mcpConfig
	timeout time.Duration
}

func (s *runnerServer) prepareCursorRun(p cursorRunParams) (preparedCursorRun, *rpcError) {
	if p.Prompt == "" {
		return preparedCursorRun{}, failure(codeBadRequest, "prompt is required")
	}
	if s.cfg.cursorAgentBin == "" {
		return preparedCursorRun{}, failure(codeNotReady, "this machine has no Cursor CLI (cursor_agent_bin was not sent)")
	}
	if _, err := launcherFor(s.cfg.cursorAgentBin); err != nil {
		return preparedCursorRun{}, failure(codeNotReady, "%v", err)
	}

	dir, err := resolveInWorkspace(s.cfg.workspaceDir, p.Workspace)
	if err != nil {
		return preparedCursorRun{}, failure(codeBadRequest, "workspace: %v", err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return preparedCursorRun{}, failure(codeBadRequest,
			"workspace %q is not a directory on this machine; call workspace.prepare or workspace.ensure first", p.Workspace)
	}

	// -p is --print (a boolean; cursor-agent's own --help spells it out), not
	// a value-taking flag — the prompt is a separate, later, positional
	// argument. --force is the same unattended-approval role --auto plays for
	// opencode: there is no human at this terminal to answer a permission
	// prompt.
	args := []string{"-p", "--force", "--output-format", "stream-json"}
	if p.Model != "" {
		if !cursorModelName.MatchString(p.Model) {
			return preparedCursorRun{}, failure(codeBadRequest, "model %q is not a model name this runner will use", p.Model)
		}
		args = append(args, "--model", p.Model)
	}
	if p.Resume != "" {
		if !cursorSessionID.MatchString(p.Resume) {
			return preparedCursorRun{}, failure(codeBadRequest, "resume %q is not a session id this runner will use", p.Resume)
		}
		args = append(args, "--resume", p.Resume)
	}

	mcpCfg, mcpErr := checkMCP(p.MCP)
	if mcpErr != nil {
		return preparedCursorRun{}, mcpErr
	}

	env, envErr := checkEnv(p.Env)
	if envErr != nil {
		return preparedCursorRun{}, envErr
	}

	return preparedCursorRun{
		dir:     dir,
		args:    args,
		prompt:  p.Prompt,
		env:     env,
		mcp:     mcpCfg,
		timeout: time.Duration(p.TimeoutMS) * time.Millisecond,
	}, nil
}

// writeCursorMCP merges the run's MCP server into the workspace's own
// .cursor/mcp.json and returns the restore that undoes exactly that — see the
// file header for why this is the one on-disk credential in this package that
// lives inside the workspace root rather than beside it.
//
// The restore is ALWAYS non-nil, including on the error paths, mirroring
// writeMCPRun in mcp.go: a half-written file is still a file with a token in
// it, and the caller defers this before it looks at the error.
func writeCursorMCP(dir string, m *mcpConfig) (func(), *rpcError) {
	noop := func() {}
	if m == nil {
		return noop, nil
	}

	path := filepath.Join(dir, cursorMCPRelPath)
	original, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return noop, failure(codeInternal, "could not read the workspace's %s: %v", cursorMCPRelPath, readErr)
	}

	doc := map[string]any{}
	if existed {
		if err := json.Unmarshal(original, &doc); err != nil {
			// Refused rather than overwritten: a file this program cannot
			// parse back is a file it cannot restore, and guessing at
			// somebody's broken JSON is how a developer's real config gets
			// replaced with ours permanently.
			return noop, failure(codeBadRequest, "the workspace's %s is not valid JSON; this runner will not overwrite a file it cannot restore", cursorMCPRelPath)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[m.serverName] = map[string]any{
		"url":     m.url,
		"headers": map[string]string{"Authorization": "Bearer " + m.token},
	}
	doc["mcpServers"] = servers

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return noop, failure(codeInternal, "could not encode %s: %v", cursorMCPRelPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return noop, failure(codeInternal, "could not create the workspace's .cursor directory: %v", err)
	}
	if !existed {
		// Written before the token lands: a runner killed mid-run skips the
		// restore below, and an agent's own `git add -A` must never be what
		// carries the bearer token into the repository's history.
		excludeFromGit(dir, "/"+cursorMCPRelPath)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return noop, failure(codeInternal, "could not write %s: %v", cursorMCPRelPath, err)
	}

	restore := func() {
		if !existed {
			if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				log.Error().Err(rmErr).Str("path", path).Msg("could not remove the cursor MCP config this run wrote; a token may still be in the workspace")
			}
			return
		}
		if wErr := os.WriteFile(path, original, 0o600); wErr != nil {
			log.Error().Err(wErr).Str("path", path).Msg("could not restore the workspace's cursor MCP config; a token may still be in it")
		}
	}
	return restore, nil
}

func (s *runnerServer) handleCursorRun(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}

	var req cursorRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}

	prepared, prepErr := s.prepareCursorRun(req.cursorRunParams)
	if prepErr != nil {
		writeError(w, prepErr)
		return
	}

	id := req.ID
	if id == "" {
		id = newCallID()
	}

	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new runs"))
		return
	}
	defer s.leaveRun()

	run, releaseSlot, queued := s.queueDurable(w, r, id, "cursor.run")
	if !queued {
		return
	}

	// Written only once a slot is free, matching claude.run's — a call queued
	// behind fifteen others has no business holding a bearer token merged
	// into the workspace's own config file while it waits — and restored when
	// the run ends, which may be long after this request does.
	surface := s.openCLISurface(run.ctx, id, req.Workspace, prepared.mcp, surfaceToolPolicy(prepared.mcp, nil), req.Env, prepared.timeout)
	restoreMCP, mcpErr := writeCursorMCP(prepared.dir, surface.mcp)
	if mcpErr != nil {
		restoreMCP()
		surface.close()
		releaseSlot()
		run.abandon()
		writeError(w, mcpErr)
		return
	}

	s.serveDurable(w, r, run, []func(){releaseSlot, surface.close, restoreMCP}, func(runCtx context.Context, frames io.Writer) {
		c := &call{
			ctx:    runCtx,
			id:     id,
			cfg:    s.cfg,
			state:  s.state,
			params: body,
			w:      frames,
			// cursor-agent inherits this process's environment unmodified too
			// — see spawnCursor — so the same transcript-side scrubbing
			// claude.run and opencode.run get applies here. See redact.go.
			redact: heldSecretRedactor(s.cfg.policy, surface.held()),
		}
		_ = c.emit(startedEvent{V: protocolVersion, ID: id, Event: "started"})

		spawnCtx := runCtx
		if prepared.timeout > 0 {
			var stopTimeout context.CancelFunc
			spawnCtx, stopTimeout = context.WithTimeout(runCtx, prepared.timeout)
			defer stopTimeout()
		}

		result, callErr := spawnCursor(spawnCtx, c, prepared.dir, prepared.args, prepared.prompt, prepared.env)
		if callErr == nil && runCtx.Err() != nil {
			callErr = failure(codeCancelled, "the call was cancelled")
		}
		c.finish(result, callErr)
	})
}

// spawnCursor runs one cursor-agent session to completion, streaming its
// output. Structurally identical to spawnClaude and spawnOpencode — same
// process-group kill on cancellation, same drain-safe reap, forward and
// killProcessGroup shared verbatim — except there is no stdin goroutine: the
// prompt already went into argv, appended here (never earlier) with a literal
// `--` immediately before it so cursor-agent's own parser cannot read a
// leading `-` in the task's text as an option. See the file header.
func spawnCursor(ctx context.Context, c *call, dir string, args []string, prompt string, env []string) (any, *rpcError) {
	argv := make([]string, 0, len(args)+2)
	argv = append(argv, args...)
	argv = append(argv, "--", prompt)

	cmd, err := commandFor(c.cfg.cursorAgentBin, argv...)
	if err != nil {
		return nil, failure(codeNotReady, "%v", err)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	// cursor-agent has no stdin-prompt mode (see the file header); leaving
	// Stdin nil gives it the null device rather than this process's own,
	// which it has no business reading from.

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
		return nil, failure(codeInternal, "could not start %s: %v", c.cfg.cursorAgentBin, err)
	}
	defer group.release()
	log.Info().Str("call", c.id).Int("pid", cmd.Process.Pid).Str("dir", dir).Msg("cursor session started")

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
			close(killed)
			killProcessGroup(group, claudeGrace, exited)
		case <-exited:
		}
	}()

	streams.Wait()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var runErr error
	select {
	case runErr = <-waitErr:
	case <-time.After(claudeReapTimeout):
		close(exited)
		return nil, failure(codeInternal, "the Cursor session would not exit %s after being killed", claudeReapTimeout)
	}
	close(exited)

	result := cursorRunResult{Workspace: dir, DurationMS: time.Since(started).Milliseconds()}
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
		return nil, failure(codeInternal, "waiting for the Cursor session: %v", runErr)
	}

	select {
	case <-killed:
		if ctx.Err() != nil && c.ctx.Err() == nil {
			return nil, failure(codeCancelled, "the session passed its %dms limit and was stopped", time.Since(started).Milliseconds())
		}
	default:
	}

	log.Info().Str("call", c.id).Int("exit", result.ExitCode).Dur("took", time.Since(started)).Msg("cursor session finished")
	return result, nil
}

// excludeFromGit adds pattern to the checkout's .git/info/exclude once. It is
// best effort: a workspace that is not a plain git checkout (no .git
// directory, e.g. a linked worktree) is left alone and only the restore
// protects it.
func excludeFromGit(dir, pattern string) {
	infoDir := filepath.Join(dir, ".git", "info")
	if st, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !st.IsDir() {
		return
	}
	excludePath := filepath.Join(infoDir, "exclude")
	current, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Str("path", excludePath).Msg("could not read git exclude; the cursor MCP config is protected only by its restore")
		return
	}
	for _, line := range strings.Split(string(current), "\n") {
		if strings.TrimSpace(line) == pattern {
			return
		}
	}
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		log.Warn().Err(err).Str("path", infoDir).Msg("could not create .git/info")
		return
	}
	prefix := ""
	if len(current) > 0 && !strings.HasSuffix(string(current), "\n") {
		prefix = "\n"
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Warn().Err(err).Str("path", excludePath).Msg("could not open git exclude")
		return
	}
	defer f.Close()
	if _, err := f.WriteString(prefix + pattern + "\n"); err != nil {
		log.Warn().Err(err).Str("path", excludePath).Msg("could not write git exclude")
	}
}
