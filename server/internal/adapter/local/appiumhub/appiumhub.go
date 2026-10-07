// Package appiumhub runs the Appium server the mobile_* tools drive, and only
// while they are driving it.
//
// A hub is a ~100 MB Node process and most machines with Appium installed
// never drive a device, so nothing starts one at boot: the first tool call
// that needs it does (Ensure), and once nothing has called for IdleAfter and
// no device lease is open, it is stopped again. A hub that already answers on
// the address — one the user runs, with their own drivers and plugins — is
// used as it is and never stopped: it is not this process's to stop.
package appiumhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/winshim"
)

const (
	DefaultIdleAfter = 10 * time.Minute
	// DefaultReadyTimeout is generous because the first start after a driver
	// install loads every driver, and XCUITest's is slow to load.
	DefaultReadyTimeout = 45 * time.Second
	defaultReapEvery    = 30 * time.Second

	stopGrace     = 5 * time.Second
	statusTimeout = 2 * time.Second
	pollEvery     = 200 * time.Millisecond
	leftoverGrace = 3 * time.Second

	// failureCooldown hands a caller the previous caller's failure instead of
	// another full ReadyTimeout: the pool walks every registered phone, all on
	// this one hub, and would otherwise pay it once per phone.
	failureCooldown = 30 * time.Second
)

// passThrough are variables Appium reads that the child allowlist does not
// carry: where its drivers are installed, and which Xcode xcrun resolves to.
var passThrough = []string{"APPIUM_HOME", "DEVELOPER_DIR"}

var errClosed = errors.New("the server is shutting down")

type Config struct {
	// Bin is the appium executable. Empty means the hub at HubURL is somebody
	// else's to run, and Ensure leaves it alone.
	Bin    string
	HubURL string
	// WorkDir is the hub's working directory, and how a hub a crashed server
	// left behind is recognised at the next Start and stopped — otherwise it
	// would answer /status, be taken for the user's own and run forever.
	WorkDir      string
	IdleAfter    time.Duration
	ReadyTimeout time.Duration
	ReapEvery    time.Duration
	Environ      func() []string
}

type Manager struct {
	bin     string
	addr    hubAddr
	args    []string
	workDir string

	idleAfter    time.Duration
	readyTimeout time.Duration
	reapEvery    time.Duration
	environ      func() []string
	client       *http.Client
	now          func() time.Time

	killLeftovers func(dir string, grace time.Duration) (int, error)

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	proc     *process
	op       *flight
	lastUsed time.Time
	failErr  error
	failedAt time.Time
	inUse    func() bool
	started  bool
	closed   bool
	reaper   chan struct{}
}

// flight is the one start, stop or leftover sweep in progress. Every Ensure
// that arrives meanwhile waits on it, and a start's result is theirs too.
type flight struct {
	starting bool
	done     chan struct{}
	err      error
}

func New(cfg Config) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		workDir:       cfg.WorkDir,
		idleAfter:     orDefault(cfg.IdleAfter, DefaultIdleAfter),
		readyTimeout:  orDefault(cfg.ReadyTimeout, DefaultReadyTimeout),
		reapEvery:     orDefault(cfg.ReapEvery, defaultReapEvery),
		environ:       cfg.Environ,
		client:        &http.Client{Timeout: statusTimeout},
		now:           time.Now,
		killLeftovers: proctree.KillProcessesUnder,
		ctx:           ctx,
		cancel:        cancel,
	}
	if m.environ == nil {
		m.environ = os.Environ
	}
	bin := strings.TrimSpace(cfg.Bin)
	if bin == "" {
		return m
	}
	addr, err := parseHub(cfg.HubURL)
	if err != nil {
		log.Warn().Err(err).Str("hub_url", cfg.HubURL).
			Msg("appium hub: APPIUM_BIN is set but this address cannot be served from this machine; treating it as an external hub")
		return m
	}
	m.bin, m.addr, m.args = bin, addr, addr.args()
	return m
}

func orDefault(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

// Manages reports whether the hub at hubURL is one this process starts on
// demand — so that nothing answering there means nothing is leased on it.
func (m *Manager) Manages(hubURL string) bool {
	if m == nil || m.bin == "" {
		return false
	}
	other, err := parseHub(hubURL)
	return err == nil && other == m.addr
}

// SetInUse names what still holds the hub besides tool calls: an open device
// lease. Called without the manager's lock held, so it may take its own.
func (m *Manager) SetInUse(fn func() bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.inUse = fn
	m.mu.Unlock()
}

// Ensure returns once the hub at hubURL answers, starting it when it is
// managed and nothing does. Every call counts as use of the hub.
func (m *Manager) Ensure(ctx context.Context, hubURL string) error {
	if !m.Manages(hubURL) {
		return nil
	}
	for {
		m.mu.Lock()
		m.lastUsed = m.now()
		if m.closed {
			m.mu.Unlock()
			return errClosed
		}
		if m.op == nil {
			if m.proc != nil && m.proc.running() {
				m.mu.Unlock()
				return nil
			}
			if m.failErr != nil && m.now().Sub(m.failedAt) < failureCooldown {
				err := m.failErr
				m.mu.Unlock()
				return err
			}
			m.op = &flight{starting: true, done: make(chan struct{})}
			go m.bringUp(m.op)
		}
		op := m.op
		m.mu.Unlock()

		select {
		case <-op.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if op.starting {
			return op.err
		}
	}
}

func (m *Manager) bringUp(op *flight) {
	proc, err := m.start()
	m.mu.Lock()
	m.op = nil
	if err != nil {
		m.failErr, m.failedAt = err, m.now()
	} else {
		m.proc, m.failErr = proc, nil
	}
	m.mu.Unlock()
	op.err = err
	close(op.done)
}

// start adopts a hub that already answers, or spawns one and waits for it.
// A nil process with a nil error is the adopted case.
func (m *Manager) start() (*process, error) {
	if m.answers(m.ctx) {
		log.Info().Str("hub", m.addr.base()).Msg("appium hub: one is already answering; using it as it is")
		return nil, nil
	}
	began := time.Now()
	p, err := m.spawn()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", m.bin, err)
	}
	if err := m.waitReady(p); err != nil {
		p.stop(stopGrace)
		return nil, err
	}
	p.ready.Store(true)
	log.Info().Int("pid", p.tree.Pid()).Str("hub", m.addr.base()).Dur("took", time.Since(began)).
		Msg("appium hub started")
	return p, nil
}

func (m *Manager) spawn() (*process, error) {
	if m.workDir != "" {
		if err := os.MkdirAll(m.workDir, 0o700); err != nil {
			return nil, err
		}
	}
	environ := m.environ()
	cmd := winshim.Cmd(m.bin, m.args...)
	cmd.Dir = m.workDir
	cmd.Env = childenv.For(environ, passedThrough(environ))
	out := &output{}
	cmd.Stdout, cmd.Stderr = out, out
	tree, err := proctree.Start(cmd)
	if err != nil {
		return nil, err
	}
	p := &process{tree: tree, out: out, done: make(chan struct{})}
	go func() {
		p.exit = cmd.Wait()
		close(p.done)
		m.exited(p)
	}()
	return p, nil
}

func passedThrough(environ []string) []string {
	var out []string
	for _, entry := range environ {
		if name, _, ok := strings.Cut(entry, "="); ok && slices.Contains(passThrough, name) {
			out = append(out, entry)
		}
	}
	return out
}

func (m *Manager) exited(p *process) {
	m.mu.Lock()
	if m.proc == p {
		m.proc = nil
	}
	m.mu.Unlock()
	if p.ready.Load() && !p.stopping.Load() {
		log.Warn().Err(p.exit).Str("output", p.out.tail()).
			Msg("appium hub exited; the next mobile tool call starts it again")
	}
	// Whatever it started that is still in its tree goes with it.
	p.tree.Close()
}

func (m *Manager) waitReady(p *process) error {
	deadline := time.NewTimer(m.readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		if m.answers(m.ctx) {
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("appium exited before it answered on %s (%v)%s", m.addr.base(), p.exit, p.out.suffix())
		case <-deadline.C:
			return fmt.Errorf("appium did not answer on %s within %s%s", m.addr.base(), m.readyTimeout, p.out.suffix())
		case <-m.ctx.Done():
			return errClosed
		case <-tick.C:
		}
	}
}

func (m *Manager) answers(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.addr.base()+"/status", nil)
	if err != nil {
		return false
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode == http.StatusOK
}

// Start begins the idle reaper, after stopping any hub a previous server left
// in WorkDir. Ensure works without it; nothing is then ever stopped for idling.
func (m *Manager) Start() {
	if m == nil || m.bin == "" {
		return
	}
	m.mu.Lock()
	if m.started || m.closed {
		m.mu.Unlock()
		return
	}
	m.started = true
	var leftovers *flight
	if m.workDir != "" && m.op == nil && m.proc == nil {
		leftovers = &flight{done: make(chan struct{})}
		m.op = leftovers
	}
	m.reaper = make(chan struct{})
	m.mu.Unlock()
	go m.run(leftovers)
}

func (m *Manager) run(leftovers *flight) {
	defer close(m.reaper)
	if leftovers != nil {
		if n, err := m.killLeftovers(m.workDir, leftoverGrace); err != nil {
			log.Warn().Err(err).Str("dir", m.workDir).Msg("appium hub: could not look for a hub a previous server left running")
		} else if n > 0 {
			log.Info().Int("count", n).Msg("appium hub: stopped a hub a previous server left running")
		}
		m.mu.Lock()
		m.op = nil
		m.mu.Unlock()
		close(leftovers.done)
	}
	t := time.NewTicker(m.reapEvery)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.reapIdle()
		}
	}
}

// reapIdle stops the hub this process started once nothing has called for
// idleAfter and no lease is open. The idle check is repeated under the lock
// after asking about leases, because an Ensure in between is a caller that
// is about to use the hub this would otherwise pull out from under it.
func (m *Manager) reapIdle() {
	m.mu.Lock()
	p, inUse := m.proc, m.inUse
	idle := p != nil && m.op == nil && m.now().Sub(m.lastUsed) >= m.idleAfter
	m.mu.Unlock()
	if !idle || (inUse != nil && inUse()) {
		return
	}

	m.mu.Lock()
	if m.closed || m.proc != p || m.op != nil || m.now().Sub(m.lastUsed) < m.idleAfter {
		m.mu.Unlock()
		return
	}
	op := &flight{done: make(chan struct{})}
	m.op, m.proc = op, nil
	m.mu.Unlock()

	log.Info().Dur("idle", m.idleAfter).Msg("appium hub: unused, stopping it until a mobile tool needs it again")
	p.stop(stopGrace)

	m.mu.Lock()
	m.op = nil
	m.mu.Unlock()
	close(op.done)
}

// Close stops the hub this process started; an adopted one is left running.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	op, reaper := m.op, m.reaper
	m.mu.Unlock()

	m.cancel()
	if op != nil {
		<-op.done
	}
	if reaper != nil {
		<-reaper
	}
	m.mu.Lock()
	p := m.proc
	m.proc = nil
	m.mu.Unlock()
	if p != nil {
		log.Info().Msg("appium hub: stopping it with the server")
		p.stop(stopGrace)
	}
}

type process struct {
	tree     *proctree.Tree
	out      *output
	done     chan struct{}
	exit     error
	ready    atomic.Bool
	stopping atomic.Bool
}

func (p *process) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *process) stop(grace time.Duration) {
	p.stopping.Store(true)
	p.tree.Terminate(grace)
	p.tree.Close()
	select {
	case <-p.done:
	case <-time.After(proctree.DefaultWaitDelay + time.Second):
	}
}

// hubAddr is a hub address this process can serve: plain http on loopback. A
// hub bound anywhere else is a remote-control interface for every device on
// this machine, reachable from whatever network it is on.
type hubAddr struct {
	host string
	port string
	path string
}

func parseHub(raw string) (hubAddr, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return hubAddr{}, err
	}
	if u.Scheme != "http" {
		return hubAddr{}, fmt.Errorf("scheme %q is not http", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if !loopback(host) {
		return hubAddr{}, fmt.Errorf("host %q is not loopback", host)
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	return hubAddr{host: host, port: port, path: strings.TrimRight(u.Path, "/")}, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a hubAddr) base() string {
	return "http://" + net.JoinHostPort(a.host, a.port) + a.path
}

func (a hubAddr) args() []string {
	args := []string{"--address", a.host, "--port", a.port}
	if a.path != "" {
		args = append(args, "--base-path", a.path)
	}
	// Its output is kept for error messages and debug logs, where escape
	// codes are noise.
	return append(args, "--log-no-colors")
}
