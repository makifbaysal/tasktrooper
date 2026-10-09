//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

// The tunnel is this program's whole job, and the way it fails is quiet: a
// session ends, the reconnect loop stalls or backs off forever, and the Mac
// stops running its member's tasks without anything looking broken. So the tests
// in this file drive the real reconnect loop against a real HTTP server, a
// real WebSocket upgrade and a real yamux client. Nothing here is mocked,
// because every failure worth catching lives in the interaction between those
// three and a mocked transport would prove none of it.

const testToken = "test-runner-token-must-never-be-logged"

// ---------------------------------------------------------------------------
// an in-process control plane
// ---------------------------------------------------------------------------

// dialAttempt records one arrival at the gateway's tunnel endpoint, whether or
// not it was allowed to upgrade.
type dialAttempt struct {
	at     time.Time
	path   string
	query  string
	header http.Header
}

// fakeGateway stands in for the control plane: it serves the tunnel endpoint,
// upgrades to WebSocket and runs the yamux CLIENT side (the gateway opens
// streams; the runner only ever accepts them).
type fakeGateway struct {
	srv *httptest.Server

	// reject decides what happens to the nth dial, counting from 1: a non-zero
	// HTTP status refuses it, zero upgrades it. A nil reject upgrades
	// everything. It runs on the server goroutine, so a reject that sleeps
	// stalls that dial — which is how a slow refusal is simulated.
	reject func(attempt int) int

	mu       sync.Mutex
	attempts []dialAttempt

	sessions chan *yamux.Session
	done     chan struct{}
}

func newFakeGateway(t *testing.T, reject func(attempt int) int) *fakeGateway {
	t.Helper()
	g := &fakeGateway{
		reject:   reject,
		sessions: make(chan *yamux.Session, 8),
		done:     make(chan struct{}),
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(func() {
		close(g.done)
		g.srv.Close()
	})
	return g
}

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.attempts = append(g.attempts, dialAttempt{
		at:     time.Now(),
		path:   r.URL.Path,
		query:  r.URL.RawQuery,
		header: r.Header.Clone(),
	})
	n := len(g.attempts)
	g.mu.Unlock()

	if r.URL.Path != tunnelPath {
		http.Error(w, "no such endpoint", http.StatusNotFound)
		return
	}
	if g.reject != nil {
		if code := g.reject(n); code != 0 {
			http.Error(w, http.StatusText(code), code)
			return
		}
	}

	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(-1)
	session, err := yamux.Client(websocket.NetConn(context.Background(), ws, websocket.MessageBinary), quietYamux())
	if err != nil {
		ws.Close(websocket.StatusInternalError, "yamux client failed")
		return
	}
	select {
	case g.sessions <- session:
	default:
	}
	// Hold the hijacked connection open until the session ends or the test
	// does; returning here would leave nobody to close it.
	select {
	case <-session.CloseChan():
	case <-g.done:
	}
	_ = session.Close()
}

func (g *fakeGateway) baseURL() string { return g.srv.URL }

func (g *fakeGateway) snapshot() []dialAttempt {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]dialAttempt(nil), g.attempts...)
}

func (g *fakeGateway) dials() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.attempts)
}

// awaitDials blocks until the endpoint has seen at least n dials.
func (g *fakeGateway) awaitDials(t *testing.T, n int, within time.Duration) []dialAttempt {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if got := g.snapshot(); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the control plane saw %d dials in %s, want at least %d", g.dials(), within, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitSession blocks until the runner has attached a tunnel.
func (g *fakeGateway) awaitSession(t *testing.T, within time.Duration) *yamux.Session {
	t.Helper()
	select {
	case s := <-g.sessions:
		return s
	case <-time.After(within):
		t.Fatalf("no tunnel attached within %s (%d dials reached the control plane)", within, g.dials())
		return nil
	}
}

func quietYamux() *yamux.Config {
	c := yamux.DefaultConfig()
	c.LogOutput = io.Discard
	return c
}

// ---------------------------------------------------------------------------
// an in-process agent-server
// ---------------------------------------------------------------------------

// fakeTooling writes the two binaries a config must name — the desktop app
// detects them and passes their paths, so this test has to supply real,
// executable files rather than plausible strings.
//
// A shell script, not a compiled fake: what these tests need is a process that
// really is spawned with a real process group, and `/bin/sh` gives that for
// two lines. `claude` here prints one line and then blocks forever, which is
// what makes it useful for the concurrency and cancellation tests.
func fakeTooling(t *testing.T) (workspace, claudeBin, gitBin string) {
	t.Helper()
	dir := t.TempDir()

	claudeBin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\necho started-a-session\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(claudeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}
	gitBin = filepath.Join(dir, "git")
	if err := os.WriteFile(gitBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the fake git: %v", err)
	}

	workspace = filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "repo"), 0o755); err != nil {
		t.Fatalf("creating the workspace: %v", err)
	}
	return workspace, claudeBin, gitBin
}

// ---------------------------------------------------------------------------
// shared fixtures
// ---------------------------------------------------------------------------

// testConfig builds the runner's configuration the way the supervisor does —
// one JSON document, parsed by loadConfig — so these tests exercise the real
// parse and the real http->ws rewrite rather than a hand-built struct. The
// base URL carries a path, a query and a fragment on purpose: what the runner
// actually dials must be the fixed tunnel path with neither of the other two.
func testConfig(t *testing.T, baseURL string) config {
	t.Helper()
	workspace, claudeBin, gitBin := fakeTooling(t)
	doc, err := json.Marshal(wireConfig{
		TMBaseURL:         baseURL + "/ignored/path?debug=1#frag",
		RunnerToken:       testToken,
		TenantID:          "tenant-under-test",
		MemberUID:         "member-under-test",
		WorkspaceDir:      workspace,
		ClaudeBin:         claudeBin,
		GitBin:            gitBin,
		EmbeddingsBaseURL: "http://127.0.0.1:1234/v1",
		EmbeddingModel:    "nomic-embed-text-v1.5",
	})
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	cfg, err := loadConfig(doc)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	return cfg
}

// tunnelClient is how the control plane actually reaches this Mac: an
// http.Transport whose only job is to open a yamux stream instead of dialling a
// socket. That is precisely what `httputil.ReverseProxy` sits on at the other
// end, so these tests exercise the real framing rather than a convention two
// files agreed on.
//
// The host in the URL is a placeholder — nothing resolves it, because
// DialContext never looks at it — but HTTP requires one.
func tunnelClient(session *yamux.Session) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) { return session.Open() },
		},
	}
}

// tunnelCall makes one request over the tunnel and reads the whole answer.
func tunnelCall(t *testing.T, client *http.Client, method, path, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://runner"+path, reader)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s over the tunnel: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the response: %v", err)
	}
	return res.StatusCode, string(raw)
}

// ---------------------------------------------------------------------------
// the session
// ---------------------------------------------------------------------------

// The whole path in one test: the config document names a base URL, the runner
// rewrites it, dials it, presents its credentials, accepts the streams the
// control plane opens and answers each one as HTTP.
//
// `preflight.report` is the method used here because its whole answer comes
// from the pipe the supervisor writes — so a pass proves the control channel,
// the HTTP framing and the tunnel all line up, without spawning anything.
func TestConnectAndServeAnswersHTTPFromTheControlPlane(t *testing.T) {
	gw := newFakeGateway(t, nil)
	cfg := testConfig(t, gw.baseURL())
	st := newState()
	st.setPreflight(json.RawMessage(`{"ready":true,"items":[{"id":"claude","status":"ok"}]}`))
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		up  time.Duration
		err error
	}
	served := make(chan result, 1)
	go func() {
		up, err := connectAndServe(ctx, cfg, st)
		served <- result{up, err}
	}()

	session := gw.awaitSession(t, 10*time.Second)
	client := tunnelClient(session)

	const calls = 3
	for i := 0; i < calls; i++ {
		status, body := tunnelCall(t, client, http.MethodGet, "/preflight.report", "")
		if status != http.StatusOK {
			t.Fatalf("call %d answered %d: %s", i, status, body)
		}
		var report map[string]any
		if err := json.Unmarshal([]byte(body), &report); err != nil {
			t.Fatalf("the report is not JSON: %v", err)
		}
		if report["ready"] != true {
			t.Fatalf("the report did not survive the trip: %s", body)
		}
	}

	// An unknown path is answered, not dropped. A control plane that has moved
	// ahead of this build must learn that rather than time out — and it gets
	// both a status and the code string, so it can branch on either.
	status, body := tunnelCall(t, client, http.MethodPost, "/something.new", "{}")
	if status != http.StatusNotFound {
		t.Fatalf("an unknown path answered %d, want 404", status)
	}
	var failed errorBody
	if err := json.Unmarshal([]byte(body), &failed); err != nil || failed.Error.Code != codeUnsupportedMethod {
		t.Fatalf("an unknown path answered %q, want the %s code in the body", body, codeUnsupportedMethod)
	}

	cancel()
	var res result
	select {
	case res = <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("connectAndServe did not return after its context was cancelled")
	}
	if res.err != nil {
		t.Fatalf("connectAndServe: %v", res.err)
	}
	if res.up <= 0 {
		t.Fatalf("connectAndServe reported %s of uptime for a session that attached and served %d calls", res.up, calls)
	}

	dials := gw.snapshot()
	if len(dials) != 1 {
		t.Fatalf("the control plane saw %d dials, want 1", len(dials))
	}
	// The rewrite, observed from the far end: the fixed tunnel path, and none
	// of the query the base URL carried.
	if dials[0].path != tunnelPath {
		t.Fatalf("runner dialed %q, want %q", dials[0].path, tunnelPath)
	}
	if dials[0].query != "" {
		t.Fatalf("runner dialed with query %q, want it discarded", dials[0].query)
	}
	if got, want := dials[0].header.Get("Authorization"), "Bearer "+testToken; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got, want := dials[0].header.Get("X-Runner-Tenant"), cfg.tenantID; got != want {
		t.Fatalf("X-Runner-Tenant = %q, want %q", got, want)
	}
	// The member, alongside the tenant: a tenant may have several paired Macs
	// and the control plane routes a run to the one belonging to the member the
	// task is assigned to.
	if got, want := dials[0].header.Get("X-Runner-Member"), cfg.memberUID; got != want {
		t.Fatalf("X-Runner-Member = %q, want %q", got, want)
	}

	out := logs.String()
	if strings.Contains(out, testToken) {
		t.Fatalf("the runner token reached the log output:\n%s", out)
	}
	// The supervisor reads these two lines as tunnel state, and the stream
	// count off the second one.
	var attached, detached bool
	for _, rec := range logRecords(t, out) {
		switch rec["message"] {
		case "tunnel attached":
			attached = true
		case "tunnel detached":
			detached = true
			// Connections, not calls: with HTTP the transport pools them, so
			// several of the calls above rode one stream. What the field has
			// always meant is what this session carried, and it still does.
			if streams, ok := rec["streams"].(float64); !ok || streams < 1 {
				t.Fatalf(`"tunnel detached" reported streams=%v, want at least 1`, rec["streams"])
			}
		}
	}
	if !attached || !detached {
		t.Fatalf("a whole session logged attached=%v detached=%v:\n%s", attached, detached, out)
	}
}

// maxConcurrentSessions is the only thing standing between a control plane that
// asks for work faster than this Mac can do it and a laptop running fifty
// compilers.
//
// The bound moved with the protocol, and moving it was the point. It used to
// count STREAMS, because a stream was a call; with HTTP a stream is a pooled
// connection that may carry many requests, so counting those would bound the
// wrong thing. What is counted here is what is actually expensive: the number
// of `claude` processes running at once. The fake CLI blocks forever, so that
// number is exactly the number of calls in flight.
func TestConnectAndServeBoundsConcurrentSessions(t *testing.T) {
	gw := newFakeGateway(t, nil)
	cfg := testConfig(t, gw.baseURL())
	st := newState()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	served := make(chan struct{})
	go func() {
		defer close(served)
		_, _ = connectAndServe(ctx, cfg, st)
	}()

	session := gw.awaitSession(t, 10*time.Second)
	client := tunnelClient(session)

	// Each request asks for a session that never ends. Every accepted one holds
	// its slot; the rest wait, having sent no bytes back.
	//
	// The reader keeps the response body OPEN for the life of the test, and
	// that is not incidental: closing it is itself a cancellation now, so a
	// reader that stopped as soon as it had counted would free the slot it was
	// there to occupy and let the next queued call through — which is a
	// perfectly good test of the wrong thing.
	const overshoot = 8
	var started atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < maxConcurrentSessions+overshoot; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body := `{"id":"c` + strconv.Itoa(n) + `","workspace":"repo","prompt":"go"}`
			res, err := client.Post("http://runner/claude.run", "application/json", strings.NewReader(body))
			if err != nil {
				return
			}
			defer func() { _ = res.Body.Close() }()
			// The fake claude's own first line, not the runner's `started`
			// frame: reading it is what proves the PROCESS is running rather
			// than merely that the request was accepted.
			scanner := bufio.NewScanner(res.Body)
			counted := false
			for scanner.Scan() {
				if !counted && strings.Contains(scanner.Text(), "started-a-session") {
					started.Add(1)
					counted = true
				}
			}
		}(i)
	}

	// Give the runner every chance to overshoot: wait until it has saturated
	// the semaphore, then keep watching for a while longer.
	deadline := time.Now().Add(30 * time.Second)
	for started.Load() < maxConcurrentSessions && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)

	if got := started.Load(); got != maxConcurrentSessions {
		t.Fatalf("%d Claude Code sessions were started for %d open calls, want exactly the %d-session cap",
			got, maxConcurrentSessions+overshoot, maxConcurrentSessions)
	}

	cancel()
	select {
	case <-served:
	case <-time.After(60 * time.Second):
		t.Fatal("connectAndServe did not return after its context was cancelled")
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// the reconnect loop
// ---------------------------------------------------------------------------

// fastTimings compresses the reconnect loop's clock so a test can watch several
// reconnects without spending a minute of wall clock on each one.
func fastTimings(floor, stable time.Duration) timings {
	return timings{minBackoff: floor, stableAfter: stable}
}

// A rejected token is the one failure retrying cannot fix. Looping on it
// achieves nothing, buries the single log line that says what to do, and is a
// good way to be rate-limited by the control plane.
func TestRunTreatsAuthRejectionAsFatalAndNeverRetries(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			gw := newFakeGateway(t, func(int) int { return status })
			cfg := testConfig(t, gw.baseURL())

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := run(ctx, cfg, newState(), fastTimings(10*time.Millisecond, time.Hour))

			var authErr *authError
			if !errors.As(err, &authErr) {
				t.Fatalf("run against an HTTP %d control plane returned %v, want an *authError", status, err)
			}
			if authErr.status != status {
				t.Fatalf("authError.status = %d, want %d", authErr.status, status)
			}
			if n := gw.dials(); n != 1 {
				t.Fatalf("the control plane saw %d dials for a rejected token, want exactly 1", n)
			}
			// The supervisor matches this text to tell the user to re-pair
			// (../src/main/supervisor/runner-log.ts, isAuthRejection).
			if !strings.Contains(err.Error(), "control plane rejected the connection") {
				t.Fatalf("auth error reads %q; the supervisor matches on \"control plane rejected the connection\"", err)
			}
		})
	}
}

// Anything that is not an auth rejection is retried, and the delay grows and
// then stops growing. The numbers are chosen so the two failure modes are far
// apart: with a floor of 25ms and a 50ms cap the delays are 25, 50, 50, 50 …
// which is at least eight dials in 500ms, while a cap that leaked would give
// 25, 50, 100, 200 — five at most. The lower bounds are the strict ones: a gap
// shorter than the floor is a hot reconnect loop against the control plane,
// which is the thing backoff exists to prevent.
func TestRunBackoffGrowsAndIsCapped(t *testing.T) {
	const (
		floor   = 25 * time.Millisecond
		ceiling = 50 * time.Millisecond
		watch   = 500 * time.Millisecond
	)
	gw := newFakeGateway(t, func(int) int { return http.StatusInternalServerError })
	cfg := testConfig(t, gw.baseURL())
	// Set directly: loadConfig floors the configured ceiling at minBackoff,
	// which is a rule about the config document (tested in main_test.go), not
	// about the loop.
	cfg.maxBackoff = ceiling

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, newState(), fastTimings(floor, time.Hour)) }()

	time.Sleep(watch)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v after a cancelled context, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}

	dials := gw.snapshot()
	if len(dials) < 8 {
		t.Fatalf("only %d dials in %s with a %s floor and a %s cap — the cap is not holding", len(dials), watch, floor, ceiling)
	}
	for i := 1; i < len(dials); i++ {
		gap := dials[i].at.Sub(dials[i-1].at)
		if gap < floor-2*time.Millisecond {
			t.Fatalf("dial %d followed dial %d after %s, below the %s floor", i+1, i, gap, floor)
		}
	}
	// The second gap is the doubled one; the first is the floor.
	if grown := dials[2].at.Sub(dials[1].at); grown < 2*floor-2*time.Millisecond {
		t.Fatalf("the second retry waited %s, want the doubled %s — the backoff is not growing", grown, 2*floor)
	}
}

// A session that stood for a while was working, so whatever broke it is a new
// problem and gets a fresh delay. Without the reset, a Mac that reconnects
// once an hour ends up waiting the maximum every time.
func TestRunResetsBackoffAfterAStableSession(t *testing.T) {
	const (
		floor  = 20 * time.Millisecond
		stable = 50 * time.Millisecond
		hold   = 120 * time.Millisecond
	)
	// Five refusals first, so the delay climbs 20, 40, 80, 160, 320ms and the
	// next one would be another 320. Then the tunnel attaches.
	gw := newFakeGateway(t, func(n int) int {
		if n <= 5 {
			return http.StatusInternalServerError
		}
		return 0
	})
	cfg := testConfig(t, gw.baseURL())
	cfg.maxBackoff = 320 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, newState(), fastTimings(floor, stable)) }()

	session := gw.awaitSession(t, 20*time.Second)
	time.Sleep(hold) // stay up well past the stability threshold
	closedAt := time.Now()
	_ = session.Close()

	dials := gw.awaitDials(t, 7, 20*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}

	delay := dials[6].at.Sub(closedAt)
	if delay < floor-2*time.Millisecond {
		t.Fatalf("reconnected %s after the session dropped, below the %s floor", delay, floor)
	}
	if delay > 150*time.Millisecond {
		t.Fatalf("reconnected %s after a %s session ended; the backoff did not reset to the %s floor "+
			"(the run of failures before it had built the delay up to %s)", delay, hold, floor, cfg.maxBackoff)
	}
}

// The stability reset keys off how long the tunnel was ATTACHED, not off how
// long the attempt took. A control plane that accepts the TCP connection, sits
// on the request and then refuses it can burn more than the threshold without
// ever attaching a tunnel; counting that as a stable session pins the delay at
// the floor and turns a struggling control plane into a hammered one.
func TestRunDoesNotCountASlowFailedDialAsUptime(t *testing.T) {
	const (
		floor  = 20 * time.Millisecond
		stable = 30 * time.Millisecond
		stall  = 60 * time.Millisecond
	)
	gw := newFakeGateway(t, func(int) int {
		time.Sleep(stall) // longer than the stability threshold, and still a refusal
		return http.StatusInternalServerError
	})
	cfg := testConfig(t, gw.baseURL())
	cfg.maxBackoff = 320 * time.Millisecond
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, newState(), fastTimings(floor, stable)) }()

	dials := gw.awaitDials(t, 5, 20*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}

	// Delays of 20, 40, 80 and 160ms on top of a 60ms stall. Were the stalled
	// dials counted as stable sessions, every gap would be the 80ms floor plus
	// stall instead.
	if grown := dials[4].at.Sub(dials[3].at); grown < 180*time.Millisecond {
		t.Fatalf("the fourth retry came %s after the third; with a %s stall and a %s floor the delay should have "+
			"grown to about %s — a dial that never attached is being treated as a stable session",
			grown, stall, floor, stall+8*floor)
	}
	// Said directly, and without a stopwatch: a dial that never attached has
	// no uptime, and the supervisor renders this field.
	var warnings int
	for _, rec := range logRecords(t, logs.String()) {
		if rec["message"] != "tunnel session ended, reconnecting" {
			continue
		}
		warnings++
		if rec["uptime"] != float64(0) {
			t.Fatalf("a dial that never attached logged uptime=%v, want 0", rec["uptime"])
		}
	}
	if warnings == 0 {
		t.Fatalf("no reconnect warning was logged:\n%s", logs.String())
	}
}

// Shutdown is a drain: the supervisor stops the runner first so the control
// plane sees a clean close rather than a timeout, which only works if a
// cancelled context takes the session down with it.
func TestRunShutsDownCleanlyOnContextCancel(t *testing.T) {
	gw := newFakeGateway(t, nil)
	cfg := testConfig(t, gw.baseURL())
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, newState(), fastTimings(10*time.Millisecond, time.Hour)) }()

	session := gw.awaitSession(t, 10*time.Second)
	awaitLog(t, logs, "tunnel attached", 10*time.Second)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v on shutdown, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}
	select {
	case <-session.CloseChan():
	case <-time.After(10 * time.Second):
		t.Fatal("the control plane's side of the session was never closed — a cancelled runner must hang up, not abandon the tunnel")
	}

	if n := gw.dials(); n != 1 {
		t.Fatalf("the control plane saw %d dials, want 1 — a shutting-down runner must not reconnect", n)
	}
	out := logs.String()
	for _, want := range []string{"tunnel attached", "tunnel detached", "shut down cleanly"} {
		if !strings.Contains(out, want) {
			t.Fatalf("a clean shutdown never logged %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tunnel session ended, reconnecting") {
		t.Fatalf("a cancelled runner logged the reconnect warning, which the supervisor renders as a fault:\n%s", out)
	}
}

// Quitting the app is the most ordinary thing a user does with this daemon,
// and it used to leave an "[ERR] yamux: Failed to read header …" in the log
// tail every time. A log that cries wolf on every clean quit is a log people
// learn to skip, and it is the one they will be reading when something is
// actually wrong.
func TestCleanShutdownLogsNothingAtErrorLevel(t *testing.T) {
	gw := newFakeGateway(t, nil)
	cfg := testConfig(t, gw.baseURL())
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, newState(), fastTimings(10*time.Millisecond, time.Hour)) }()

	session := gw.awaitSession(t, 10*time.Second)
	awaitLog(t, logs, "tunnel attached", 10*time.Second)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v on shutdown, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after its context was cancelled")
	}
	select {
	case <-session.CloseChan():
	case <-time.After(10 * time.Second):
		t.Fatal("the control plane's side of the session was never closed")
	}
	// yamux complains from its own goroutines, a moment behind the shutdown
	// that provoked it.
	time.Sleep(100 * time.Millisecond)

	// logRecords fails on any line that is not JSON: after the shutdown, every
	// line in this log is still a record, yamux's included.
	for _, rec := range logRecords(t, logs.String()) {
		switch rec["level"] {
		case "warn", "error", "fatal", "panic":
			t.Fatalf("a clean shutdown logged a %v-level record %q — this is the log the user reads when something is genuinely wrong:\n%s",
				rec["level"], rec["message"], logs.String())
		}
	}
	// Nothing in this whole run may have written raw text to stderr — yamux's
	// default logger is the only thing here that ever did, and it no longer
	// has that output.
	if raw := strings.TrimSpace(testStderr.String()); raw != "" {
		t.Fatalf("raw text reached stderr; the supervisor can only pass it through unparsed into the log tail:\n%s", raw)
	}
}
