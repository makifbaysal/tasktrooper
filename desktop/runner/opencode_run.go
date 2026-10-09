package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// opencode.run — a headless OpenCode session, streamed.
//
// The shape is claude.run's: refuse everything refusable before the 200,
// stream one JSON object per line as it arrives, kill the process GROUP on
// cancellation, never put the prompt in argv. What differs is smaller than it
// looks:
//
//   - No --output-format flag to choose: `--format json` is opencode's own
//     name for the same thing claude.run asks with --output-format
//     stream-json, and this side does not parse it — agent-server's
//     adapter/agentcli/opencode package already owns that parser for the
//     local path, and it is reused for this one rather than read twice.
//   - No MCP file. opencode reads inline config from ONE environment
//     variable, OPENCODE_CONFIG_CONTENT, so there is no per-run file to write
//     under mcpRoot and nothing to sweep on a SIGKILL — see opencodeMCPEnv.
//     On 2.x that variable only reaches a server started with it, so the run
//     gets a private one — see opencode_server.go.
//   - No --tools/--allowedTools split: opencode has one blanket permission
//     flag, --auto, sent unconditionally (the run has no human at this
//     terminal to approve edits) — the tool-policy narrowing this run gets is
//     entirely the MCP server's, same as every other host-executed CLI that
//     has no native-tool allowlist of its own.
//   - Resume is `-s <id>` rather than claude's --resume, confirmed against a
//     real opencode session this Mac ran: a second `opencode run` with -s and
//     no positional message continued the first turn's context (cache-read
//     tokens on the second call's step_finish event were non-zero), so this
//     is a verified capability, not a guess from --help text.
//   - `model` is REQUIRED and must name a provider: OpenCode has no curated
//     default the way claude_code's model list does, and a run that fell back
//     to whatever OpenCode picked would be a run this Mac cannot account for.
//     `anthropic/…` and `google/…` are refused before the 200 by default,
//     whatever the case (policy.go): a Claude subscription authenticates
//     `claude` and nothing else, Gemini subscription use is off, and
//     OpenCode reads `~/.claude`'s and Google's own stored credentials as
//     readily as it reads an API key, so this is enforced on the model name
//     rather than trusted to whatever credential happens to be configured.

// opencodeModelName allows the CLI's own "provider/model" shape (see `opencode
// models`, e.g. "opencode/big-pickle") — one slash, unlike claude's alias-only
// modelName grammar.
var opencodeModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// opencodeSessionID is the CLI's own id shape (e.g. "ses_fa28a1f76ffec30xqZz11TwLsX"),
// observed directly rather than documented — opencode --help names no format
// for it.
var opencodeSessionID = regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`)

type opencodeRunParams struct {
	// Workspace is relative to the workspace folder, exactly as claude.run's
	// is — usually workspace.prepare's `rel`, or workspace.ensure's for a
	// chat that has no repository.
	Workspace string `json:"workspace"`
	// Prompt travels on stdin, never in argv — see the package doc.
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
	// Resume continues a prior opencode session by its own id (-s). Empty
	// starts a fresh one.
	Resume    string            `json:"resume,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
	MCP       *mcpParams        `json:"mcp,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

type opencodeRunRequest struct {
	opencodeRunParams
	ID string `json:"id,omitempty"`
}

type opencodeRunResult struct {
	Workspace  string `json:"workspace"`
	ExitCode   int    `json:"exit_code"`
	Signal     string `json:"signal,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type preparedOpencodeRun struct {
	dir    string
	args   []string
	prompt string
	env    []string
	// mcp is rendered at launch, once the CLI's generation decides where the
	// config goes: the run's own env (1.x) or its private server's (2.x).
	mcp     *mcpConfig
	timeout time.Duration
}

func (s *runnerServer) prepareOpencodeRun(p opencodeRunParams) (preparedOpencodeRun, *rpcError) {
	if p.Prompt == "" {
		return preparedOpencodeRun{}, failure(codeBadRequest, "prompt is required")
	}
	if s.cfg.opencodeBin == "" {
		return preparedOpencodeRun{}, failure(codeNotReady, "this machine has no OpenCode CLI (opencode_bin was not sent)")
	}
	if _, err := launcherFor(s.cfg.opencodeBin); err != nil {
		return preparedOpencodeRun{}, failure(codeNotReady, "%v", err)
	}

	dir, err := resolveInWorkspace(s.cfg.workspaceDir, p.Workspace)
	if err != nil {
		return preparedOpencodeRun{}, failure(codeBadRequest, "workspace: %v", err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return preparedOpencodeRun{}, failure(codeBadRequest,
			"workspace %q is not a directory on this machine; call workspace.prepare or workspace.ensure first", p.Workspace)
	}

	args := []string{"run", "--format", "json", "--auto"}
	// Required, and checked in "provider/model" form BEFORE the argument is
	// trusted to name a real provider — see the file header on why anthropic
	// and google are refused rather than merely discouraged.
	if p.Model == "" {
		return preparedOpencodeRun{}, failure(codeBadRequest,
			`model is required and must be "provider/model" (e.g. "openai/gpt-4.1", "opencode/big-pickle", "github-copilot/claude-sonnet-4.5", "openrouter/…"); OpenCode has no default this runner will fall back to`)
	}
	if !opencodeModelName.MatchString(p.Model) {
		return preparedOpencodeRun{}, failure(codeBadRequest, "model %q is not a model name this runner will use", p.Model)
	}
	provider, _, hasProvider := strings.Cut(p.Model, "/")
	if !hasProvider || provider == "" {
		return preparedOpencodeRun{}, failure(codeBadRequest, `model %q must be in "provider/model" form`, p.Model)
	}
	if refusal := s.cfg.policy.opencodeRefusal(p.Model, provider); refusal != nil {
		return preparedOpencodeRun{}, refusal
	}
	args = append(args, "-m", p.Model)
	if p.Resume != "" {
		if !opencodeSessionID.MatchString(p.Resume) {
			return preparedOpencodeRun{}, failure(codeBadRequest, "resume %q is not a session id this runner will use", p.Resume)
		}
		args = append(args, "-s", p.Resume)
	}

	mcpCfg, mcpErr := checkMCP(p.MCP)
	if mcpErr != nil {
		return preparedOpencodeRun{}, mcpErr
	}

	env, envErr := checkEnv(p.Env)
	if envErr != nil {
		return preparedOpencodeRun{}, envErr
	}

	prepared := preparedOpencodeRun{
		dir:     dir,
		args:    args,
		prompt:  p.Prompt,
		env:     env,
		mcp:     mcpCfg,
		timeout: time.Duration(p.TimeoutMS) * time.Millisecond,
	}
	return prepared, nil
}

// opencodeMCPEnv renders the one environment variable opencode reads its
// inline config from — OPENCODE_CONFIG_CONTENT, documented by opencode.ai as
// JSON merged over the project's own opencode.json at startup. cfg has
// already been through checkMCP's grammar (https, absolute, no userinfo, a
// non-empty token, a plain-identifier server name), so nothing here re-checks
// it — only shapes it into the "type":"remote" server entry the CLI expects.
func opencodeMCPEnv(cfg *mcpConfig) (string, error) {
	doc := map[string]any{
		"mcp": map[string]any{
			cfg.serverName: map[string]any{
				"type": "remote",
				"url":  cfg.url,
				"headers": map[string]string{
					"Authorization": "Bearer " + cfg.token,
				},
			},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *runnerServer) handleOpencodeRun(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}

	var req opencodeRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}

	prepared, prepErr := s.prepareOpencodeRun(req.opencodeRunParams)
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

	run, releaseSlot, queued := s.queueDurable(w, r, id, "opencode.run")
	if !queued {
		return
	}
	surface := s.openCLISurface(run.ctx, id, req.Workspace, prepared.mcp, surfaceToolPolicy(prepared.mcp, nil), req.Env, prepared.timeout)
	prepared.mcp = surface.mcp

	s.serveDurable(w, r, run, []func(){releaseSlot, surface.close}, func(runCtx context.Context, frames io.Writer) {
		c := &call{
			ctx:    runCtx,
			id:     id,
			cfg:    s.cfg,
			state:  s.state,
			params: body,
			w:      frames,
			// opencode inherits this process's environment unmodified too —
			// see spawnOpencode — so the same transcript-side scrubbing
			// claude.run gets applies here. See redact.go.
			redact: heldSecretRedactor(s.cfg.policy, surface.held()),
		}
		_ = c.emit(startedEvent{V: protocolVersion, ID: id, Event: "started"})

		spawnCtx := runCtx
		if prepared.timeout > 0 {
			var stopTimeout context.CancelFunc
			spawnCtx, stopTimeout = context.WithTimeout(runCtx, prepared.timeout)
			defer stopTimeout()
		}

		launch, launchErr := s.launchOpencode(spawnCtx, id, prepared)
		if launchErr != nil {
			c.finish(nil, launchErr)
			return
		}
		defer launch.stop()

		result, callErr := spawnOpencode(spawnCtx, c, prepared.dir, launch.args, prepared.prompt, launch.env)
		if callErr == nil && runCtx.Err() != nil {
			callErr = failure(codeCancelled, "the call was cancelled")
		}
		c.finish(result, callErr)
	})
}

// spawnOpencode runs one opencode session to completion, streaming its
// output. Structurally identical to spawnClaude — same stdin-prompt rule,
// same process-group kill on cancellation, same drain-safe reap — because
// none of that is claude-specific; it is what running ANY long-lived CLI
// session over this tunnel safely requires. forward and killProcessGroup are
// shared verbatim rather than copied.
func spawnOpencode(ctx context.Context, c *call, dir string, args []string, prompt string, env []string) (any, *rpcError) {
	cmd, err := commandFor(c.cfg.opencodeBin, args...)
	if err != nil {
		return nil, failure(codeNotReady, "%v", err)
	}
	cmd.Dir = dir
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
		return nil, failure(codeInternal, "could not start %s: %v", c.cfg.opencodeBin, err)
	}
	defer group.release()
	log.Info().Str("call", c.id).Int("pid", cmd.Process.Pid).Str("dir", dir).Msg("opencode session started")

	// `opencode run` with no positional message reads stdin — verified against
	// the installed CLI, not inferred from --help.
	go func() {
		defer func() { _ = stdin.Close() }()
		if _, err := io.WriteString(stdin, prompt); err != nil {
			log.Warn().Str("call", c.id).Err(err).Msg("could not write the prompt")
		}
	}()

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
		return nil, failure(codeInternal, "the OpenCode session would not exit %s after being killed", claudeReapTimeout)
	}
	close(exited)

	result := opencodeRunResult{Workspace: dir, DurationMS: time.Since(started).Milliseconds()}
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
		return nil, failure(codeInternal, "waiting for the OpenCode session: %v", runErr)
	}

	select {
	case <-killed:
		if ctx.Err() != nil && c.ctx.Err() == nil {
			return nil, failure(codeCancelled, "the session passed its %dms limit and was stopped", time.Since(started).Milliseconds())
		}
	default:
	}

	log.Info().Str("call", c.id).Int("exit", result.ExitCode).Dur("took", time.Since(started)).Msg("opencode session finished")
	return result, nil
}
