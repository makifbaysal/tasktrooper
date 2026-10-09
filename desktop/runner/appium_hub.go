package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

// The Appium hub `/mobile.appium/…` drives, run by this runner only while
// something drives it.
//
// A hub is a ~100 MB Node process and most members with Appium installed never
// drive a device, so nothing starts one when the runner does. A proxied call
// and `mobile.boot` start it (`ensure`); once nothing has used it for
// `idleAfter` it is stopped again. A hub that already answers on the address —
// one the member runs, with their own drivers and plugins — is used as it is
// and never stopped: it is not this process's to stop. `mobile.devices` never
// starts one: it is polled by settings pages and sweepers, and a probe that
// started a hub would keep it alive forever.
//
// The same semantics as agent-server's adapter/local/appiumhub, which this
// module cannot import; the differences are the runner's own process layer
// (startProcessGroup, the npm-shim launcher) and its error shape.
//
// "In use" is a proxied call in flight or one within idleAfter. An Appium
// session is not tracked here: the cloud creates them with a
// newCommandTimeout well under idleAfter, so a session nobody has called for
// that long has already been ended by Appium itself.

type hubTimings struct {
	idleAfter time.Duration
	// readyTimeout is generous because the first start after a driver install
	// loads every driver, and XCUITest's is slow to load.
	readyTimeout time.Duration
	reapEvery    time.Duration
	// cooldown hands a caller the previous start's failure instead of another
	// full readyTimeout of its own: a broken install fails the same way twice.
	cooldown  time.Duration
	stopGrace time.Duration
}

var defaultHubTimings = hubTimings{
	idleAfter:    10 * time.Minute,
	readyTimeout: 45 * time.Second,
	reapEvery:    30 * time.Second,
	cooldown:     30 * time.Second,
	stopGrace:    5 * time.Second,
}

const (
	hubStatusTimeout = 2 * time.Second
	hubPollEvery     = 200 * time.Millisecond
)

// appiumHub is nil when this runner starts no hub — no appium_bin — and every
// method is then a no-op, so the proxy reaches whatever the member runs.
type appiumHub struct {
	bin     string
	base    string
	port    string
	args    []string
	environ func() []string
	t       hubTimings
	client  *http.Client
	// recordPath is where the hub this runner started is written down, so the
	// next runner can take back one this runner was killed before stopping.
	// Empty on Windows, where the job object ends the hub with the runner.
	recordPath string
	recordMu   sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	shut   chan struct{}

	mu       sync.Mutex
	proc     *hubProcess
	op       *hubFlight
	inflight int
	lastUsed time.Time
	failErr  *rpcError
	failedAt time.Time
	closed   bool
}

// hubFlight is the one start or idle stop in progress. Every ensure that
// arrives meanwhile waits on it, and a start's result is theirs too.
type hubFlight struct {
	starting bool
	done     chan struct{}
	err      *rpcError
}

func newAppiumHub(cfg config, t hubTimings) *appiumHub {
	if cfg.appiumBin == "" || cfg.appiumBaseURL == "" {
		return nil
	}
	args, err := appiumHubArgs(cfg.appiumBaseURL)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	adb := cfg.adbBin
	return &appiumHub{
		bin:        cfg.appiumBin,
		base:       cfg.appiumBaseURL,
		port:       args[3],
		args:       args,
		environ:    func() []string { return hubEnviron(os.Environ(), adb) },
		t:          t,
		client:     &http.Client{Timeout: hubStatusTimeout},
		recordPath: hubRecordPath(),
		ctx:        ctx,
		cancel:     cancel,
		shut:       make(chan struct{}),
	}
}

// appiumHubArgs is the argv for a hub on base: plain http on loopback, because
// a hub bound anywhere else is a remote-control interface for every device on
// this machine. loadConfig has already refused a base that is not loopback.
func appiumHubArgs(base string) ([]string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" {
		return nil, fmt.Errorf("this runner starts Appium on plain http, not %q", u.Scheme)
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	args := []string{"--address", u.Hostname(), "--port", port}
	if path := strings.TrimRight(u.Path, "/"); path != "" {
		args = append(args, "--base-path", path)
	}
	// Its output is kept for error messages, where escape codes are noise.
	return append(args, "--log-no-colors"), nil
}

// hubEnviron is this process's environment minus the credentials the agent
// CLIs need and a hub has no use for, plus the SDK root the adb that detection
// found belongs to: a GUI-launched runner has no ANDROID_HOME, and the
// UiAutomator2 driver finds adb through it.
func hubEnviron(environ []string, adbBin string) []string {
	out := make([]string, 0, len(environ)+2)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if slices.ContainsFunc(credentialEnvNames, func(secret string) bool { return strings.EqualFold(secret, name) }) {
			continue
		}
		out = append(out, entry)
	}
	if root := androidRootOf(adbBin); root != "" {
		out = append(out, "ANDROID_HOME="+root, "ANDROID_SDK_ROOT="+root)
	}
	return out
}

// androidRootOf mirrors the desktop's androidRootFrom: <root>/platform-tools/adb.
func androidRootOf(adbBin string) string {
	if adbBin == "" {
		return ""
	}
	tools := filepath.Dir(adbBin)
	if filepath.Base(tools) != "platform-tools" {
		return ""
	}
	return filepath.Dir(tools)
}

func hubUnavailable(format string, args ...any) *rpcError {
	return failure(codeUpstream, "the Appium hub on this machine could not be started: "+format, args...)
}

// ensure returns once the hub answers, starting it when nothing does. Every
// call counts as use of the hub.
func (h *appiumHub) ensure(ctx context.Context) *rpcError {
	if h == nil {
		return nil
	}
	for {
		h.mu.Lock()
		h.lastUsed = time.Now()
		if h.closed {
			h.mu.Unlock()
			return failure(codeCancelled, "this runner is shutting down and starts no Appium hub")
		}
		if h.op == nil {
			if h.proc != nil && h.proc.running() {
				h.mu.Unlock()
				return nil
			}
			if h.failErr != nil && time.Since(h.failedAt) < h.t.cooldown {
				err := h.failErr
				h.mu.Unlock()
				return err
			}
			h.op = &hubFlight{starting: true, done: make(chan struct{})}
			go h.bringUp(h.op)
		}
		op := h.op
		h.mu.Unlock()

		select {
		case <-op.done:
		case <-ctx.Done():
			return failure(codeCancelled, "the call was cancelled while the Appium hub was starting")
		}
		if op.starting {
			return op.err
		}
	}
}

// ensureAsync starts ensure beside work that does not need the hub yet, so a
// device boot and a cold hub start overlap.
func (h *appiumHub) ensureAsync(ctx context.Context) <-chan *rpcError {
	out := make(chan *rpcError, 1)
	if h == nil {
		out <- nil
		return out
	}
	go func() { out <- h.ensure(ctx) }()
	return out
}

// hold marks a call in flight; the idle stop never pulls the hub out from
// under one. The returned func ends it.
func (h *appiumHub) hold() func() {
	if h == nil {
		return func() {}
	}
	h.mu.Lock()
	h.inflight++
	h.lastUsed = time.Now()
	h.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			h.inflight--
			h.lastUsed = time.Now()
			h.mu.Unlock()
		})
	}
}

// recentFailure is the last start's failure while ensure would still answer
// with it.
func (h *appiumHub) recentFailure() *rpcError {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failErr != nil && time.Since(h.failedAt) < h.t.cooldown {
		return h.failErr
	}
	return nil
}

func (h *appiumHub) bringUp(op *hubFlight) {
	p, err := h.start()
	h.settle(op, p, err)
}

func (h *appiumHub) settle(op *hubFlight, p *hubProcess, err *rpcError) {
	h.mu.Lock()
	h.op = nil
	if err != nil {
		h.failErr, h.failedAt = err, time.Now()
	} else {
		h.proc, h.failErr = p, nil
	}
	h.mu.Unlock()
	if p != nil {
		go h.watchIdle(p)
	}
	op.err = err
	close(op.done)
}

// resume takes back, at startup, a hub a previous runner started and was
// killed before stopping, so it idles out like any hub this runner started
// instead of waiting for a call that may never come. Not a start: an ensure
// that arrives meanwhile waits for it and then asks again.
func (h *appiumHub) resume() {
	if h == nil || h.recordPath == "" {
		return
	}
	h.mu.Lock()
	if h.closed || h.op != nil || h.proc != nil {
		h.mu.Unlock()
		return
	}
	op := &hubFlight{done: make(chan struct{})}
	h.op = op
	h.lastUsed = time.Now()
	h.mu.Unlock()
	go func() {
		var p *hubProcess
		if h.answers(h.ctx) {
			p = h.reclaim()
		}
		h.settle(op, p, nil)
	}()
}

// start adopts a hub that already answers, or spawns one and waits for it. A
// nil process with a nil error is the adopted case, asked again on the next
// ensure: the member may have stopped theirs in the meantime. One the record
// proves a previous runner started is not adopted but taken back.
func (h *appiumHub) start() (*hubProcess, *rpcError) {
	if h.answers(h.ctx) {
		if p := h.reclaim(); p != nil {
			return p, nil
		}
		log.Debug().Str("hub", h.base).Msg("an Appium hub is already answering; using it as it is")
		return nil, nil
	}
	began := time.Now()
	p, err := h.spawn()
	if err != nil {
		callErr := hubUnavailable("could not start %s: %v", h.bin, err)
		log.Warn().Str("hub", h.base).Msg(callErr.Message)
		return nil, callErr
	}
	if callErr := h.waitReady(p); callErr != nil {
		p.stop(h.t.stopGrace)
		if callErr.Code != codeCancelled {
			log.Warn().Str("hub", h.base).Msg(callErr.Message)
		}
		return nil, callErr
	}
	p.ready.Store(true)
	h.remember(p)
	log.Info().Int("pid", p.pid).Str("hub", h.base).Dur("took", time.Since(began)).Msg("appium hub started")
	return p, nil
}

func (h *appiumHub) spawn() (*hubProcess, error) {
	cmd, err := commandFor(h.bin, h.args...)
	if err != nil {
		return nil, err
	}
	cmd.Env = h.environ()
	out := &hubOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	// A driver's own children inherit these pipes, and Wait would otherwise
	// hold out for the last of them.
	cmd.WaitDelay = h.t.stopGrace
	group, err := startProcessGroup(cmd)
	if err != nil {
		return nil, err
	}
	p := &hubProcess{pid: cmd.Process.Pid, cmd: cmd, group: group, out: out, done: make(chan struct{})}
	go func() {
		p.exit = cmd.Wait()
		group.release()
		p.markDone()
		h.exited(p)
	}()
	return p, nil
}

func (h *appiumHub) exited(p *hubProcess) {
	h.mu.Lock()
	if h.proc == p {
		h.proc = nil
	}
	h.mu.Unlock()
	h.forget(p)
	if p.ready.Load() && !p.stopping.Load() {
		log.Warn().Err(p.exit).Str("output", p.out.tail()).Msg("the Appium hub exited; the next Appium call starts it again")
	}
}

func (h *appiumHub) waitReady(p *hubProcess) *rpcError {
	deadline := time.NewTimer(h.t.readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(hubPollEvery)
	defer tick.Stop()
	for {
		if h.answers(h.ctx) {
			return nil
		}
		select {
		case <-p.done:
			return hubUnavailable("appium exited before it answered on %s (%v)%s", h.base, p.exit, p.out.suffix())
		case <-deadline.C:
			return hubUnavailable("appium did not answer on %s within %s%s", h.base, h.t.readyTimeout, p.out.suffix())
		case <-h.ctx.Done():
			return failure(codeCancelled, "this runner is shutting down and starts no Appium hub")
		case <-tick.C:
		}
	}
}

func (h *appiumHub) answers(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, hubStatusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/status", nil)
	if err != nil {
		return false
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (h *appiumHub) watchIdle(p *hubProcess) {
	tick := time.NewTicker(h.t.reapEvery)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-h.ctx.Done():
			return
		case <-tick.C:
			if h.stopIfIdle(p) {
				return
			}
		}
	}
}

// stopIfIdle stops p when it is still the hub this process started and
// nothing has used it for idleAfter. It becomes the flight while it stops, so
// an ensure arriving meanwhile waits and then starts a fresh hub rather than
// being handed one that is going away.
func (h *appiumHub) stopIfIdle(p *hubProcess) bool {
	h.mu.Lock()
	if h.closed || h.proc != p || h.op != nil || h.inflight > 0 || time.Since(h.lastUsed) < h.t.idleAfter {
		h.mu.Unlock()
		return false
	}
	op := &hubFlight{done: make(chan struct{})}
	h.op, h.proc = op, nil
	h.mu.Unlock()

	log.Info().Dur("idle", h.t.idleAfter).Msg("the Appium hub is unused; stopping it until an Appium call needs it again")
	p.stop(h.t.stopGrace)
	h.forget(p)

	h.mu.Lock()
	h.op = nil
	h.mu.Unlock()
	close(op.done)
	return true
}

// close stops the hub this process started; an adopted one is left running.
// Safe to call more than once and from several goroutines: every caller
// returns once the hub is down.
func (h *appiumHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		<-h.shut
		return
	}
	h.closed = true
	op := h.op
	h.mu.Unlock()

	h.cancel()
	if op != nil {
		<-op.done
	}
	h.mu.Lock()
	p := h.proc
	h.proc = nil
	h.mu.Unlock()
	if p != nil {
		log.Info().Msg("stopping the Appium hub with the runner")
		p.stop(h.t.stopGrace)
		h.forget(p)
	}
	close(h.shut)
}

// hubProcess is a hub this runner owns: one it spawned (cmd set), or one a
// previous runner spawned and the record proves is still that process (cmd
// nil, identified by pid and start time).
type hubProcess struct {
	pid      int
	start    string
	cmd      *exec.Cmd
	group    *processGroup
	out      *hubOutput
	done     chan struct{}
	doneOnce sync.Once
	exit     error
	ready    atomic.Bool
	stopping atomic.Bool
}

func (p *hubProcess) markDone() { p.doneOnce.Do(func() { close(p.done) }) }

func (p *hubProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *hubProcess) stop(grace time.Duration) {
	p.stopping.Store(true)
	if p.cmd == nil {
		stopLeftover(p, grace)
	} else {
		killProcessGroup(p.group, grace, p.done)
	}
	select {
	case <-p.done:
	case <-time.After(claudeReapTimeout):
		log.Warn().Int("pid", p.pid).Msg("the Appium hub would not exit after being killed")
	}
}

const (
	// hubLeftoverPoll is how often a taken-back hub, which is not this
	// process's child and so cannot be waited for, is asked whether it is
	// still there.
	hubLeftoverPoll  = time.Second
	hubRecordMaxSize = 4 << 10
)

// hubRecord is what the next runner needs to tell the hub this runner started
// from one somebody else runs on the same port: a pid alone may be anybody's
// by then; the start time and the command line make it this one.
type hubRecord struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Hub   string `json:"hub"`
}

func (h *appiumHub) remember(p *hubProcess) {
	if h.recordPath == "" {
		return
	}
	start, _, err := processIdentity(h.ctx, p.pid)
	if err != nil {
		log.Debug().Err(err).Int("pid", p.pid).Msg("could not read the Appium hub's start time; a runner killed before stopping it will leave it to its own")
		return
	}
	p.start = start
	raw, err := json.Marshal(hubRecord{PID: p.pid, Start: start, Hub: h.base})
	if err != nil {
		return
	}
	h.recordMu.Lock()
	defer h.recordMu.Unlock()
	if err := writeHubRecord(h.recordPath, raw); err != nil {
		log.Warn().Err(err).Str("path", h.recordPath).Msg("could not record the Appium hub this runner started")
	}
}

// forget removes the record when it still names p: by the time an exited hub
// is noticed, a newer one may have been written down.
func (h *appiumHub) forget(p *hubProcess) {
	if h.recordPath == "" {
		return
	}
	h.recordMu.Lock()
	defer h.recordMu.Unlock()
	if rec, ok := h.readRecord(); ok && rec.PID == p.pid {
		_ = os.Remove(h.recordPath)
	}
}

func (h *appiumHub) readRecord() (hubRecord, bool) {
	f, err := os.Open(h.recordPath)
	if err != nil {
		return hubRecord{}, false
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, hubRecordMaxSize))
	if err != nil {
		return hubRecord{}, false
	}
	var rec hubRecord
	if json.Unmarshal(raw, &rec) != nil {
		return hubRecord{}, false
	}
	return rec, true
}

// reclaim is the hub answering on our address when the record proves a
// previous runner started it: same address, a live pid, the same start time,
// and a command line that is appium on our port. Anything short of all four
// is somebody else's hub, adopted and never stopped.
func (h *appiumHub) reclaim() *hubProcess {
	if h.recordPath == "" {
		return nil
	}
	h.recordMu.Lock()
	rec, ok := h.readRecord()
	h.recordMu.Unlock()
	if !ok || rec.Hub != h.base || rec.PID <= 1 || rec.Start == "" || !pidAlive(rec.PID) {
		return nil
	}
	start, command, err := processIdentity(h.ctx, rec.PID)
	if err != nil || start != rec.Start || !isOurHubCommand(command, h.port) {
		return nil
	}
	p := &hubProcess{pid: rec.PID, start: start, out: &hubOutput{}, done: make(chan struct{})}
	p.ready.Store(true)
	go h.watchLeftover(p)
	log.Info().Int("pid", p.pid).Str("hub", h.base).
		Msg("the Appium hub a previous runner started is still running; this runner stops it once idle")
	return p
}

func isOurHubCommand(command, port string) bool {
	return strings.Contains(command, "appium") && strings.Contains(" "+command+" ", " --port "+port+" ")
}

func (h *appiumHub) watchLeftover(p *hubProcess) {
	tick := time.NewTicker(hubLeftoverPoll)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-tick.C:
			if !pidAlive(p.pid) {
				p.markDone()
				h.exited(p)
				return
			}
		}
	}
}

// writeHubRecord replaces the record whole: a reader never sees half of one.
func writeHubRecord(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".appium-hub-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
	}
	return werr
}

const (
	hubTailLines = 12
	hubTailWidth = 300
	// hubMaxPartial bounds a line that never ends: at Appium's default level a
	// page source is logged whole.
	hubMaxPartial = 64 << 10
)

// hubOutput keeps the hub's last few lines, which are what explain a start
// that failed. Nothing else of it is kept: at its default level Appium logs
// every request, far too much for the log the supervisor reads.
type hubOutput struct {
	mu      sync.Mutex
	partial []byte
	lines   []string
}

func (o *hubOutput) Write(b []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rest := append(o.partial, b...)
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		o.keep(string(rest[:i]))
		rest = rest[i+1:]
	}
	if len(rest) > hubMaxPartial {
		o.keep(string(rest))
		rest = nil
	}
	o.partial = append(o.partial[:0], rest...)
	return len(b), nil
}

func (o *hubOutput) keep(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	o.lines = append(o.lines, clipLine(line, hubTailWidth))
	if len(o.lines) > hubTailLines {
		o.lines = o.lines[len(o.lines)-hubTailLines:]
	}
}

func (o *hubOutput) tail() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	lines := append([]string(nil), o.lines...)
	if partial := strings.TrimSpace(string(o.partial)); partial != "" {
		lines = append(lines, clipLine(partial, hubTailWidth))
	}
	return strings.Join(lines, " | ")
}

func (o *hubOutput) suffix() string {
	if tail := o.tail(); tail != "" {
		return ": " + tail
	}
	return ""
}

func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
