package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Durable runs. claude.run, opencode.run, cursor.run and agent.run outlive the
// HTTP stream that started them and the tunnel session that carried it: the
// cloud's load balancer cuts a WebSocket at an hour, and a task is often
// longer than that. Once a run has its slot it belongs to the registry, not
// to the request; it stops on POST /cancel, on its own timeout, or when this
// runner shuts down, and on nothing else. Every frame it writes gets a seq
// and lands in a bounded buffer on disk, so a caller that lost its stream
// reads the rest with POST /run.attach.

const (
	defaultRunBufferBytes = 16 << 20
	minRunBufferBytes     = 64 << 10
	maxRunBufferBytes     = 1 << 30

	runRetention = 30 * time.Minute
	// maxFinishedRuns bounds the disk a stream of short runs can hold for the
	// retention window; the oldest finished run goes first.
	maxFinishedRuns = 64
	runReadBatch    = 256 << 10
	runsDirName     = "runs"
)

var runDirName = regexp.MustCompile(`^[0-9a-f]{32}$`)

// runnerCapabilities lets the cloud feature-detect this protocol.
var runnerCapabilities = []string{"run.attach", "run.status"}

type runPhase int

const (
	runPending runPhase = iota
	runRunning
	runDone
)

type runRegistry struct {
	dir       string
	limit     int64
	retention time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{}

	mu       sync.Mutex
	runs     map[string]*durableRun
	finished []*durableRun
	closed   bool
	active   int
	idle     chan struct{}
}

// newRunRegistry keeps buffers under dir, or in memory when dir is "" or
// cannot be made. Leftovers from an earlier process are removed: a restarted
// runner has no index to serve them with.
func newRunRegistry(dir string, limit int64, retention time.Duration) *runRegistry {
	if limit <= 0 {
		limit = defaultRunBufferBytes
	}
	if dir != "" {
		sweepRunBuffers(dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Warn().Err(err).Str("dir", dir).Msg("run buffers stay in memory: their directory could not be made")
			dir = ""
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &runRegistry{
		dir:       dir,
		limit:     limit,
		retention: retention,
		ctx:       ctx,
		cancel:    cancel,
		sem:       make(chan struct{}, maxConcurrentSessions),
		runs:      map[string]*durableRun{},
	}
}

func sweepRunBuffers(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && runDirName.MatchString(e.Name()) {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				log.Warn().Err(err).Str("dir", e.Name()).Msg("could not remove a run buffer an earlier runner left")
			}
		}
	}
}

// runsDir is where a configured runner keeps its buffers.
func runsDir(cfg config) string {
	base := cfg.runnerDataDir
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil || cache == "" {
			return ""
		}
		base = filepath.Join(cache, "TaskTrooper", "runner")
	}
	return filepath.Join(base, runsDirName)
}

type durableRun struct {
	reg    *runRegistry
	id     string
	method string
	ctx    context.Context
	cancel context.CancelFunc
	detach func() bool

	mu          sync.Mutex
	phase       runPhase
	cancelAsked bool
	ring        *frameRing
	lastSeq     int64
	sawDone     bool
	changed     chan struct{}
	gone        bool
	gc          *time.Timer
}

// reserve claims id for a run that has not started. Until start, the run's
// context also ends with reqCtx: a call that is still queued when its caller
// goes away never starts. A finished run under the same id is replaced; a
// live one is refused, because two runs under one id would make cancel and
// attach ambiguous.
func (reg *runRegistry) reserve(id, method string, reqCtx context.Context) (*durableRun, *rpcError) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.closed {
		return nil, failure(codeCancelled, "this runner is shutting down and is not taking new runs")
	}
	if old := reg.runs[id]; old != nil {
		if old.state() != runDone {
			return nil, failure(codeBadRequest, "a call with id %q is already running on this machine; attach to it with run.attach", id)
		}
		reg.forgetLocked(old)
	}
	ctx, cancel := context.WithCancel(reg.ctx)
	run := &durableRun{reg: reg, id: id, method: method, ctx: ctx, cancel: cancel, changed: make(chan struct{})}
	run.detach = context.AfterFunc(reqCtx, cancel)
	reg.runs[id] = run
	return run, nil
}

func (run *durableRun) state() runPhase {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.phase
}

// abandon drops a reservation that never started. A no-op once it has.
func (run *durableRun) abandon() {
	run.mu.Lock()
	pending := run.phase == runPending
	run.mu.Unlock()
	if !pending {
		return
	}
	run.detach()
	run.cancel()
	run.reg.mu.Lock()
	if run.reg.runs[run.id] == run {
		delete(run.reg.runs, run.id)
	}
	run.reg.mu.Unlock()
	run.release()
}

// start hands the run to the registry: from here on its context descends from
// the runner's alone, and body runs in a goroutine of its own whatever happens
// to the request.
func (run *durableRun) start(body func(ctx context.Context, frames io.Writer)) *rpcError {
	if !run.reg.enter() {
		return failure(codeCancelled, "this runner is shutting down and is not taking new runs")
	}
	run.mu.Lock()
	if run.phase != runPending {
		run.mu.Unlock()
		run.reg.leave()
		return failure(codeInternal, "run %q was started twice", run.id)
	}
	if run.cancelAsked || run.ctx.Err() != nil || !run.detach() {
		run.mu.Unlock()
		run.reg.leave()
		return failure(codeCancelled, "the call was cancelled before it started")
	}
	run.ring = run.reg.newRing()
	run.phase = runRunning
	run.mu.Unlock()

	go run.execute(body)
	return nil
}

func (run *durableRun) execute(body func(ctx context.Context, frames io.Writer)) {
	defer run.reg.leave()
	defer run.cancel()
	func() {
		defer func() {
			if p := recover(); p != nil {
				log.Error().Str("call", run.id).Str("method", run.method).Interface("panic", p).
					Str("stack", string(debug.Stack())).Msg("a run panicked")
			}
		}()
		body(run.ctx, runFrames{run})
	}()
	run.complete()
}

// complete writes the done a body failed to write and marks the run finished:
// a caller is promised exactly one done, and an attach waits for it.
func (run *durableRun) complete() {
	run.mu.Lock()
	if !run.sawDone {
		code, message := codeInternal, "the run ended without a result"
		if run.ctx.Err() != nil {
			code, message = codeCancelled, "the call was cancelled"
		}
		line, _ := json.Marshal(doneEvent{V: protocolVersion, ID: run.id, Event: "done", OK: false, Error: &rpcError{Code: code, Message: message}})
		run.appendLocked(line)
	}
	run.phase = runDone
	run.notifyLocked()
	run.mu.Unlock()
	run.reg.finish(run)
}

// runFrames is the writer a run's call emits into. call writes exactly one
// frame per Write, which is what lets a frame be the unit of seq and of the
// buffer.
type runFrames struct{ run *durableRun }

func (f runFrames) Write(p []byte) (int, error) {
	f.run.mu.Lock()
	defer f.run.mu.Unlock()
	if f.run.gone {
		return 0, errors.New("the run's buffer is gone")
	}
	f.run.appendLocked(p)
	return len(p), nil
}

// appendLocked numbers a frame and keeps it. Nothing follows a done.
func (run *durableRun) appendLocked(line []byte) {
	if run.sawDone || run.ring == nil || run.gone {
		return
	}
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return
	}
	run.lastSeq++
	run.ring.append(run.lastSeq, withSeq(line, run.lastSeq))
	if bytes.Contains(line, []byte(`"done"`)) && isDoneLine(line) {
		run.sawDone = true
	}
	run.notifyLocked()
}

func (run *durableRun) notifyLocked() {
	close(run.changed)
	run.changed = make(chan struct{})
}

// withSeq adds a top-level seq to one JSON object, keeping its bytes. Last, so
// a seq the frame already carried is the one a decoder discards. A frame that
// does not end in a brace (a line cut at the length limit) gets it first.
func withSeq(line []byte, seq int64) []byte {
	field := `"seq":` + strconv.FormatInt(seq, 10)
	trimmed := bytes.TrimRight(line, " \t")
	out := make([]byte, 0, len(trimmed)+len(field)+3)
	if n := len(trimmed); n >= 2 && trimmed[0] == '{' && trimmed[n-1] == '}' {
		body := bytes.TrimRight(trimmed[:n-1], " \t\r\n")
		out = append(out, body...)
		if body[len(body)-1] != '{' {
			out = append(out, ',')
		}
		out = append(out, field...)
		out = append(out, '}', '\n')
		return out
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		out = append(out, '{')
		out = append(out, field...)
		if rest := trimmed[1:]; len(bytes.TrimSpace(rest)) > 0 && bytes.TrimSpace(rest)[0] != '}' {
			out = append(out, ',')
		}
		out = append(out, trimmed[1:]...)
		return append(out, '\n')
	}
	return append(append(out, trimmed...), '\n')
}

func (run *durableRun) release() {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.gone {
		return
	}
	run.gone = true
	if run.gc != nil {
		run.gc.Stop()
	}
	if run.ring != nil {
		run.ring.close()
	}
	run.notifyLocked()
}

func (reg *runRegistry) enter() bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.closed {
		return false
	}
	reg.active++
	return true
}

func (reg *runRegistry) leave() {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.active--
	if reg.active == 0 && reg.idle != nil {
		close(reg.idle)
		reg.idle = nil
	}
}

func (reg *runRegistry) finish(run *durableRun) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.runs[run.id] != run {
		return
	}
	reg.finished = append(reg.finished, run)
	for len(reg.finished) > maxFinishedRuns {
		reg.forgetLocked(reg.finished[0])
	}
	run.mu.Lock()
	if !run.gone {
		run.gc = time.AfterFunc(reg.retention, func() { reg.forget(run) })
	}
	run.mu.Unlock()
}

func (reg *runRegistry) forget(run *durableRun) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.forgetLocked(run)
}

func (reg *runRegistry) forgetLocked(run *durableRun) {
	if reg.runs[run.id] == run {
		delete(reg.runs, run.id)
	}
	for i, f := range reg.finished {
		if f == run {
			reg.finished = append(reg.finished[:i], reg.finished[i+1:]...)
			break
		}
	}
	run.release()
}

func (reg *runRegistry) newRing() *frameRing {
	if reg.dir == "" {
		return newFrameRing("", reg.limit)
	}
	name, err := randomHex(16)
	if err == nil {
		dir := filepath.Join(reg.dir, name)
		if err = os.Mkdir(dir, 0o700); err == nil {
			return newFrameRing(dir, reg.limit)
		}
	}
	log.Warn().Err(err).Msg("a run's buffer stays in memory: its directory could not be made")
	return newFrameRing("", reg.limit)
}

// lookup answers a started run; a reservation still queued is not one yet.
func (reg *runRegistry) lookup(id string) *durableRun {
	reg.mu.Lock()
	run := reg.runs[id]
	reg.mu.Unlock()
	if run == nil || run.state() == runPending {
		return nil
	}
	return run
}

// cancelRun stops a queued or running run. A finished one is not an error.
func (reg *runRegistry) cancelRun(id string) bool {
	reg.mu.Lock()
	run := reg.runs[id]
	reg.mu.Unlock()
	if run == nil {
		return false
	}
	run.mu.Lock()
	live := run.phase != runDone
	if live {
		run.cancelAsked = true
	}
	run.mu.Unlock()
	if live {
		run.cancel()
	}
	return live
}

// shutdown refuses new runs and cancels every live one. Runs stop their
// process groups on the way out; wait is how a caller knows they have.
func (reg *runRegistry) shutdown() {
	reg.mu.Lock()
	reg.closed = true
	reg.mu.Unlock()
	reg.cancel()
}

func (reg *runRegistry) wait(budget time.Duration) bool {
	reg.mu.Lock()
	if reg.active == 0 {
		reg.mu.Unlock()
		return true
	}
	if reg.idle == nil {
		reg.idle = make(chan struct{})
	}
	idle := reg.idle
	reg.mu.Unlock()
	select {
	case <-idle:
		return true
	case <-time.After(budget):
		return false
	}
}

// close is the runner's last word on runs: cancel, wait out the drain budget,
// and remove every buffer.
func (reg *runRegistry) close() {
	reg.shutdown()
	if !reg.wait(drainBudget) {
		log.Warn().Dur("budget", drainBudget).Msg("runs were still stopping when the runner closed their buffers")
	}
	reg.mu.Lock()
	all := make([]*durableRun, 0, len(reg.runs))
	for _, run := range reg.runs {
		all = append(all, run)
	}
	for _, run := range all {
		reg.forgetLocked(run)
	}
	reg.mu.Unlock()
}

type runStatus struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	State   string `json:"state"`
	LastSeq int64  `json:"last_seq"`
}

func (reg *runRegistry) status(id string) runStatus {
	st := runStatus{V: protocolVersion, ID: id, State: "unknown"}
	run := reg.lookup(id)
	if run == nil {
		return st
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.gone {
		return st
	}
	st.LastSeq = run.lastSeq
	st.State = "running"
	if run.phase == runDone {
		st.State = "done"
	}
	return st
}

type runBatch struct {
	gapFrom, gapTo int64
	frames         []byte
	last           int64
	finished       bool
	gone           bool
	changed        <-chan struct{}
}

// read is one step of a subscriber: what comes after `after`, and whether that
// is everything there will ever be.
func (run *durableRun) read(after int64, max int) runBatch {
	run.mu.Lock()
	defer run.mu.Unlock()
	b := runBatch{last: after, changed: run.changed}
	if run.gone {
		b.gone = true
		return b
	}
	if after < run.lastSeq {
		from, data, last := run.ring.read(after, max)
		if from == 0 {
			from, last = run.lastSeq+1, run.lastSeq
		}
		if from > after+1 {
			b.gapFrom, b.gapTo = after+1, from-1
		}
		b.frames, b.last = data, last
	}
	b.finished = run.phase == runDone && b.last >= run.lastSeq
	return b
}

type gapEvent struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	Event   string `json:"event"`
	FromSeq int64  `json:"from_seq"`
	ToSeq   int64  `json:"to_seq"`
}

// streamRun writes a run's frames after `after` to one caller, live, until
// the done has gone out or the caller is gone. A caller going away ends this
// and nothing else.
func streamRun(ctx context.Context, w io.Writer, flush func(), run *durableRun, after int64) {
	cursor := after
	for {
		b := run.read(cursor, runReadBatch)
		if b.gone {
			return
		}
		wrote := false
		if b.gapFrom > 0 {
			line, _ := json.Marshal(gapEvent{V: protocolVersion, ID: run.id, Event: "gap", FromSeq: b.gapFrom, ToSeq: b.gapTo})
			if _, err := w.Write(append(line, '\n')); err != nil {
				return
			}
			wrote = true
		}
		if len(b.frames) > 0 {
			if _, err := w.Write(b.frames); err != nil {
				log.Debug().Str("call", run.id).Err(err).Msg("a caller stopped reading a run; the run goes on")
				return
			}
			wrote = true
		}
		if wrote && flush != nil {
			flush()
		}
		cursor = b.last
		if b.finished {
			return
		}
		if wrote {
			continue
		}
		select {
		case <-b.changed:
		case <-ctx.Done():
			return
		}
	}
}

// --- the HTTP half ------------------------------------------------------------

// queueDurable reserves the id and waits for a session slot, both on the
// request's terms: until start, a caller that goes away or cancels takes a run
// that never started with it. The release is the caller's until start hands
// it to the run.
func (s *runnerServer) queueDurable(w http.ResponseWriter, r *http.Request, id, method string) (*durableRun, func(), bool) {
	run, rpcErr := s.state.runs.reserve(id, method, r.Context())
	if rpcErr != nil {
		writeError(w, rpcErr)
		return nil, nil, false
	}
	if run.ctx.Err() != nil {
		run.abandon()
		writeError(w, failure(codeCancelled, "the call was cancelled before it started"))
		return nil, nil, false
	}
	// Checked after the semaphore too: a select with both cases ready picks at
	// random, and cancellation has to win that race every time.
	select {
	case s.sem <- struct{}{}:
		if run.ctx.Err() != nil {
			<-s.sem
			run.abandon()
			writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
			return nil, nil, false
		}
	case <-run.ctx.Done():
		run.abandon()
		writeError(w, failure(codeCancelled, "the call was cancelled while it was waiting for a free session slot"))
		return nil, nil, false
	}
	var once sync.Once
	return run, func() { once.Do(func() { <-s.sem }) }, true
}

// serveDurable starts the run and streams it to the caller that asked for it,
// as the first of any number of subscribers. cleanup runs once, in reverse,
// after body returns — or here, when the run never starts.
func (s *runnerServer) serveDurable(w http.ResponseWriter, r *http.Request, run *durableRun, cleanup []func(), body func(ctx context.Context, frames io.Writer)) {
	runCleanup := func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		run.abandon()
		runCleanup()
		writeError(w, failure(codeInternal, "this response cannot be streamed"))
		return
	}
	if startErr := run.start(func(ctx context.Context, frames io.Writer) {
		defer runCleanup()
		body(ctx, frames)
	}); startErr != nil {
		run.abandon()
		runCleanup()
		writeError(w, startErr)
		return
	}
	writeStreamHeaders(w)
	flusher.Flush()
	streamRun(r.Context(), w, flusher.Flush, run, 0)
}

func writeStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
}

type runRef struct {
	ID       string `json:"id"`
	AfterSeq *int64 `json:"after_seq,omitempty"`
}

func readRunRef(r *http.Request) (runRef, *rpcError) {
	body, readErr := readBody(r)
	if readErr != nil {
		return runRef{}, readErr
	}
	var ref runRef
	if err := json.Unmarshal(body, &ref); err != nil {
		return runRef{}, failure(codeBadRequest, "the request body is not the expected object: %v", err)
	}
	if ref.ID == "" {
		return runRef{}, failure(codeBadRequest, "id is required: it names the run")
	}
	if ref.AfterSeq != nil && *ref.AfterSeq < 0 {
		return runRef{}, failure(codeBadRequest, "after_seq %d is negative", *ref.AfterSeq)
	}
	return ref, nil
}

func (s *runnerServer) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	ref, rpcErr := readRunRef(r)
	if rpcErr != nil {
		writeError(w, rpcErr)
		return
	}
	writeJSON(w, http.StatusOK, s.state.runs.status(ref.ID))
}

func (s *runnerServer) handleRunAttach(w http.ResponseWriter, r *http.Request) {
	ref, rpcErr := readRunRef(r)
	if rpcErr != nil {
		writeError(w, rpcErr)
		return
	}
	run := s.state.runs.lookup(ref.ID)
	if run == nil {
		writeError(w, failure(codeUnknownRun, "this machine has no run %q (never started, finished more than %s ago, or the runner restarted)", ref.ID, runRetention))
		return
	}
	if !s.enterRun() {
		writeError(w, failure(codeCancelled, "this machine's tunnel session is shutting down"))
		return
	}
	defer s.leaveRun()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, failure(codeInternal, "this response cannot be streamed"))
		return
	}
	var after int64
	if ref.AfterSeq != nil {
		after = *ref.AfterSeq
	}
	writeStreamHeaders(w)
	flusher.Flush()
	streamRun(r.Context(), w, flusher.Flush, run, after)
}

// --- the buffer -----------------------------------------------------------------

// frameRing keeps a run's newest frames within limit bytes, in segment files
// of a quarter of it: the oldest whole segment goes when the total passes the
// limit. The newest segment is never evicted, so the newest frame — the done,
// at the end — is always there to replay, even one larger than the limit.
type frameRing struct {
	dir      string
	limit    int64
	segLimit int64
	segs     []*ringSegment
	total    int64
	nextSeg  int
	memory   bool
}

type ringSegment struct {
	f      *os.File
	mem    []byte
	path   string
	size   int64
	frames []ringFrame
}

type ringFrame struct {
	seq int64
	off int64
	n   int64
}

func newFrameRing(dir string, limit int64) *frameRing {
	seg := limit / 4
	if seg < 1 {
		seg = 1
	}
	return &frameRing{dir: dir, limit: limit, segLimit: seg, memory: dir == ""}
}

func (r *frameRing) append(seq int64, line []byte) {
	n := int64(len(line))
	cur := r.current()
	if cur == nil || (cur.size > 0 && cur.size+n > r.segLimit) {
		cur = r.rotate()
	}
	if err := cur.write(line); err != nil {
		log.Warn().Err(err).Str("dir", r.dir).Msg("a run's buffer moves to memory: its file could not be written")
		r.memory = true
		cur = r.rotate()
		_ = cur.write(line)
	}
	cur.frames = append(cur.frames, ringFrame{seq: seq, off: cur.size - n, n: n})
	r.total += n
	for r.total > r.limit && len(r.segs) > 1 {
		old := r.segs[0]
		r.segs = r.segs[1:]
		r.total -= old.size
		old.drop()
	}
}

func (r *frameRing) current() *ringSegment {
	if len(r.segs) == 0 {
		return nil
	}
	return r.segs[len(r.segs)-1]
}

func (r *frameRing) rotate() *ringSegment {
	seg := &ringSegment{}
	if !r.memory {
		path := filepath.Join(r.dir, fmt.Sprintf("%08d.ndjson", r.nextSeg))
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			log.Warn().Err(err).Str("dir", r.dir).Msg("a run's buffer moves to memory: a segment could not be created")
			r.memory = true
		} else {
			seg.f, seg.path = f, path
		}
	}
	r.nextSeg++
	r.segs = append(r.segs, seg)
	return seg
}

func (s *ringSegment) write(line []byte) error {
	if s.f == nil {
		s.mem = append(s.mem, line...)
		s.size += int64(len(line))
		return nil
	}
	if _, err := s.f.WriteAt(line, s.size); err != nil {
		return err
	}
	s.size += int64(len(line))
	return nil
}

func (s *ringSegment) drop() {
	if s.f != nil {
		_ = s.f.Close()
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", s.path).Msg("could not remove a run buffer segment")
		}
	}
	s.mem, s.frames = nil, nil
}

// read returns whole frames after `after` from one segment, up to max bytes
// but never less than one frame. from is the first seq returned, 0 for none.
func (r *frameRing) read(after int64, max int) (from int64, data []byte, last int64) {
	for _, seg := range r.segs {
		if len(seg.frames) == 0 || seg.frames[len(seg.frames)-1].seq <= after {
			continue
		}
		i := 0
		if first := seg.frames[0].seq; after >= first {
			i = int(after + 1 - first)
		}
		j := i
		size := seg.frames[i].n
		for j+1 < len(seg.frames) && size+seg.frames[j+1].n <= int64(max) {
			j++
			size += seg.frames[j].n
		}
		start := seg.frames[i].off
		buf := make([]byte, size)
		if seg.f == nil {
			copy(buf, seg.mem[start:start+size])
		} else if _, err := seg.f.ReadAt(buf, start); err != nil {
			log.Warn().Err(err).Str("path", seg.path).Msg("could not read a run buffer segment")
			continue
		}
		return seg.frames[i].seq, buf, seg.frames[j].seq
	}
	return 0, nil, after
}

func (r *frameRing) close() {
	for _, seg := range r.segs {
		seg.drop()
	}
	r.segs = nil
	r.total = 0
	if r.dir != "" {
		if err := os.RemoveAll(r.dir); err != nil {
			log.Warn().Err(err).Str("dir", r.dir).Msg("could not remove a run's buffer")
		}
	}
}
