package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// The wire format: HTTP/1.1 over the tunnel.
//
// The control plane is the yamux CLIENT — it opens the streams — and it reaches
// this Mac with `httputil.ReverseProxy` over an `http.Transport`. What lands on
// a stream is therefore genuine HTTP framing, not a line of our own choosing,
// and this program is an `http.Server` whose listener happens to be a yamux
// session rather than a socket. That is also what the tunnel was built for: it
// carried HTTP to agent-server before, and keeping HTTP keeps status codes,
// headers and incremental streaming, all of which the callers need.
//
//	POST /claude.run          → 200 application/x-ndjson, streamed
//	POST /opencode.run        → 200 application/x-ndjson, streamed
//	POST /cursor.run          → 200 application/x-ndjson, streamed
//	POST /workspace.prepare   → 200 application/json
//	POST /workspace.ensure    → 200 application/json
//	POST /toolchain.detect    → 200 application/json
//	POST /embeddings.create   → the embedding engine's own status and body, verbatim
//	GET  /mobile.devices      → 200 application/json
//	POST /mobile.boot         → 200 application/json
//	POST /mobile.shutdown     → 200 application/json
//	ANY  /mobile.appium/…     → the local Appium hub's own status and body, verbatim
//	GET  /preflight.report    → 200 application/json
//	POST /models.list         → 200 application/json
//	POST /agent.run           → the local executor's NDJSON, streamed
//	POST /llm.complete        → the local executor's status and body
//	POST /cancel              → 200 application/json
//
// Nothing here binds a socket. The only listener is the session, and the rule
// that this process is unreachable from the network is unchanged — see
// `rules_test.go`, which still forbids every `net.Listen`.
const protocolVersion = 1

// requestReadLimit bounds one request body. Prompts are the large field and
// they are genuinely large — a task description with a diff in it — but they
// are not megabytes, and a limit is what stops a request from being an
// allocation primitive.
const requestReadLimit = 4 * 1024 * 1024

// requestHeaderTimeout bounds how long a request's headers may take to arrive
// once the first byte has. A stream opened and then left half-written would
// otherwise hold a goroutine for the life of the session.
const requestHeaderTimeout = 30 * time.Second

// idleTimeout reaps a kept-alive connection nobody is using. `ReverseProxy`
// pools connections, so an idle one is normal and expected; one that is idle
// for minutes is a stream this side is holding for no reason.
const idleTimeout = 5 * time.Minute

// maxConcurrentSessions bounds how many Claude Code sessions run at once.
//
// The bound moved with the protocol, and moving it was the point. It used to
// count STREAMS, because a stream was a call; with HTTP a stream is a pooled
// connection that may carry many requests, and counting those would bound the
// wrong thing. What is actually expensive is a session: it owns a checkout and
// a CPU. Sixteen of those is already a Mac under load.
//
// Waiting rather than refusing: the caller asked for work to be done and a 503
// would make it decide what to do about a queue this side can simply hold. The
// wait honours the request's context, so a caller that gives up stops waiting.
const maxConcurrentSessions = 16

// The error codes, a closed set on purpose: the control plane branches on
// them, and a message it has to pattern-match is a message that cannot be
// reworded. Each maps onto exactly one status, and the code string is in the
// body as well so a caller can keep switching on it rather than on a number.
const (
	codeBadRequest        = "bad_request"        // malformed, or naming something outside the workspace
	codeUnsupportedMethod = "unsupported_method" // no such path, or the wrong HTTP method on one
	codeNotReady          = "not_ready"          // the answer genuinely does not exist yet
	codeCancelled         = "cancelled"          // the caller asked, or the tunnel went away
	codeUpstream          = "upstream"           // something this Mac depends on failed (the embedding engine, a git remote)
	codeInternal          = "internal"           // a bug here
)

// statusCancelled is 499, nginx's "client closed request".
//
// Chosen over 408 deliberately. 408 is a REQUEST timeout — the server gave up
// waiting for the client to finish sending — and every cancellation here is the
// opposite: the request arrived, the work started, and the client (or the
// tunnel) went away. 499 is the code everyone already reads that way, and it
// cannot be confused with a Mac that was too slow to accept a request.
const statusCancelled = 499

// statusFor maps a code onto its status. One place, so the two can never
// disagree, and a code with no mapping is a bug rather than a silent 200.
func statusFor(code string) int {
	switch code {
	case codeBadRequest:
		return http.StatusBadRequest
	case codeUnsupportedMethod:
		return http.StatusNotFound
	case codeNotReady:
		return http.StatusConflict
	case codeCancelled:
		return statusCancelled
	case codeUpstream:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func failure(code, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// runnerServer is one tunnel session's worth of HTTP. It is built per session
// so the run registry and the session semaphore go away with the session they
// belong to — a cancel for a call from a connection that no longer exists is
// not a thing anyone should be able to send.
type runnerServer struct {
	cfg   config
	state *state

	// runs is how POST /cancel finds a running session. Keyed by call id.
	mu   sync.Mutex
	runs map[string]context.CancelFunc

	// sem bounds concurrent Claude Code sessions; see maxConcurrentSessions.
	sem chan struct{}

	// mcpRoot is where a run's MCP config directory is created — the user's
	// own temp directory in the shipped binary, and somewhere a test can watch
	// in the tests. See mcp.go for why it is not under the workspace.
	mcpRoot string

	// The in-flight accounting, under mu with the run registry.
	//
	// Deliberately NOT a sync.WaitGroup, which is what this was. A WaitGroup
	// makes "start a run" and "wait for the runs" two operations that cannot be
	// ordered against each other: `Add` racing a `Wait` that is already blocked
	// panics with "sync: WaitGroup misuse", and a panic here aborts the whole
	// daemon — which orphans precisely the `claude` process groups the ordered
	// teardown exists to reap. The window is small and the outcome is the worst
	// one available, so it is closed with a lock instead.
	//
	// It also gives teardown a decision a WaitGroup cannot: once `draining` is
	// set, a request that arrives afterwards is REFUSED rather than counted, so
	// a run cannot start on a session that is going away.
	active   int
	draining bool
	// idle is closed when the last in-flight run finishes during a drain. A
	// channel rather than a sync.Cond because the waiter also wants a timeout.
	idle chan struct{}
}

// enterRun claims a slot in the in-flight accounting, or refuses because the
// session is being torn down. Every caller that gets true must call leaveRun.
func (s *runnerServer) enterRun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return false
	}
	s.active++
	return true
}

func (s *runnerServer) leaveRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	if s.draining && s.active == 0 && s.idle != nil {
		close(s.idle)
		s.idle = nil
	}
}

// drain stops accepting new runs and waits for the ones in flight.
//
// Returns when the last one has returned — which, because `claude.run` reaps
// its process group before it returns, is the moment this Mac is genuinely
// running nothing. The caller cancels the session context first; this only
// waits.
func (s *runnerServer) drain() {
	s.mu.Lock()
	s.draining = true
	if s.active == 0 {
		s.mu.Unlock()
		return
	}
	if s.idle == nil {
		s.idle = make(chan struct{})
	}
	idle := s.idle
	s.mu.Unlock()
	<-idle
}

func newRunnerServer(cfg config, st *state) *runnerServer {
	// Seeds state's embeddingsBaseURL from the startup config exactly once —
	// see seedEmbeddingsBaseURLIfUnset's own comment for why a reconnect must
	// not re-seed a value a control message has already moved on.
	st.seedEmbeddingsBaseURLIfUnset(cfg.embeddingsBaseURL)
	return &runnerServer{
		cfg:     cfg,
		state:   st,
		runs:    map[string]context.CancelFunc{},
		sem:     make(chan struct{}, maxConcurrentSessions),
		mcpRoot: os.TempDir(),
	}
}

// register claims an id for a run. It refuses a duplicate rather than
// overwriting: two live calls under one id would make POST /cancel ambiguous,
// and the caller would have no way to find out which one it stopped.
func (s *runnerServer) register(id string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.runs[id]; taken {
		return false
	}
	s.runs[id] = cancel
	return true
}

func (s *runnerServer) unregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, id)
}

// cancelRun stops a running call and reports whether there was one. A cancel
// that arrives after the call finished is NOT an error — that race is ordinary
// — so this answers with a boolean rather than a failure.
func (s *runnerServer) cancelRun(id string) bool {
	s.mu.Lock()
	cancel, found := s.runs[id]
	s.mu.Unlock()
	if !found {
		return false
	}
	cancel()
	return true
}

// handler is the routing table. `http.ServeMux` would answer an unknown path
// with its own plain-text 404, and every failure this program produces has to
// be the JSON shape callers parse — so the mux is explicit.
func (s *runnerServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := func(method string, fn func(http.ResponseWriter, *http.Request)) {
			if r.Method != method {
				// Folded into unsupported_method rather than given a 405 of its
				// own: the closed set maps one code to one status, and a status
				// with no code would be the one answer a caller cannot branch
				// on.
				writeError(w, failure(codeUnsupportedMethod, "%s is not allowed on %s (use %s)", r.Method, r.URL.Path, method))
				return
			}
			fn(w, r)
		}

		// The one prefix route, and it is one because of what is underneath it:
		// an Appium client builds its own paths — `/session`, then
		// `/session/{id}/element/{id}/attribute/{name}` — and the caller's whole
		// Appium client works unchanged only if every one of them lands here.
		// Any method, because Appium uses four of them. Checked before the
		// switch so a path under the prefix can never fall through to the 404.
		if r.URL.Path == appiumPrefix || strings.HasPrefix(r.URL.Path, appiumPrefix+"/") {
			s.proxyAppium(w, r)
			return
		}

		switch r.URL.Path {
		case "/claude.run":
			route(http.MethodPost, s.handleClaudeRun)
		case "/opencode.run":
			route(http.MethodPost, s.handleOpencodeRun)
		case "/cursor.run":
			route(http.MethodPost, s.handleCursorRun)
		case "/workspace.prepare":
			route(http.MethodPost, s.handlePrepare)
		case "/workspace.ensure":
			route(http.MethodPost, s.handleWorkspaceEnsure)
		case "/toolchain.detect":
			route(http.MethodPost, s.handleToolchain)
		case "/embeddings.create":
			route(http.MethodPost, s.handleEmbeddings)
		case "/mobile.devices":
			route(http.MethodGet, s.handleMobileDevices)
		case "/mobile.boot":
			route(http.MethodPost, s.handleMobileBoot)
		case "/mobile.shutdown":
			route(http.MethodPost, s.handleMobileShutdown)
		case "/mobile.release":
			// Streaming, like claude.run and unlike the other mobile methods: a
			// store build is an hour of output somebody is watching, not one
			// document at the end.
			route(http.MethodPost, s.handleMobileRelease)
		case "/preflight.report":
			route(http.MethodGet, s.handlePreflight)
		case "/models.list":
			route(http.MethodPost, s.handleModels)
		case "/agent.run":
			// Streaming, forwarded to the local executor (executor.go).
			route(http.MethodPost, s.handleAgentRun)
		case "/llm.complete":
			route(http.MethodPost, s.handleLLMComplete)
		case "/cancel":
			route(http.MethodPost, s.handleCancel)
		default:
			writeError(w, failure(codeUnsupportedMethod, "this runner has no %q", r.URL.Path))
		}
	})
}

// call is what a method implementation is given: the request's context, the
// configuration, the parsed body, and — for `claude.run` — the response it is
// streaming into.
type call struct {
	ctx    context.Context
	id     string
	cfg    config
	state  *state
	params json.RawMessage

	mu    sync.Mutex
	w     io.Writer
	flush func()
	done  bool

	// redact scrubs one encoded frame before it is written. nil is the ordinary
	// case; `mobile.release` sets it because that call — alone among these — is
	// handed signing material, and a tool on the far side that echoes its own
	// inputs would otherwise put a distribution key in a log somebody pastes
	// into a ticket. It is applied to the MARSHALLED bytes because that is the
	// one place output events, the terminal frame and the error inside it all
	// converge; a value that arrived by a path nobody thought of is caught
	// there anyway. See mobile_release.go.
	redact func([]byte) []byte
}

// embeddingsBaseURL prefers state's live value — updated by an
// "embeddings-base-url" control message after the embedder restarts on a new
// port — and falls back to cfg's startup value when state is nil or has not
// been seeded yet (a call built directly, outside newRunnerServer, as tests
// do; newRunnerServer itself seeds state from cfg on every real request path).
func (c *call) embeddingsBaseURL() string {
	if c.state != nil {
		if u := c.state.getEmbeddingsBaseURL(); u != "" {
			return u
		}
	}
	return c.cfg.embeddingsBaseURL
}

// emit writes one NDJSON event and flushes it.
//
// Serialised, because `claude.run` forwards stdout and stderr from two
// goroutines and interleaved halves of two JSON documents would be
// unparseable — the one thing a caller must be able to rely on.
//
// Flushed on every line, because the point of this response is that the cloud
// sees the run as it happens. Without the flush, Go's response buffer and the
// proxy in front of it would hold the transcript until the run ended, which is
// the buffering this whole shape exists to avoid.
//
// A write failure is returned rather than swallowed: it means the far end is
// gone, and that is exactly when the process being streamed must be killed
// rather than left running for nobody.
func (c *call) emit(event any) error {
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encoding %T: %w", event, err)
	}
	if c.redact != nil {
		line = c.redact(line)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing to the caller: %w", err)
	}
	if c.flush != nil {
		c.flush()
	}
	return nil
}

type startedEvent struct {
	V     int    `json:"v"`
	ID    string `json:"id"`
	Event string `json:"event"`
}

type outputEvent struct {
	V      int    `json:"v"`
	ID     string `json:"id"`
	Event  string `json:"event"`
	Stream string `json:"stream"`
	Data   string `json:"data"`
}

// output forwards one line the child wrote. `stream` is "stdout" or "stderr",
// kept apart because a Claude Code session's stdout is structured JSON the
// control plane parses and its stderr is prose for a human.
func (c *call) output(stream, data string) error {
	return c.emit(outputEvent{V: protocolVersion, ID: c.id, Event: "output", Stream: stream, Data: data})
}

type doneEvent struct {
	V      int             `json:"v"`
	ID     string          `json:"id"`
	Event  string          `json:"event"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// finish writes the terminal line, once. Later calls are dropped rather than
// written, so a method that returns an error after having already answered
// cannot produce a response with two endings.
//
// The status is 200 by the time this runs — the stream started long ago — so a
// failure here lives in the body, with the same code string it would have
// carried as a status. That is why callers switch on the code and not on the
// number: for the streaming method, the number was decided before the work.
func (c *call) finish(result any, callErr *rpcError) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	c.done = true
	c.mu.Unlock()

	ev := doneEvent{V: protocolVersion, ID: c.id, Event: "done", OK: callErr == nil, Error: callErr}
	if callErr == nil && result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			ev.OK = false
			ev.Error = &rpcError{Code: codeInternal, Message: fmt.Sprintf("encoding the result: %v", err)}
		} else {
			ev.Result = encoded
		}
	}
	line, err := json.Marshal(ev)
	if err != nil {
		log.Error().Err(err).Str("call", c.id).Msg("could not encode the terminal frame")
		return
	}
	// The error inside this frame is scrubbed too, and that is the point of
	// doing it here: a refusal quoting what it could not import is exactly the
	// message a secret escapes in.
	if c.redact != nil {
		line = c.redact(line)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, writeErr := c.w.Write(append(line, '\n')); writeErr != nil {
		log.Debug().Err(writeErr).Str("call", c.id).Msg("caller went away before the result")
		return
	}
	if c.flush != nil {
		c.flush()
	}
}

// --- the streaming method ---------------------------------------------------

// claudeRunRequest is claude.run's body: the params, plus an optional id the
// caller may choose so it can cancel the run before it has read a byte of the
// response.
type claudeRunRequest struct {
	claudeRunParams
	ID string `json:"id,omitempty"`
}

func (s *runnerServer) handleClaudeRun(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}

	var req claudeRunRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}

	// Everything that can be refused is refused BEFORE the 200, so a bad
	// request is an HTTP failure a caller can see at once rather than a
	// two-hundred-with-an-error it has to read a stream to discover.
	prepared, prepErr := s.prepareRun(req.claudeRunParams)
	if prepErr != nil {
		writeError(w, prepErr)
		return
	}

	id := req.ID
	if id == "" {
		id = newCallID()
	}

	runCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if !s.register(id, cancel) {
		writeError(w, failure(codeBadRequest, "a call with id %q is already running on this machine", id))
		return
	}
	defer s.unregister(id)

	// Claim a place in the in-flight accounting, or be refused because this
	// session is going away. A run that started here after `drain` had returned
	// would be a `claude` nobody is waiting for on a tunnel that has detached.
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down and is not taking new runs"))
		return
	}
	defer s.leaveRun()

	// Cancellation beats the queue, deterministically.
	//
	// A `select` with both cases ready picks at RANDOM, so a request that
	// arrived as the session was being torn down had roughly even odds of
	// taking the semaphore and spawning `claude` anyway. Checking first makes
	// the common case exact, and re-checking after the semaphore closes the
	// remaining window: the two can still become ready together, and the
	// re-check is what decides it in cancellation's favour every time.
	if runCtx.Err() != nil {
		writeError(w, failure(codeCancelled, "the call was cancelled before it started"))
		return
	}
	// Registered before the wait, so a caller that changes its mind while
	// queued can still cancel by id and is not left waiting on a Mac it has
	// stopped caring about.
	select {
	case s.sem <- struct{}{}:
		if runCtx.Err() != nil {
			<-s.sem
			writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
			return
		}
		defer func() { <-s.sem }()
	case <-runCtx.Done():
		writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
		return
	}

	// The MCP configuration becomes a file on disk for exactly as long as this
	// run, and the defer is the whole guarantee: it fires however the call ends
	// — a return from here, the session finishing or failing, a POST /cancel,
	// or the tunnel dropping and cancelling the request context under it. It is
	// registered BEFORE the error is looked at, because a half-written file is
	// still a file with a token in it, and BEFORE the 200, so a disk that
	// refused is a status rather than a frame nobody reads until later.
	//
	// It is also registered after `defer s.leaveRun()`, so it runs first:
	// by the time a session teardown reports "tunnel detached", the tokens of
	// the runs it was serving are already off the disk.
	mcpArgs, removeMCP, mcpErr := writeMCPRun(s.mcpRoot, id, prepared.mcp)
	defer removeMCP()
	if mcpErr != nil {
		writeError(w, mcpErr)
		return
	}
	args := append(prepared.args, mcpArgs...)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, failure(codeInternal, "this response cannot be streamed"))
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	// Nothing between here and the caller should try to buffer a response whose
	// whole value is arriving early. Harmless where it is not understood.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	c := &call{
		ctx:    runCtx,
		id:     id,
		cfg:    s.cfg,
		state:  s.state,
		params: body,
		w:      w,
		flush:  flusher.Flush,
		// claude inherits this process's environment unmodified — see
		// spawnClaude — so the credential values that authenticate it are
		// exactly the ones a misbehaving tool on the far side could echo back
		// into the transcript. This is what keeps them out of it.
		redact: credentialRedactor(s.cfg.policy, mcpTokenOf(prepared.mcp)),
	}

	// The first line, always, and it carries the id — including the one this
	// side generated, which is the only way a caller that did not choose one
	// can ever cancel. It also tells the caller the wait for a slot is over,
	// which "no output yet" cannot.
	if err := c.emit(startedEvent{V: protocolVersion, ID: id, Event: "started"}); err != nil {
		log.Debug().Str("call", id).Err(err).Msg("caller went away before the session started")
		return
	}

	// The run's own ceiling, if it asked for one. Kept OFF `c.ctx` on purpose:
	// spawnClaude tells a timeout from a cancellation by comparing the two, and
	// a caller that set a timeout still deserves to be told which of the two
	// stopped its task.
	spawnCtx := runCtx
	if prepared.timeout > 0 {
		var stopTimeout context.CancelFunc
		spawnCtx, stopTimeout = context.WithTimeout(runCtx, prepared.timeout)
		defer stopTimeout()
	}

	result, callErr := spawnClaude(spawnCtx, c, prepared.dir, args, prepared.prompt, prepared.env)

	// A cancelled call reports as cancelled whatever the method returned. A
	// process killed mid-flight usually surfaces as some incidental error — a
	// closed pipe, a signal — and reporting that instead would make every
	// cancellation look like a different bug.
	if callErr == nil && runCtx.Err() != nil {
		callErr = failure(codeCancelled, "the call was cancelled")
	}
	c.finish(result, callErr)
}

// --- the JSON methods -------------------------------------------------------

// handleJSON is every method whose answer is one document: read the body, run
// the method, answer with the result or with the failure.
//
// The cancellation check after the method matters and is not decoration. A
// method that was killed mid-way usually returns some incidental error — a
// process that was signalled, a read on a closed pipe — and reporting that
// instead of `cancelled` would make every dropped tunnel look like a different
// bug. It is the same rule `claude.run` applies to its `done` frame.
func (s *runnerServer) handleJSON(w http.ResponseWriter, r *http.Request, method func(*call) (any, *rpcError)) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	c := &call{ctx: r.Context(), id: newCallID(), cfg: s.cfg, state: s.state, params: body}
	result, callErr := method(c)
	if callErr == nil && r.Context().Err() != nil {
		callErr = failure(codeCancelled, "the call was cancelled")
	}
	if callErr != nil {
		writeError(w, callErr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *runnerServer) handlePrepare(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, prepareWorkspace)
}

func (s *runnerServer) handleToolchain(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, detectToolchain)
}

func (s *runnerServer) handleWorkspaceEnsure(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, ensureWorkspace)
}

func (s *runnerServer) handleModels(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, listModels)
}

func (s *runnerServer) handleMobileBoot(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, mobileBoot)
}

func (s *runnerServer) handleMobileShutdown(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, mobileShutdown)
}

// handleMobileDevices is a GET, like `preflight.report` and for the same
// reason: it is a read with no parameters, and a body on it would be a body
// with nothing in it.
func (s *runnerServer) handleMobileDevices(w http.ResponseWriter, r *http.Request) {
	s.handleJSON(w, r, mobileDevices)
}

// handleEmbeddings passes the embedding engine's answer back untouched — its
// status code included.
//
// A 404 from the embedding engine means "that model is not loaded", and
// re-labelling it as a 502 from this Mac would throw away the one fact the
// caller needs. This side only produces a status of its own when it could not
// reach the embedding engine at all, or when the request was refused before it
// was sent.
func (s *runnerServer) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	c := &call{ctx: r.Context(), id: newCallID(), cfg: s.cfg, state: s.state, params: body}
	status, answer, callErr := createEmbeddings(c)
	if callErr != nil {
		writeError(w, callErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(answer); err != nil {
		log.Debug().Err(err).Msg("caller went away before the embeddings")
	}
}

func (s *runnerServer) handlePreflight(w http.ResponseWriter, _ *http.Request) {
	report, callErr := s.preflightReport()
	if callErr != nil {
		writeError(w, callErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(report); err != nil {
		log.Debug().Err(err).Msg("caller went away before the preflight report")
	}
}

type cancelRequest struct {
	ID string `json:"id"`
}

type cancelResponse struct {
	V         int    `json:"v"`
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
}

// handleCancel is the explicit trigger. The other one is the client closing the
// response body, which `http.Server` turns into a cancelled request context —
// see the note in main.go on why that no longer needs protecting against.
//
// A cancel for a call that has already finished is answered `cancelled:false`
// and NOT as an error: that race is ordinary — the run ends while the cancel is
// in flight — and turning it into a 4xx would make callers treat a successful
// stop as a fault.
func (s *runnerServer) handleCancel(w http.ResponseWriter, r *http.Request) {
	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}
	var req cancelRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, failure(codeBadRequest, "the request body is not the expected object: %v", err))
		return
	}
	if req.ID == "" {
		writeError(w, failure(codeBadRequest, "id is required: a cancel names the call it stops"))
		return
	}
	cancelled := s.cancelRun(req.ID)
	log.Info().Str("call", req.ID).Bool("found", cancelled).Msg("cancel requested")
	writeJSON(w, http.StatusOK, cancelResponse{V: protocolVersion, ID: req.ID, Cancelled: cancelled})
}

// --- plumbing ---------------------------------------------------------------

func readBody(r *http.Request) ([]byte, *rpcError) {
	body, err := io.ReadAll(io.LimitReader(r.Body, requestReadLimit+1))
	if err != nil {
		return nil, failure(codeBadRequest, "could not read the request body: %v", err)
	}
	if len(body) > requestReadLimit {
		return nil, failure(codeBadRequest, "the request body exceeds %d bytes", requestReadLimit)
	}
	return body, nil
}

type errorBody struct {
	V     int      `json:"v"`
	Error rpcError `json:"error"`
}

// writeError is the only way a failure leaves this program: a status from the
// closed mapping, and the code repeated in the body so a caller can keep
// switching on the string it already knew.
func writeError(w http.ResponseWriter, err *rpcError) {
	writeJSON(w, statusFor(err.Code), errorBody{V: protocolVersion, Error: *err})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("could not encode a response")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"v":1,"error":{"code":"internal","message":"the response could not be encoded"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(encoded); err != nil {
		log.Debug().Err(err).Msg("caller went away before the response")
	}
}

// newCallID is used when the caller did not choose one. Random rather than a
// counter, because ids appear in both sides' logs and a per-session counter
// would collide across reconnects in exactly the log where that matters.
func newCallID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(buf[:])
}
