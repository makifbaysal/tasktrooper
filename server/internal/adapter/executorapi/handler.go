// Package executorapi is the executor's loopback HTTP surface: the process
// that spawned it (the runner, or the desktop in development) drives agent
// runs and one-shot model calls through it.
package executorapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
)

const (
	PathHealth      = "/exec/health"
	PathAgentRun    = "/exec/agent.run"
	PathLLMComplete = "/exec/llm.complete"
	PathCancel      = "/exec/cancel"
	PathIndexEnsure = "/exec/index.ensure"
	PathIndexSearch = "/exec/index.search"
	PathEmbeddings  = "/exec/embeddings.set"
	PathVerify      = "/exec/verify"
	PathGitStatus   = "/exec/git.status"
	PathGitDiff     = "/exec/git.diff"
	PathGitLog      = "/exec/git.log"
	PathCommitPush  = "/exec/commit_push"
	PathMCPOpen     = "/exec/mcp.open"
	PathMCPClose    = "/exec/mcp.close"
	PathMCPCalls    = "/exec/mcp.calls"
)

const (
	codeUnauthorized      = "unauthorized"
	codeUnsupportedMethod = "unsupported_method"
)

// statusCancelled is nginx's "client closed request", as the runner uses it:
// the work started and the caller went away, which no 4xx timeout means.
const statusCancelled = 499

const (
	runBodyLimit   = 16 << 20
	unaryBodyLimit = 4 << 20
)

type Options struct {
	Token   string
	Version string
	// Secrets are scrubbed from every response body, frame by frame: a
	// provider error that quotes the key it was sent, or a tool that echoes
	// one, must not carry it off this machine.
	Secrets []string
}

type route struct {
	method string
	serve  func(http.ResponseWriter, *http.Request)
}

type Handler struct {
	svc     *executor.Service
	token   []byte
	version string
	redact  func([]byte) []byte
	routes  map[string]route

	mu      sync.Mutex
	streams map[string]string
}

func NewHandler(svc *executor.Service, opts Options) *Handler {
	h := &Handler{
		svc:     svc,
		token:   []byte(opts.Token),
		version: opts.Version,
		redact:  newRedactor(opts.Secrets...),
		streams: make(map[string]string),
	}
	h.routes = map[string]route{
		PathHealth:      {http.MethodGet, h.health},
		PathAgentRun:    {http.MethodPost, h.agentRun},
		PathLLMComplete: {http.MethodPost, h.complete},
		PathCancel:      {http.MethodPost, h.cancel},
		PathIndexEnsure: {http.MethodPost, h.indexEnsure},
		PathIndexSearch: {http.MethodPost, h.indexSearch},
		PathEmbeddings:  {http.MethodPost, h.setEmbeddings},
		PathVerify:      {http.MethodPost, h.verify},
		PathGitStatus:   {http.MethodPost, h.gitStatus},
		PathGitDiff:     {http.MethodPost, h.gitDiff},
		PathGitLog:      {http.MethodPost, h.gitLog},
		PathCommitPush:  {http.MethodPost, h.commitPush},
		PathMCPOpen:     {http.MethodPost, h.mcpOpen},
		PathMCPClose:    {http.MethodPost, h.mcpClose},
		PathMCPCalls:    {http.MethodPost, h.mcpCalls},
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tasktrooper-executor"`)
		h.writeError(w, &executor.Failure{Code: codeUnauthorized, Message: "a valid bearer token is required"})
		return
	}
	route, ok := h.routes[r.URL.Path]
	if !ok || r.Method != route.method {
		h.writeError(w, &executor.Failure{Code: codeUnsupportedMethod, Message: fmt.Sprintf("no %s %s here", r.Method, r.URL.Path)})
		return
	}
	route.serve(w, r)
}

func (h *Handler) authorized(r *http.Request) bool {
	if len(h.token) == 0 {
		return false
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(header[len(prefix):])), h.token) == 1
}

type healthResponse struct {
	OK         bool     `json:"ok"`
	Version    string   `json:"version"`
	Protocol   int      `json:"protocol"`
	ActiveRuns int      `json:"active_runs"`
	LocalTools []string `json:"local_tools,omitempty"`
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, healthResponse{
		OK: true, Version: h.version, Protocol: executor.ProtocolVersion, ActiveRuns: h.svc.ActiveRuns(),
		LocalTools: h.svc.LocalToolNames(),
	})
}

// mcpOpen answers with the surface's own bearer, which is the point of the
// call; the coordination token it was handed never leaves in any answer.
func (h *Handler) mcpOpen(w http.ResponseWriter, r *http.Request) {
	var req executor.MCPOpen
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	cloudToken := ""
	if req.CloudMCP != nil {
		cloudToken = req.CloudMCP.Token
	}
	surface, failure := h.svc.OpenMCP(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure, cloudToken)
		return
	}
	h.write(w, http.StatusOK, surface, cloudToken)
}

type mcpCloseRequest struct {
	RunID string `json:"run_id"`
}

func (h *Handler) mcpClose(w http.ResponseWriter, r *http.Request) {
	var req mcpCloseRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	if strings.TrimSpace(req.RunID) == "" {
		h.writeError(w, &executor.Failure{Code: executor.CodeBadRequest, Message: "run_id is required: a close names the surface it stops"})
		return
	}
	h.writeJSON(w, http.StatusOK, h.svc.CloseMCP(req.RunID))
}

type mcpCallsRequest struct {
	RunID string `json:"run_id"`
	After int    `json:"after"`
}

func (h *Handler) mcpCalls(w http.ResponseWriter, r *http.Request) {
	var req mcpCallsRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	if strings.TrimSpace(req.RunID) == "" {
		h.writeError(w, &executor.Failure{Code: executor.CodeBadRequest, Message: "run_id is required: a read names the surface it asks about"})
		return
	}
	h.writeJSON(w, http.StatusOK, h.svc.MCPCallsSince(req.RunID, req.After))
}

// agentRunRequest carries the runner's own call id beside the run: when the
// runner forwards a call unchanged, frames that echo that id are frames its
// caller already knows how to match to the call.
type agentRunRequest struct {
	ID string `json:"id,omitempty"`
	executor.AgentRun
}

func (h *Handler) agentRun(w http.ResponseWriter, r *http.Request) {
	var req agentRunRequest
	if failure := decode(r, runBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	prepared, failure := h.svc.Prepare(r.Context(), req.AgentRun)
	if failure != nil {
		h.writeError(w, failure, mcpToken(req.AgentRun))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		prepared.Release()
		h.writeError(w, &executor.Failure{Code: executor.CodeInternal, Message: "this response cannot be streamed"})
		return
	}
	streamID := strings.TrimSpace(req.ID)
	if streamID == "" {
		streamID = prepared.RunID()
	}
	h.trackStream(streamID, prepared.RunID())
	defer h.untrackStream(streamID)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	stream := &frameWriter{
		w: w, flush: flusher.Flush, id: streamID,
		redact: h.redactorWith(mcpToken(req.AgentRun)),
	}
	if err := stream.started(); err != nil {
		prepared.Release()
		log.Debug().Err(err).Str("run_id", prepared.RunID()).Msg("caller went away before the run started")
		return
	}
	result, failure := prepared.Execute(stream)
	if failure != nil {
		stream.done(nil, failure)
		return
	}
	stream.done(result, nil)
}

type indexEnsureRequest struct {
	ID string `json:"id,omitempty"`
	executor.IndexEnsureRequest
}

// indexEnsure streams one index pass in the envelope agent.run uses: started,
// index_progress events, and one done frame carrying the index's state.
func (h *Handler) indexEnsure(w http.ResponseWriter, r *http.Request) {
	var req indexEnsureRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	prepared, failure := h.svc.PrepareIndexEnsure(r.Context(), req.ID, req.IndexEnsureRequest)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		prepared.Release()
		h.writeError(w, &executor.Failure{Code: executor.CodeInternal, Message: "this response cannot be streamed"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	stream := &frameWriter{w: w, flush: flusher.Flush, id: prepared.ID(), redact: h.redact}
	if err := stream.started(); err != nil {
		prepared.Release()
		log.Debug().Err(err).Str("id", prepared.ID()).Msg("caller went away before the index pass started")
		return
	}
	result, failure := prepared.Execute(stream)
	if failure != nil {
		stream.done(nil, failure)
		return
	}
	stream.done(result, nil)
}

func (h *Handler) indexSearch(w http.ResponseWriter, r *http.Request) {
	var req executor.IndexSearchRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	found, failure := h.svc.SearchIndex(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, found)
}

func (h *Handler) setEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req executor.EmbeddingsRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	state, failure := h.svc.SetEmbeddings(req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, state)
}

func mcpToken(run executor.AgentRun) string {
	if run.MCP == nil {
		return ""
	}
	return run.MCP.Token
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	var req executor.CompletionRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	completion, failure := h.svc.Complete(r.Context(), req)
	if failure != nil {
		h.writeError(w, failure)
		return
	}
	h.writeJSON(w, http.StatusOK, completion)
}

type cancelRequest struct {
	RunID string `json:"run_id"`
	ID    string `json:"id"`
}

type cancelResponse struct {
	V         int    `json:"v"`
	RunID     string `json:"run_id"`
	Cancelled bool   `json:"cancelled"`
}

// cancel takes the run id, or the id the run's stream answers under, so a
// runner can forward its own cancel body unchanged. A run that already ended
// answers cancelled:false rather than an error: the two race as a matter of
// course.
func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	if failure := decode(r, unaryBodyLimit, &req); failure != nil {
		h.writeError(w, failure)
		return
	}
	runID, streamID := strings.TrimSpace(req.RunID), strings.TrimSpace(req.ID)
	if runID == "" && streamID == "" {
		h.writeError(w, &executor.Failure{Code: executor.CodeBadRequest, Message: "run_id is required: a cancel names the run it stops"})
		return
	}
	if runID == "" {
		runID = h.runForStream(streamID)
	}
	cancelled := runID != "" && h.svc.Cancel(runID)
	if runID == "" {
		cancelled = h.svc.CancelIndexEnsure(streamID) || h.svc.CancelVerify(streamID)
	}
	log.Info().Str("run_id", runID).Str("id", streamID).Bool("found", cancelled).Msg("executor cancel requested")
	if runID == "" {
		runID = streamID
	}
	h.writeJSON(w, http.StatusOK, cancelResponse{V: executor.ProtocolVersion, RunID: runID, Cancelled: cancelled})
}

func (h *Handler) trackStream(streamID, runID string) {
	h.mu.Lock()
	h.streams[streamID] = runID
	h.mu.Unlock()
}

func (h *Handler) untrackStream(streamID string) {
	h.mu.Lock()
	delete(h.streams, streamID)
	h.mu.Unlock()
}

func (h *Handler) runForStream(streamID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.streams[streamID]
}

func decode(r *http.Request, limit int64, into any) *executor.Failure {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return &executor.Failure{Code: executor.CodeBadRequest, Message: fmt.Sprintf("could not read the request body: %v", err)}
	}
	if int64(len(body)) > limit {
		return &executor.Failure{Code: executor.CodeBadRequest, Message: fmt.Sprintf("the request body exceeds %d bytes", limit)}
	}
	if err := json.Unmarshal(body, into); err != nil {
		return &executor.Failure{Code: executor.CodeBadRequest, Message: fmt.Sprintf("the request body is not the expected object: %v", err)}
	}
	return nil
}

func statusFor(code string) int {
	switch code {
	case executor.CodeBadRequest:
		return http.StatusBadRequest
	case codeUnauthorized:
		return http.StatusUnauthorized
	case codeUnsupportedMethod:
		return http.StatusNotFound
	case executor.CodeConflict:
		return http.StatusConflict
	case executor.CodeRateLimited:
		return http.StatusTooManyRequests
	case executor.CodeCancelled:
		return statusCancelled
	case executor.CodeNotReady:
		return http.StatusServiceUnavailable
	case executor.CodeUpstream:
		return http.StatusBadGateway
	case executor.CodeTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

type errorBody struct {
	V     int               `json:"v"`
	Error *executor.Failure `json:"error"`
}

func (h *Handler) writeError(w http.ResponseWriter, failure *executor.Failure, extraSecrets ...string) {
	h.write(w, statusFor(failure.Code), errorBody{V: executor.ProtocolVersion, Error: failure}, extraSecrets...)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	h.write(w, status, payload)
}

func (h *Handler) write(w http.ResponseWriter, status int, payload any, extraSecrets ...string) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("executor could not encode a response")
		status = http.StatusInternalServerError
		encoded = []byte(`{"v":1,"error":{"code":"internal","message":"the response could not be encoded"}}`)
	}
	encoded = h.redactorWith(extraSecrets...)(encoded)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(encoded); err != nil {
		log.Debug().Err(err).Msg("caller went away before the response")
	}
}

func (h *Handler) redactorWith(extraSecrets ...string) func([]byte) []byte {
	extra := newRedactor(extraSecrets...)
	return func(body []byte) []byte { return extra(h.redact(body)) }
}

type startedFrame struct {
	V     int    `json:"v"`
	ID    string `json:"id"`
	Event string `json:"event"`
}

type eventFrame struct {
	V       int            `json:"v"`
	ID      string         `json:"id"`
	Event   string         `json:"event"`
	Payload executor.Event `json:"payload"`
}

type doneFrame struct {
	V      int               `json:"v"`
	ID     string            `json:"id"`
	Event  string            `json:"event"`
	OK     bool              `json:"ok"`
	Result any               `json:"result,omitempty"`
	Error  *executor.Failure `json:"error,omitempty"`
}

var errStreamClosed = errors.New("the run's stream already ended")

// frameWriter is the run's NDJSON stream: one JSON document per line, flushed
// as written so the caller sees the run as it happens, and exactly one done
// line at the end.
type frameWriter struct {
	w      io.Writer
	flush  func()
	id     string
	redact func([]byte) []byte

	mu       sync.Mutex
	finished bool
}

var _ executor.Sink = (*frameWriter)(nil)

func (f *frameWriter) started() error {
	return f.writeLine(startedFrame{V: executor.ProtocolVersion, ID: f.id, Event: "started"}, false)
}

func (f *frameWriter) Send(event executor.Event) error {
	return f.writeLine(eventFrame{V: executor.ProtocolVersion, ID: f.id, Event: "event", Payload: event}, false)
}

// done takes the result as any so an index pass and a run share one stream
// shape; pass a nil result (untyped) with a failure.
func (f *frameWriter) done(result any, failure *executor.Failure) {
	frame := doneFrame{V: executor.ProtocolVersion, ID: f.id, Event: "done", OK: failure == nil, Result: result, Error: failure}
	if failure != nil {
		frame.Result = nil
	}
	if err := f.writeLine(frame, true); err != nil {
		log.Debug().Err(err).Str("id", f.id).Msg("caller went away before the run's result")
	}
}

func (f *frameWriter) writeLine(frame any, final bool) error {
	line, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("encoding %T: %w", frame, err)
	}
	line = f.redact(line)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finished {
		return errStreamClosed
	}
	if final {
		f.finished = true
	}
	if _, err := f.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing to the caller: %w", err)
	}
	if f.flush != nil {
		f.flush()
	}
	return nil
}
