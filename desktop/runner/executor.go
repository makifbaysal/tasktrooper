package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// The local executor: agent-server's headless mode (server/cmd/executor),
// started, watched and restarted by this runner, and the process
// `agent.run` and `llm.complete` are forwarded to.
//
// This runner is what the cloud reaches; the executor is what runs an agent
// with the member's own API keys. The keys come from the desktop app on this
// runner's stdin, go to the executor on ITS stdin, and live in memory in both.
// They never travel over the tunnel, never land in argv, an environment block
// or a file, and are scrubbed out of everything forwarded back.
//
// The executor is the one loopback listener in this picture, and it is not
// this program's: rules_test.go still forbids net.Listen here. This side only
// dials the address the executor printed, after checking it is loopback.

// executorProtocol is the executor wire version this runner speaks. A
// different answer from `GET /exec/health` is a build mismatch, and retrying
// the same binary cannot fix it.
const executorProtocol = 1

const executorListeningPrefix = "EXECUTOR_LISTENING "

// executorTokenBytes is the size of the per-start bearer, before hex.
const executorTokenBytes = 32

// executorResponseLimit bounds a one-document answer (llm.complete, an error
// body). A completion is text, not megabytes.
const executorResponseLimit = 8 * 1024 * 1024

// executorCancelTimeout bounds the best-effort `POST /exec/cancel` sent when a
// forwarded run's caller goes away. Short, because a drain is waiting on it.
const executorCancelTimeout = 3 * time.Second

// executorSetTimeout bounds the best-effort `POST /exec/embeddings.set` sent
// when the embedder moves under a running executor.
const executorSetTimeout = 3 * time.Second

type executorTimings struct {
	// listenTimeout bounds the wait for the EXECUTOR_LISTENING line.
	listenTimeout time.Duration
	// readyWait is how long a call waits for an executor that is starting or
	// restarting before it is answered not_ready.
	readyWait time.Duration
	// stopGrace is between closing the executor's stdin and killing its
	// group. With the kill's own grace it stays inside the supervisor's
	// SIGTERM grace for this runner, because it runs beside the drain.
	stopGrace   time.Duration
	minBackoff  time.Duration
	maxBackoff  time.Duration
	stableAfter time.Duration
}

var defaultExecutorTimings = executorTimings{
	listenTimeout: 30 * time.Second,
	readyWait:     20 * time.Second,
	stopGrace:     15 * time.Second,
	minBackoff:    time.Second,
	maxBackoff:    30 * time.Second,
	stableAfter:   60 * time.Second,
}

// providerConfig is one of the member's own LLM providers, exactly as the
// executor contract names it. APIKey is a secret: it is never logged and is
// scrubbed out of every byte forwarded from the executor.
type providerConfig struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	BaseURL string   `json:"base_url,omitempty"`
	APIKey  string   `json:"api_key,omitempty"`
	Models  []string `json:"models,omitempty"`
	// TimeoutSeconds is the executor's per-provider request timeout; 0 keeps
	// its default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// executorConfig is the one JSON line the executor reads on stdin. stdin then
// stays open, and its closing is the executor's shutdown request.
type executorConfig struct {
	Listen            string           `json:"listen"`
	Token             string           `json:"token"`
	DataDir           string           `json:"data_dir"`
	WorkspaceRoot     string           `json:"workspace_root"`
	EmbeddingsBaseURL string           `json:"embeddings_base_url,omitempty"`
	PostgresCacheDir  string           `json:"postgres_cache_dir,omitempty"`
	Providers         []providerConfig `json:"providers"`
}

const maxProviders = 64

var (
	providerIdent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	agentRunID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

// checkProviders validates what the desktop app sent. A key may be empty (a
// local model server needs none); a base URL that is not loopback must be
// https, because the key travels to it.
func checkProviders(in []providerConfig) ([]providerConfig, error) {
	if len(in) > maxProviders {
		return nil, fmt.Errorf("providers: %d entries, at most %d", len(in), maxProviders)
	}
	out := make([]providerConfig, 0, len(in))
	seen := make(map[string]bool, len(in))
	for i, p := range in {
		where := fmt.Sprintf("providers[%d]", i)
		if !providerIdent.MatchString(p.ID) {
			return nil, fmt.Errorf("%s.id %q is not a provider id", where, p.ID)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("%s.id %q appears twice", where, p.ID)
		}
		seen[p.ID] = true
		if !providerIdent.MatchString(p.Type) {
			return nil, fmt.Errorf("%s.type %q is not a provider type", where, p.Type)
		}
		if providerControl.MatchString(p.APIKey) {
			return nil, fmt.Errorf("%s.api_key contains control characters", where)
		}
		if p.BaseURL != "" {
			if err := checkProviderBaseURL(p.BaseURL); err != nil {
				return nil, fmt.Errorf("%s.base_url: %w", where, err)
			}
		}
		if p.TimeoutSeconds < 0 || p.TimeoutSeconds > 3600 {
			return nil, fmt.Errorf("%s.timeout_seconds %d is out of range 0..3600", where, p.TimeoutSeconds)
		}
		for j, m := range p.Models {
			if m == "" || len(m) > 256 || providerControl.MatchString(m) {
				return nil, fmt.Errorf("%s.models[%d] is not a model name", where, j)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// providerControl matches what must never be inside a value that becomes a
// header, a JSON line on a pipe or a log field.
var providerControl = regexp.MustCompile(`[\x00-\x1f\x7f]`)

func checkProviderBaseURL(raw string) error {
	if checkLoopbackURL(raw) == nil {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return errors.New("must be https, or http to loopback: the provider's key is sent to it")
	}
	if u.User != nil {
		return errors.New("must not carry credentials in the URL")
	}
	return nil
}

// providerSecrets is every key the executor holds, labelled for the
// redaction marker.
func providerSecrets(providers []providerConfig) map[string]string {
	out := make(map[string]string, len(providers))
	for _, p := range providers {
		if p.APIKey != "" {
			out["PROVIDER_API_KEY:"+p.ID] = p.APIKey
		}
	}
	return out
}

// errExecutorProtocol is a start that answered with a protocol this runner
// does not speak. Fatal: the loop stops restarting.
var errExecutorProtocol = errors.New("executor protocol mismatch")

// executorSupervisor owns one executor process at a time: start, readiness,
// restart on exit with a capped backoff, and the stop. It belongs to `state`,
// not to a tunnel session, so a thirty-second reconnect does not restart it.
type executorSupervisor struct {
	bin              string
	dataDir          string
	postgresCacheDir string
	workspace        string
	providers        []providerConfig
	embeddings       func() string
	t                executorTimings
	client           *http.Client
	scrub            func([]byte) []byte

	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool
	done    chan struct{}

	mu      sync.Mutex
	cur     *executorProcess
	changed chan struct{}
	lastErr string
	fatal   bool
}

type executorProcess struct {
	group    *processGroup
	stdin    io.WriteCloser
	base     string
	token    string
	exited   chan struct{}
	exitErr  error
	out      *executorOutput
	stopping atomic.Bool
}

// newExecutorSupervisor returns nil when the desktop app sent no executor:
// agent.run and llm.complete then answer not_ready, the way mobile.* answers
// for a capability this machine lacks.
func newExecutorSupervisor(cfg config, embeddings func() string, t executorTimings) *executorSupervisor {
	if cfg.executorBin == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &executorSupervisor{
		bin:              cfg.executorBin,
		dataDir:          cfg.executorDataDir,
		postgresCacheDir: cfg.executorPostgresCacheDir,
		workspace:        cfg.workspaceDir,
		providers:        cfg.providers,
		embeddings:       embeddings,
		t:                t,
		client:           loopbackClient(),
		scrub:            heldSecretRedactor(cfg.policy, providerSecrets(cfg.providers)),
		ctx:              ctx,
		cancel:           cancel,
		done:             make(chan struct{}),
		changed:          make(chan struct{}),
	}
}

// loopbackClient never follows a redirect and never uses a proxy: the only
// host it is pointed at is the loopback address the executor printed, and
// neither may move the request anywhere else.
func loopbackClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 8, IdleConnTimeout: time.Minute},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (e *executorSupervisor) start() {
	if e == nil || !e.started.CompareAndSwap(false, true) {
		return
	}
	go e.loop()
}

// close stops the executor and the restart loop. Safe to call more than once;
// every caller returns once the executor is down.
func (e *executorSupervisor) close() {
	if e == nil {
		return
	}
	e.cancel()
	if e.started.Load() {
		<-e.done
	}
}

func (e *executorSupervisor) loop() {
	defer close(e.done)
	backoff := e.t.minBackoff
	for e.ctx.Err() == nil {
		p, err := e.spawn()
		if err != nil {
			if e.ctx.Err() != nil {
				return
			}
			e.setFailure(err, errors.Is(err, errExecutorProtocol))
			if errors.Is(err, errExecutorProtocol) {
				log.Error().Err(err).Msg("the local executor speaks another protocol; not restarting it")
				return
			}
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("the local executor did not start")
		} else {
			readyAt := time.Now()
			e.publish(p)
			log.Info().Str("base", p.base).Msg("local executor ready")
			select {
			case <-p.exited:
			case <-e.ctx.Done():
				e.unpublish(p)
				p.stop(e.t)
				return
			}
			e.unpublish(p)
			if time.Since(readyAt) >= e.t.stableAfter {
				backoff = e.t.minBackoff
			}
			exit := fmt.Errorf("the local executor exited (%v)%s", p.exitErr, p.out.suffix())
			e.setFailure(exit, false)
			log.Warn().Err(exit).Dur("retry_in", backoff).Msg("the local executor exited; restarting it")
		}
		select {
		case <-time.After(backoff):
		case <-e.ctx.Done():
			return
		}
		backoff = min(backoff*2, e.t.maxBackoff)
	}
}

func (e *executorSupervisor) publish(p *executorProcess) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cur = p
	e.lastErr = ""
	close(e.changed)
	e.changed = make(chan struct{})
}

func (e *executorSupervisor) unpublish(p *executorProcess) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cur == p {
		e.cur = nil
	}
}

func (e *executorSupervisor) setFailure(err error, fatal bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastErr = err.Error()
	if fatal {
		e.fatal = true
	}
	close(e.changed)
	e.changed = make(chan struct{})
}

// ready is the executor a call may use, waiting up to readyWait for one that
// is starting. Never spawns anything itself: the loop does.
func (e *executorSupervisor) ready(ctx context.Context) (*executorProcess, *rpcError) {
	deadline := time.NewTimer(e.t.readyWait)
	defer deadline.Stop()
	for {
		e.mu.Lock()
		cur, changed, lastErr, fatal := e.cur, e.changed, e.lastErr, e.fatal
		e.mu.Unlock()
		if cur != nil {
			return cur, nil
		}
		if fatal {
			return nil, failure(codeNotReady, "the local executor cannot run on this machine: %s", lastErr)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, failure(codeCancelled, "the call was cancelled while the local executor was starting")
		case <-e.ctx.Done():
			return nil, failure(codeCancelled, "this runner is shutting down")
		case <-deadline.C:
			if lastErr != "" {
				return nil, failure(codeNotReady, "the local executor is not running: %s", lastErr)
			}
			return nil, failure(codeNotReady, "the local executor did not become ready within %s", e.t.readyWait)
		}
	}
}

func (e *executorSupervisor) spawn() (*executorProcess, error) {
	if err := os.MkdirAll(e.dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the executor's data directory: %w", err)
	}
	token, err := randomHex(executorTokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generating the executor's token: %w", err)
	}
	providers := e.providers
	if providers == nil {
		providers = []providerConfig{}
	}
	line, err := json.Marshal(executorConfig{
		Listen:            "127.0.0.1:0",
		Token:             token,
		DataDir:           e.dataDir,
		WorkspaceRoot:     e.workspace,
		EmbeddingsBaseURL: e.embeddings(),
		PostgresCacheDir:  e.postgresCacheDir,
		Providers:         providers,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the executor's config: %w", err)
	}

	cmd, err := commandFor(e.bin)
	if err != nil {
		return nil, err
	}
	// The executor runs the agent's local tools, which need what any child of
	// this runner needs; the keys are NOT in here — they are on its stdin.
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out := newExecutorOutput(e.scrub)
	cmd.Stdout = out
	cmd.Stderr = out.stderr()
	// A tool's own children inherit these pipes, and Wait would otherwise hold
	// out for the last of them.
	cmd.WaitDelay = e.t.stopGrace
	group, err := startProcessGroup(cmd)
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", e.bin, err)
	}
	p := &executorProcess{group: group, stdin: stdin, token: token, exited: make(chan struct{}), out: out}
	go func() {
		p.exitErr = cmd.Wait()
		group.release()
		close(p.exited)
	}()

	if _, err := stdin.Write(append(line, '\n')); err != nil {
		p.stop(e.t)
		return nil, fmt.Errorf("handing the executor its config: %w%s", err, out.suffix())
	}

	listen := time.NewTimer(e.t.listenTimeout)
	defer listen.Stop()
	select {
	case base := <-out.listening:
		if err := checkLoopbackURL(base); err != nil {
			p.stop(e.t)
			return nil, fmt.Errorf("the executor listens on %q: %w", base, err)
		}
		p.base = strings.TrimRight(base, "/")
	case <-p.exited:
		return nil, fmt.Errorf("the executor exited before it said where it listens (%v)%s", p.exitErr, out.suffix())
	case <-listen.C:
		p.stop(e.t)
		return nil, fmt.Errorf("the executor did not print %q within %s%s", strings.TrimSpace(executorListeningPrefix), e.t.listenTimeout, out.suffix())
	case <-e.ctx.Done():
		p.stop(e.t)
		return nil, e.ctx.Err()
	}

	if err := e.checkHealth(p); err != nil {
		p.stop(e.t)
		return nil, err
	}
	return p, nil
}

type executorHealth struct {
	OK       bool   `json:"ok"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

func (e *executorSupervisor) checkHealth(p *executorProcess) error {
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/exec/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("the executor's health check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var h executorHealth
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&h); err != nil {
		return fmt.Errorf("the executor's health answer (HTTP %d) is not the expected document: %w", resp.StatusCode, err)
	}
	if h.Protocol != executorProtocol {
		return fmt.Errorf("%w: it speaks %d, this runner speaks %d (executor %s)", errExecutorProtocol, h.Protocol, executorProtocol, h.Version)
	}
	if resp.StatusCode != http.StatusOK || !h.OK {
		return fmt.Errorf("the executor reports itself unhealthy (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// stop asks first — stdin EOF is the executor's shutdown request, on every
// OS — and kills the group only when the ask was not enough.
func (p *executorProcess) stop(t executorTimings) {
	p.stopping.Store(true)
	_ = p.stdin.Close()
	select {
	case <-p.exited:
		return
	case <-time.After(t.stopGrace):
	}
	log.Warn().Dur("grace", t.stopGrace).Msg("the local executor did not stop after its stdin closed; killing it")
	killProcessGroup(p.group, claudeGrace, p.exited)
	select {
	case <-p.exited:
	case <-time.After(claudeReapTimeout):
		log.Warn().Msg("the local executor would not exit after being killed")
	}
}

// executorOutput turns the executor's stdout into the one line this side
// waits for and log records for the rest. Every line it logs is scrubbed of
// the provider keys first: the executor should never print one, and if it
// does, this log is where somebody would paste it from.
type executorOutput struct {
	scrub     func([]byte) []byte
	listening chan string

	mu    sync.Mutex
	buf   []byte
	found bool
	tail  []string
}

const executorTailLines = 5

func newExecutorOutput(scrub func([]byte) []byte) *executorOutput {
	return &executorOutput{scrub: scrub, listening: make(chan string, 1)}
}

func (o *executorOutput) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, b...)
	for {
		i := bytes.IndexByte(o.buf, '\n')
		if i < 0 {
			if len(o.buf) > outputLineLimit {
				o.line(o.buf, "stdout")
				o.buf = nil
			}
			return len(b), nil
		}
		o.line(o.buf[:i], "stdout")
		o.buf = o.buf[i+1:]
	}
}

func (o *executorOutput) line(raw []byte, stream string) {
	text := strings.TrimRight(string(raw), "\r")
	if stream == "stdout" && !o.found && strings.HasPrefix(text, executorListeningPrefix) {
		o.found = true
		o.listening <- strings.TrimSpace(strings.TrimPrefix(text, executorListeningPrefix))
		return
	}
	if text == "" {
		return
	}
	if o.scrub != nil {
		text = string(o.scrub([]byte(text)))
	}
	o.tail = append(o.tail, clipLine(text, 400))
	if len(o.tail) > executorTailLines {
		o.tail = o.tail[len(o.tail)-executorTailLines:]
	}
	log.Debug().Str("stream", stream).Str("line", text).Msg("executor")
}

// stderr is the same sink for the other stream: logged and kept for the tail,
// never searched for the listening line.
func (o *executorOutput) stderr() io.Writer { return executorStderr{o} }

type executorStderr struct{ o *executorOutput }

func (s executorStderr) Write(b []byte) (int, error) {
	s.o.mu.Lock()
	defer s.o.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		s.o.line([]byte(l), "stderr")
	}
	return len(b), nil
}

func (o *executorOutput) suffix() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.tail) == 0 {
		return ""
	}
	return "; its last output: " + strings.Join(o.tail, " | ")
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// --- the two forwarded methods ----------------------------------------------

// agentRunHead is what this side reads out of an agent.run body before
// forwarding it unchanged: the ids, and the two values a containment and a
// token grammar can refuse before the executor sees them.
//
// `id` is the caller's call id, the one every frame of the stream carries and
// POST /cancel names — the same as claude.run's. The executor echoes it on
// its frames, and falls back to `run_id` when the caller sent none; this side
// registers the same one, so a cancel by either name finds the run.
type agentRunHead struct {
	ID        string     `json:"id"`
	RunID     string     `json:"run_id"`
	Workspace string     `json:"workspace"`
	MCP       *mcpParams `json:"mcp"`
}

// callID is the id the stream's frames carry.
func (h agentRunHead) callID() string {
	if h.ID != "" {
		return h.ID
	}
	return h.RunID
}

func (s *runnerServer) executorRedactor(mcpToken string) func([]byte) []byte {
	held := providerSecrets(s.cfg.providers)
	held[mcpRunTokenLabel] = mcpToken
	return heldSecretRedactor(s.cfg.policy, held)
}

// handleAgentRun forwards POST /agent.run to the executor's /exec/agent.run
// and streams its NDJSON back line by line, flushed as each arrives. The
// frames are the executor's, in claude.run's envelope (`started`, `event`
// with a `payload`, one `done`), so the cloud reads them with the parser it
// already has. The only change is the scrubbing of keys this side holds, and
// a `done` this side adds when the executor's stream ended without one — the
// caller is promised exactly one.
func (s *runnerServer) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	ex := s.state.executorSupervisor()
	if ex == nil {
		writeError(w, failure(codeNotReady, "this machine has no local executor (executor_bin was not sent)"))
		return
	}
	var head agentRunHead
	if err := json.Unmarshal(body, &head); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}
	if !agentRunID.MatchString(head.RunID) {
		writeError(w, failure(codeBadRequest, "run_id %q is not a run id this runner will register", head.RunID))
		return
	}
	if head.ID != "" && !agentRunID.MatchString(head.ID) {
		writeError(w, failure(codeBadRequest, "id %q is not a call id this runner will register", head.ID))
		return
	}
	if head.Workspace != "" {
		if _, err := resolveInWorkspace(s.cfg.workspaceDir, head.Workspace); err != nil {
			writeError(w, failure(codeBadRequest, "workspace: %v", err))
			return
		}
	}
	mcpCfg, mcpErr := checkMCP(head.MCP)
	if mcpErr != nil {
		writeError(w, mcpErr)
		return
	}

	callID := head.callID()
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new runs"))
		return
	}
	defer s.leaveRun()

	run, releaseSlot, queued := s.queueDurable(w, r, callID, "agent.run")
	if !queued {
		return
	}
	refuse := func(e *rpcError) {
		releaseSlot()
		run.abandon()
		writeError(w, e)
	}

	inst, rpcErr := ex.ready(run.ctx)
	if rpcErr != nil {
		refuse(rpcErr)
		return
	}
	redact := s.executorRedactor(mcpTokenOf(mcpCfg))

	// On the run's context, not the request's: the executor's run is what
	// outlives this stream, so the connection to it must too.
	req, err := http.NewRequestWithContext(run.ctx, http.MethodPost, inst.base+"/exec/agent.run", bytes.NewReader(body))
	if err != nil {
		refuse(failure(codeInternal, "building the executor request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	resp, err := ex.client.Do(req)
	if err != nil {
		if run.ctx.Err() != nil {
			refuse(failure(codeCancelled, "the call was cancelled"))
			return
		}
		refuse(failure(codeUpstream, "could not reach the local executor: %v", err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		relayDocument(w, resp, redact)
		_ = resp.Body.Close()
		releaseSlot()
		run.abandon()
		return
	}
	closeBody := func() { _ = resp.Body.Close() }
	tellExecutor := func() {
		if run.ctx.Err() != nil {
			ex.cancelRun(head.RunID, inst)
		}
	}

	s.serveDurable(w, r, run, []func(){releaseSlot, closeBody, tellExecutor}, func(runCtx context.Context, frames io.Writer) {
		if relayFrames(frames, resp.Body, redact, callID) {
			return
		}
		code, message := codeUpstream, "the local executor ended the run's stream without a done line"
		if runCtx.Err() != nil {
			code, message = codeCancelled, "the run was cancelled"
		}
		closing, _ := json.Marshal(doneEvent{V: protocolVersion, ID: callID, Event: "done", OK: false, Error: &rpcError{Code: code, Message: message}})
		_, _ = frames.Write(closing)
	})
}

// handleLLMComplete forwards POST /llm.complete to /exec/llm.complete: one
// document each way, status and body passed back as the executor answered.
func (s *runnerServer) handleLLMComplete(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	ex := s.state.executorSupervisor()
	if ex == nil {
		writeError(w, failure(codeNotReady, "this machine has no local executor (executor_bin was not sent)"))
		return
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(body, &probe); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new calls"))
		return
	}
	defer s.leaveRun()

	inst, rpcErr := ex.ready(r.Context())
	if rpcErr != nil {
		writeError(w, rpcErr)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, inst.base+"/exec/llm.complete", bytes.NewReader(body))
	if err != nil {
		writeError(w, failure(codeInternal, "building the executor request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ex.client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			writeError(w, failure(codeCancelled, "the call was cancelled"))
			return
		}
		writeError(w, failure(codeUpstream, "could not reach the local executor: %v", err))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	relayDocument(w, resp, s.executorRedactor(""))
}

// relayDocument passes one bounded answer back with the executor's own status
// and content type, scrubbed.
func relayDocument(w http.ResponseWriter, resp *http.Response, redact func([]byte) []byte) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, executorResponseLimit+1))
	if err != nil {
		writeError(w, failure(codeUpstream, "reading the local executor's answer: %v", err))
		return
	}
	if len(raw) > executorResponseLimit {
		writeError(w, failure(codeUpstream, "the local executor's answer exceeds %d bytes", executorResponseLimit))
		return
	}
	if redact != nil {
		raw = redact(raw)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(raw); err != nil {
		log.Debug().Err(err).Msg("caller went away before the executor's answer")
	}
}

// relayFrames copies the executor's NDJSON into a run one frame at a time,
// scrubbed, and says whether a done went past. A line over outputLineLimit is
// cut there: the executor caps what it puts in a frame far below it, and a
// frame is the unit a run buffers.
func relayFrames(frames io.Writer, body io.Reader, redact func([]byte) []byte, callID string) bool {
	reader := bufio.NewReaderSize(body, outputReadBuffer)
	for {
		line, dropped, err := readLimitedLine(reader, outputLineLimit)
		if dropped > 0 {
			log.Warn().Str("call", callID).Int64("dropped", dropped).Msg("a frame from the executor was longer than the limit and was cut")
		}
		if len(bytes.TrimSpace(line)) > 0 {
			if redact != nil {
				line = redact(line)
			}
			if _, werr := frames.Write(line); werr != nil {
				return false
			}
			if dropped == 0 && isDoneLine(line) {
				return true
			}
		}
		if err != nil {
			return false
		}
	}
}

// isDoneLine reads a frame of the runner's own streamed envelope, which the
// executor speaks: `{"v":1,"id":…,"event":"done",…}`.
func isDoneLine(line []byte) bool {
	var frame struct {
		Event string `json:"event"`
	}
	return json.Unmarshal(bytes.TrimSpace(line), &frame) == nil && frame.Event == "done"
}

// cancelRun tells the executor a forwarded run's caller is gone. The closed
// connection already says so; this says it by name, so the executor need not
// notice a half-closed socket to stop spending the member's tokens.
func (e *executorSupervisor) cancelRun(runID string, inst *executorProcess) {
	ctx, cancel := context.WithTimeout(context.Background(), executorCancelTimeout)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"run_id": runID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/exec/cancel", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		log.Debug().Err(err).Str("run", runID).Msg("could not tell the executor a run was cancelled")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()
}

// setEmbeddingsBaseURL tells a running executor that the embedder moved. It
// never waits for or starts one: the config line and the restart path already
// carry the live value, so a control message with no executor running has
// nothing to update. A failure is logged and dropped — the executor keeps its
// old address until its next start, and a restart is never warranted for this.
func (e *executorSupervisor) setEmbeddingsBaseURL(rawURL string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	inst := e.cur
	e.mu.Unlock()
	if inst == nil {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, executorSetTimeout)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"embeddings_base_url": rawURL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.base+"/exec/embeddings.set", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+inst.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("url", rawURL).Msg("could not tell the executor the embedder moved")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Str("url", rawURL).Msg("the executor did not accept the embedder's new address")
	}
}
