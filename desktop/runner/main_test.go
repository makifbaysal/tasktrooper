package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// testSink is where every record this binary logs ends up. The package logger
// is a global and the goroutines writing through it — yamux's, now, among this
// program's own — can outlive the test that started them, so the logger is
// installed exactly once and only its DESTINATION moves afterwards (see
// captureLogs). Reassigning log.Logger per test would be a data race against
// whichever of those goroutines is mid-line.
var testSink = &switchableWriter{to: io.Discard}

type switchableWriter struct {
	mu sync.Mutex
	to io.Writer
}

func (w *switchableWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.to.Write(p)
}

func (w *switchableWriter) redirect(to io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.to = to
}

// testStderr collects everything this process writes through os.Stderr, which
// should be nothing at all: the supervisor reads this program's stdout as
// records, and raw text on stderr is a line it can only pass through unparsed
// — which is what yamux's default logger produced on every clean quit before
// yamuxLogger existed.
//
// The swap happens once, here, and never again: os.Stderr is a global that
// yamux.DefaultConfig reads every time a session starts, so moving it while
// one is live is a race in the harness rather than a finding about the
// program. Note this redirects the os.Stderr *variable* — a runtime panic
// still goes straight to fd 2, so a crash is not swallowed.
var testStderr = &syncBuffer{}

// TestMain installs the log sink and the stderr trap. What this program logs
// is a contract with the supervisor and several tests assert on it — those
// redirect the sink, see captureLogs — but a run that dumps every session's
// JSON to stdout buries its own failures.
func TestMain(m *testing.M) {
	// Started with Appium's own first flag, this binary is the fake appium the
	// hub tests drive (appium_hub_test.go), not a test run.
	if len(os.Args) > 1 && os.Args[1] == "--address" {
		os.Exit(fakeAppium(os.Args[1:]))
	}
	// Started with no arguments at all and this variable set, it is the fake
	// executor the executor tests drive (executor_test.go). A test run always
	// has -test.* flags, so the two can never be confused.
	if mode := os.Getenv(fakeExecutorEnv); len(os.Args) == 1 && mode != "" {
		os.Exit(fakeExecutor(mode))
	}
	log.Logger = zerolog.New(testSink).With().Timestamp().Logger()

	r, w, err := os.Pipe()
	if err != nil {
		panic("os.Pipe for the stderr trap: " + err.Error())
	}
	os.Stderr = w
	go func() { _, _ = io.Copy(testStderr, r) }()

	os.Exit(m.Run())
}

func TestToTunnelURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https", "https://tasktrooper.ai", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"http", "http://localhost:8081", "ws://localhost:8081/internal/runner/tunnel", false},
		{"https with existing path", "https://tasktrooper.ai/some/path", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"https with query", "https://tasktrooper.ai?foo=bar", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"path, query and fragment together", "https://tasktrooper.ai/v1/x?token=leak#frag", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"fragment only", "https://tasktrooper.ai#frag", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"port survives", "http://127.0.0.1:8081", "ws://127.0.0.1:8081/internal/runner/tunnel", false},
		{"already ws", "ws://tasktrooper.ai", "ws://tasktrooper.ai/internal/runner/tunnel", false},
		{"already wss", "wss://tasktrooper.ai", "wss://tasktrooper.ai/internal/runner/tunnel", false},
		{"unsupported scheme", "ftp://tasktrooper.ai", "", true},
		{"no host", "https://", "", true},
		{"not a url", "://not a url", "", true},
		// A bare host is the shape someone types by hand, and it must fail at
		// startup rather than become a relative dial nobody can explain.
		{"no scheme", "tasktrooper.ai", "", true},
		{"host:port with no scheme", "tasktrooper.ai:8081", "", true},
		{"empty", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toTunnelURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("toTunnelURL(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("toTunnelURL(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("toTunnelURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNextBackoff(t *testing.T) {
	max := 30 * time.Second
	cases := []struct {
		cur  time.Duration
		want time.Duration
	}{
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{16 * time.Second, 30 * time.Second}, // capped, not 32s
		{30 * time.Second, 30 * time.Second}, // stays capped
	}
	for _, tc := range cases {
		if got := defaultTimings.nextBackoff(tc.cur, max); got != tc.want {
			t.Fatalf("nextBackoff(%s, %s) = %s, want %s", tc.cur, max, got, tc.want)
		}
	}
}

func TestNextBackoffFloor(t *testing.T) {
	// A max below the floor must not produce a delay shorter than minBackoff:
	// a zero or sub-second retry is a hot loop against the control plane.
	if got := defaultTimings.nextBackoff(minBackoff, 100*time.Millisecond); got != minBackoff {
		t.Fatalf("nextBackoff floored at %s, want %s", got, minBackoff)
	}
}

// The happy path, and the shape of it matters as much as the values: the whole
// configuration arrives as the FIRST LINE on stdin, and nothing is read from
// the environment or from argv.
const validConfig = `{"tm_base_url":"https://tasktrooper.ai","runner_token":"tok-123","tenant_id":"tenant-abc","member_uid":"member-7","workspace_dir":"/Users/x/TaskTrooper","claude_bin":"/opt/homebrew/bin/claude","git_bin":"/usr/bin/git","embeddings_base_url":"http://127.0.0.1:1234","embedding_model":"nomic-embed-text-v1.5"}`

// configWithout returns validConfig with one field replaced, so a table of
// rejections does not have to restate nine fields to change one.
func configWith(t *testing.T, changes map[string]any) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(validConfig), &doc); err != nil {
		t.Fatalf("the fixture is not valid JSON: %v", err)
	}
	for k, v := range changes {
		if v == nil {
			delete(doc, k)
			continue
		}
		doc[k] = v
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encoding the fixture: %v", err)
	}
	return string(out)
}

func TestLoadConfigFromStdin(t *testing.T) {
	cfg, err := loadConfig([]byte(validConfig))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.tunnelURL != "wss://tasktrooper.ai/internal/runner/tunnel" {
		t.Fatalf("tunnelURL = %q", cfg.tunnelURL)
	}
	if cfg.runnerToken != "tok-123" {
		t.Fatalf("runnerToken = %q", cfg.runnerToken)
	}
	if cfg.tenantID != "tenant-abc" {
		t.Fatalf("tenantID = %q", cfg.tenantID)
	}
	// The field that arrived with teams. A tenant may have several paired
	// Macs, one per member, and the control plane routes a task's run by the
	// member it is assigned to.
	if cfg.memberUID != "member-7" {
		t.Fatalf("memberUID = %q", cfg.memberUID)
	}
	if cfg.workspaceDir != "/Users/x/TaskTrooper" {
		t.Fatalf("workspaceDir = %q", cfg.workspaceDir)
	}
	if cfg.claudeBin != "/opt/homebrew/bin/claude" || cfg.gitBin != "/usr/bin/git" {
		t.Fatalf("binaries = %q, %q", cfg.claudeBin, cfg.gitBin)
	}
	if cfg.embeddingModel != "nomic-embed-text-v1.5" {
		t.Fatalf("embeddingModel = %q", cfg.embeddingModel)
	}
	if cfg.maxBackoff != defaultMaxBackoff {
		t.Fatalf("maxBackoff = %s, want the %s default", cfg.maxBackoff, defaultMaxBackoff)
	}
}

func TestLoadConfigOptionalBackoff(t *testing.T) {
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{"reconnect_max_backoff": "2m"})))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.maxBackoff != 2*time.Minute {
		t.Fatalf("maxBackoff = %s, want 2m", cfg.maxBackoff)
	}
}

// This Mac's embedding engine runs on the user's own machine, and proxying to
// it is only an embeddings proxy for as long as it cannot be pointed anywhere
// else. A base URL naming another host would make this method a
// general-purpose request forwarder that the control plane's configuration
// aims.
func TestLoadConfigRefusesANonLoopbackEmbeddingsBase(t *testing.T) {
	for _, addr := range []string{"http://10.0.0.4:1234/v1", "https://api.openai.com/v1", "http://embedder.internal/v1"} {
		if _, err := loadConfig([]byte(configWith(t, map[string]any{"embeddings_base_url": addr}))); err == nil {
			t.Fatalf("embeddings_base_url %q was accepted; that turns this into a request forwarder", addr)
		}
	}
	for _, addr := range []string{"http://127.0.0.1:1234/v1", "http://localhost:1234/v1", "http://[::1]:1234/v1"} {
		if _, err := loadConfig([]byte(configWith(t, map[string]any{"embeddings_base_url": addr}))); err != nil {
			t.Fatalf("embeddings_base_url %q: %v", addr, err)
		}
	}
}

// A Mac with no local embedding engine configured is an ordinary case, not a
// misconfiguration — the two fields are optional, independently and together,
// and embeddings.create is what answers not_ready for their absence (see
// TestEmbeddingsReportNotReadyWithNoLocalEngine in methods_test.go).
func TestLoadConfigAcceptsMissingEmbeddingsFields(t *testing.T) {
	for _, changes := range []map[string]any{
		{"embeddings_base_url": nil, "embedding_model": nil},
		{"embeddings_base_url": nil},
		{"embedding_model": nil},
	} {
		cfg, err := loadConfig([]byte(configWith(t, changes)))
		if err != nil {
			t.Fatalf("loadConfig(%v): %v", changes, err)
		}
		if _, gone := changes["embeddings_base_url"]; gone && cfg.embeddingsBaseURL != "" {
			t.Fatalf("embeddingsBaseURL = %q, want empty", cfg.embeddingsBaseURL)
		}
		if _, gone := changes["embedding_model"]; gone && cfg.embeddingModel != "" {
			t.Fatalf("embeddingModel = %q, want empty", cfg.embeddingModel)
		}
	}
}

// A member who works only with their own API keys, or with OpenCode or
// Cursor, has no Claude Code; their runner still starts.
func TestLoadConfigAcceptsMissingClaudeBin(t *testing.T) {
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{"claude_bin": nil})))
	if err != nil {
		t.Fatalf("loadConfig without claude_bin: %v", err)
	}
	if cfg.claudeBin != "" {
		t.Fatalf("claudeBin = %q, want empty", cfg.claudeBin)
	}
}

// Every one of these is a startup error with a message, not a zero value
// quietly wired in: a runner that dials nowhere with an empty token is far
// harder to diagnose than one that refuses to start.
func TestLoadConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty stdin", "", "no config on stdin"},
		{"whitespace only", "   \n\t ", "no config on stdin"},
		{"not json", "tm_base_url=https://x", "not the expected JSON document"},
		{"json array", `["nope"]`, "not the expected JSON document"},
		{"json null", `null`, "tm_base_url is required"},
		// A document that stops mid-way is what a pipe that died looks like.
		{"truncated document", `{"tm_base_url":"https://x","runner_`, "not the expected JSON document"},
		{"a second document after the first", validConfig + `{"runner_token":"other"}`, "trailing data"},
		{
			// A field this build does not know is a supervisor and a binary
			// that disagree about the contract. Ignoring it is how a renamed
			// field becomes a runner using a stale default.
			"unknown field",
			configWith(t, map[string]any{"log_format": "json"}),
			"not the expected JSON document",
		},
		{
			// This runner has no antigravity flavor, and the field is absent
			// rather than ignored: a supervisor sending antigravity_bin
			// disagrees with this build about what it can run, and
			// DisallowUnknownFields is what makes that fail loudly at startup
			// instead of silently wiring in a capability it does not have.
			"antigravity_bin is refused as unknown",
			configWith(t, map[string]any{"antigravity_bin": "/usr/local/bin/antigravity"}),
			"not the expected JSON document",
		},
		{"missing tm_base_url", configWith(t, map[string]any{"tm_base_url": nil}), "tm_base_url is required"},
		{"blank tm_base_url", configWith(t, map[string]any{"tm_base_url": "  "}), "tm_base_url is required"},
		{"bad tm_base_url scheme", configWith(t, map[string]any{"tm_base_url": "ftp://x"}), "unsupported scheme"},
		{"tm_base_url is not a URL", configWith(t, map[string]any{"tm_base_url": "tasktrooper.ai"}), "unsupported scheme"},
		{"missing runner_token", configWith(t, map[string]any{"runner_token": nil}), "runner_token is required"},
		{"empty runner_token", configWith(t, map[string]any{"runner_token": ""}), "runner_token is required"},
		{"missing tenant_id", configWith(t, map[string]any{"tenant_id": nil}), "tenant_id is required"},
		{"blank tenant_id", configWith(t, map[string]any{"tenant_id": "  \t"}), "tenant_id is required"},
		{"missing member_uid", configWith(t, map[string]any{"member_uid": nil}), "member_uid is required"},
		{"blank member_uid", configWith(t, map[string]any{"member_uid": " "}), "member_uid is required"},
		{"missing workspace_dir", configWith(t, map[string]any{"workspace_dir": nil}), "workspace_dir is required"},
		{
			// Every path a caller names is resolved against this root and then
			// required to still be inside it. A relative root would make that
			// containment depend on a working directory nobody set.
			"relative workspace_dir",
			configWith(t, map[string]any{"workspace_dir": "TaskTrooper"}),
			"must be an absolute path",
		},
		{"relative claude_bin", configWith(t, map[string]any{"claude_bin": "claude"}), "must be an absolute path"},
		{"missing git_bin", configWith(t, map[string]any{"git_bin": nil}), "git_bin is required"},
		{"bad backoff", configWith(t, map[string]any{"reconnect_max_backoff": "soon"}), "reconnect_max_backoff"},
		{"backoff below floor", configWith(t, map[string]any{"reconnect_max_backoff": "10ms"}), "below the 1s floor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfig([]byte(tc.in))
			if err == nil {
				t.Fatalf("loadConfig(%q) succeeded, want an error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadConfig error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// The supervisor writes the document down a real pipe. A pipe that closes
// part-way through — the supervisor crashed, or was killed mid-write — must be
// a startup error, not a config half-built from whatever arrived. This uses a
// real os.Pipe because that is the descriptor this program is actually given.
func TestLoadConfigRejectsStdinClosedMidDocument(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	go func() {
		_, _ = w.WriteString(`{"tm_base_url":"https://tasktrooper.ai","runner_to`)
		w.Close()
	}()

	line, err := readLine(bufio.NewReaderSize(r, configReadLimit))
	if err != nil {
		t.Fatalf("readLine on a half-written document: %v", err)
	}
	if _, err := loadConfig(line); err == nil {
		t.Fatal("loadConfig on a half-written document succeeded, want an error")
	} else if !strings.Contains(err.Error(), "not the expected JSON document") {
		t.Fatalf("loadConfig error = %q, want it to name the malformed document", err)
	}
}

// A pipe that closes before anything arrived is not an empty document and not
// a malformed one: it must surface as what it is, rather than as a confident
// message about a missing field.
func TestReadLineReportsAClosedPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	w.Close()

	if _, err := readLine(bufio.NewReaderSize(r, configReadLimit)); err == nil {
		t.Fatal("readLine on a closed pipe succeeded, want an error")
	} else if !strings.Contains(err.Error(), "stdin closed") {
		t.Fatalf("readLine error = %q, want it to say the pipe closed", err)
	}
}

// A config document is a few hundred bytes and a preflight report a few
// kilobytes. Reading an unbounded line would make this process's memory a
// function of what the parent writes, which is not a thing a supervised child
// should allow.
func TestReadLineBoundsTheLine(t *testing.T) {
	huge := strings.Repeat("a", configReadLimit+10)
	_, err := readLine(bufio.NewReaderSize(strings.NewReader(huge), configReadLimit))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("readLine on an oversized line = %v, want a size error", err)
	}
}

// The preflight report is produced by the desktop app, not by this program,
// and it arrives on the same pipe the configuration did — so a line after the
// first has to be told apart from the first, and an unusable one must not take
// the tunnel down with it.
func TestReadControlStoresThePreflightReport(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	st := newState()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		readControl(ctx, bufio.NewReaderSize(r, configReadLimit), st)
	}()

	// A line this build does not understand, then a malformed one, then a real
	// report. The first two must be survivable: a control message a future
	// supervisor invents cannot be allowed to kill the tunnel.
	_, _ = w.WriteString("{\"type\":\"from-the-future\"}\n")
	_, _ = w.WriteString("not json at all\n")
	_, _ = w.WriteString(`{"type":"preflight","report":{"ready":true,"items":[]}}` + "\n")
	w.Close()
	<-done

	report := st.getPreflight()
	if len(report) == 0 {
		t.Fatal("no preflight report was stored")
	}
	var parsed map[string]any
	if err := json.Unmarshal(report, &parsed); err != nil {
		t.Fatalf("the stored report is not JSON: %v", err)
	}
	if parsed["ready"] != true {
		t.Fatalf("the report was altered on the way through: %v", parsed)
	}
}

// stdin closing is a shutdown request. It is the only one a Windows supervisor
// can make without killing the drain outright, and on every OS it is how this
// process learns the app that started it has died.
func TestTheControlChannelClosingShutsTheRunnerDown(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	st := newState()
	ctx := watchControl(context.Background(), bufio.NewReaderSize(r, configReadLimit), st)

	_, _ = w.WriteString(`{"type":"preflight","report":{"ready":true}}` + "\n")
	deadline := time.Now().Add(2 * time.Second)
	for len(st.getPreflight()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(st.getPreflight()) == 0 {
		t.Fatal("the report written before the close was never stored")
	}
	if ctx.Err() != nil {
		t.Fatal("the runner was shut down while its control channel was still open")
	}

	w.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stdin closed and the runner kept running; an orphaned runner is one nobody can stop")
	}
}

// A line too long to read is skipped, not taken as the channel ending: the
// line after it still arrives, and the close after that is still seen.
func TestAnOverlongControlLineIsSkippedNotFatal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()

	st := newState()
	ctx := watchControl(context.Background(), bufio.NewReaderSize(r, configReadLimit), st)
	go func() {
		_, _ = w.WriteString(strings.Repeat("x", 3*configReadLimit) + "\n")
		_, _ = w.WriteString(`{"type":"preflight","report":{"after":"the long line"}}` + "\n")
	}()

	deadline := time.Now().Add(5 * time.Second)
	for len(st.getPreflight()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(string(st.getPreflight()), "the long line") {
		t.Fatalf("the line after an over-long one was lost: %q", st.getPreflight())
	}
	if ctx.Err() != nil {
		t.Fatal("an over-long control line shut the runner down")
	}

	w.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the close after an over-long line was never seen")
	}
}

// The token must never appear in a log line. It is the one value in the
// config whose disclosure hands someone else this tenant's tunnel.
func TestLoggerNeverLogsToken(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{"runner_token": "super-secret-token"})))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	// The lines connectAndServe emits around a session, reproduced here
	// because the real ones need a live control plane.
	logger.Debug().Str("url", cfg.tunnelURL).Msg("dialing control plane")
	logger.Info().Msg("tunnel attached")
	logger.Info().Int("streams", 3).Msg("tunnel detached")

	if strings.Contains(buf.String(), "super-secret-token") {
		t.Fatalf("the runner token reached the log output:\n%s", buf.String())
	}
}

// The supervisor parses these lines as state. One JSON object per line, the
// message under "message", structured fields surviving as fields.
func TestLogOutputIsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()
	logger.Info().Int("streams", 3).Msg("tunnel detached")

	line := strings.TrimSpace(buf.String())
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("log line %q is not JSON: %v", line, err)
	}
	if got["message"] != "tunnel detached" {
		t.Fatalf("message = %v, want %q (the supervisor keys off this field)", got["message"], "tunnel detached")
	}
	if got["level"] != "info" {
		t.Fatalf("level = %v, want info", got["level"])
	}
	if got["streams"] != float64(3) {
		t.Fatalf("streams = %v, want 3 — structured fields must survive as fields", got["streams"])
	}
	if _, ok := got["time"]; !ok {
		t.Fatal("log line has no time field")
	}
}

// yamux has opinions about a connection going away that this program does not
// share, and it states them as errors. Which of them is really an error
// depends on whether this process is the one that pulled the connection out
// from under it.
func TestYamuxRecordLevels(t *testing.T) {
	// The exact line the mux writes when the websocket is closed under it,
	// which is what a clean quit looks like from inside yamux.
	const readHeader = "[ERR] yamux: Failed to read header: failed to get reader: context canceled"
	// And one that means the tunnel is dying for a reason worth reading.
	const keepalive = "[ERR] yamux: keepalive failed: i/o timeout"
	const backlog = "[WARN] yamux: backlog exceeded, forcing connection reset"

	cases := []struct {
		name         string
		line         string
		shuttingDown bool
		wantLevel    zerolog.Level
		wantMsg      string
		wantLogged   bool
	}{
		{"read failure while shutting down", readHeader, true, zerolog.DebugLevel,
			"yamux: Failed to read header: failed to get reader: context canceled", true},
		{"read failure during a live session", readHeader, false, zerolog.ErrorLevel,
			"yamux: Failed to read header: failed to get reader: context canceled", true},
		{"keepalive failure is a real error", keepalive, false, zerolog.ErrorLevel,
			"yamux: keepalive failed: i/o timeout", true},
		{"keepalive failure while shutting down", keepalive, true, zerolog.DebugLevel,
			"yamux: keepalive failed: i/o timeout", true},
		{"warning during a live session", backlog, false, zerolog.WarnLevel,
			"yamux: backlog exceeded, forcing connection reset", true},
		{"warning while shutting down", backlog, true, zerolog.DebugLevel,
			"yamux: backlog exceeded, forcing connection reset", true},
		{"untagged", "yamux: something it narrates", false, zerolog.DebugLevel,
			"yamux: something it narrates", true},
		{"trailing newline is trimmed", keepalive + "\n", false, zerolog.ErrorLevel,
			"yamux: keepalive failed: i/o timeout", true},
		{"blank line", "\n", false, zerolog.NoLevel, "", false},
		{"empty write", "", false, zerolog.NoLevel, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			level, msg, logged := yamuxRecord(tc.line, tc.shuttingDown)
			if logged != tc.wantLogged {
				t.Fatalf("yamuxRecord(%q, %v) logged = %v, want %v", tc.line, tc.shuttingDown, logged, tc.wantLogged)
			}
			if !logged {
				return
			}
			if level != tc.wantLevel {
				t.Fatalf("yamuxRecord(%q, %v) level = %s, want %s", tc.line, tc.shuttingDown, level, tc.wantLevel)
			}
			if msg != tc.wantMsg {
				t.Fatalf("yamuxRecord(%q, %v) message = %q, want %q", tc.line, tc.shuttingDown, msg, tc.wantMsg)
			}
		})
	}
}

// What yamux writes must come out as records, not as the raw text the
// supervisor would have to pass through unparsed into the log the user reads.
func TestYamuxLoggerEmitsStructuredRecords(t *testing.T) {
	logs := captureLogs(t)

	live, cancelLive := context.WithCancel(context.Background())
	defer cancelLive()
	shuttingDown, cancelShuttingDown := context.WithCancel(context.Background())
	cancelShuttingDown()

	yamuxLogger(shuttingDown).Printf("[ERR] yamux: Failed to read header: failed to get reader: context canceled")
	yamuxLogger(live).Printf("[ERR] yamux: keepalive failed: i/o timeout")

	// logRecords fails on any line that is not JSON, which is the assertion:
	// nothing raw got through.
	records := logRecords(t, logs.String())
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2:\n%s", len(records), logs.String())
	}
	if records[0]["level"] != "debug" {
		t.Fatalf("the mux noticing the connection this process just closed logged at %v, want debug", records[0]["level"])
	}
	if records[1]["level"] != "error" {
		t.Fatalf("a keepalive failure on a live session logged at %v, want error", records[1]["level"])
	}
	for _, rec := range records {
		msg, _ := rec["message"].(string)
		if !strings.HasPrefix(msg, "yamux: ") {
			t.Fatalf("message = %q, want yamux's own prefix kept so the record needs no field to say where it came from", msg)
		}
		if strings.Contains(msg, "[ERR]") {
			t.Fatalf("message = %q still carries yamux's level tag; the level is a field now", msg)
		}
		if _, ok := rec["time"]; !ok {
			t.Fatalf("record %v has no time field", rec)
		}
	}
}

// yamux writes from its read, write and keepalive goroutines at once. The
// writer must lose nothing and must not tear records into each other; run this
// under -race for the other half of the claim.
func TestYamuxLoggerIsSafeForConcurrentUse(t *testing.T) {
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := yamuxLogger(ctx)

	const writers = 32
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			logger.Printf("[WARN] yamux: backlog exceeded, forcing connection reset")
		}()
	}
	wg.Wait()

	records := logRecords(t, logs.String())
	if len(records) != writers {
		t.Fatalf("%d concurrent writes produced %d records, want %d", writers, len(records), writers)
	}
	for _, rec := range records {
		if rec["level"] != "warn" || rec["message"] != "yamux: backlog exceeded, forcing connection reset" {
			t.Fatalf("record came out garbled: %v", rec)
		}
	}
}

// Every key the supervisor writes into the configuration document is a field
// this binary declares.
//
// `loadConfig` sets `DisallowUnknownFields`, so a key the Go side does not know
// is not ignored — it is a startup error, and the runner never attaches. That
// is the right behaviour and it is also a whole class of outage: this document
// crosses two languages with no compiler between them, and it has already
// broken once for exactly this reason. `tenant_uid` here against `tenant_id`
// there meant the desktop app refused every bundle the server actually sent,
// and pairing could not succeed at all — neither repository's tests saw it,
// because this side never parsed a real supervisor payload.
//
// Reading the TypeScript is what makes the duplication a derivation. It is the
// same technique `rules_test.go` uses for the two grace periods, and for the
// same reason: the alternative is two files that agree until somebody edits
// one.
func TestEveryKeyTheSupervisorSendsIsAFieldThisBinaryDeclares(t *testing.T) {
	const envTS = runnerSupervisorDir + "/env.ts"
	src, err := os.ReadFile(envTS)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s does not exist yet: the desktop app's runner supervisor is not in this checkout", envTS)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", envTS, err)
	}

	// The object literal `runnerConfig` stringifies, and only that. Anything
	// outside it — the helpers above, `preflightMessage` below — is not this
	// document.
	const open = "return `${JSON.stringify({"
	const close = "})}\\n`;"
	start := strings.Index(string(src), open)
	if start < 0 {
		t.Fatalf("%s no longer builds the config with %q; this rule is enforcing nothing", envTS, open)
	}
	end := strings.Index(string(src)[start:], close)
	if end < 0 {
		t.Fatalf("%s no longer closes the config with %q; this rule is enforcing nothing", envTS, close)
	}
	body := string(src)[start : start+end]

	// Two shapes: a plain `key: value` line, and a conditional spread that
	// contributes `{ key: value }` only when this Mac has the thing.
	keyPattern := regexp.MustCompile(`(?m)(?:^\s*|\{\s*)([a-z][a-z0-9_]*):`)
	matches := keyPattern.FindAllStringSubmatch(body, -1)
	if len(matches) < 8 {
		t.Fatalf("only %d keys were found in %s; the extraction has stopped working, which would make this test pass forever", len(matches), envTS)
	}

	declared := map[string]bool{}
	fields := reflect.TypeOf(wireConfig{})
	for i := range fields.NumField() {
		tag := fields.Field(i).Tag.Get("json")
		declared[strings.Split(tag, ",")[0]] = true
	}

	seen := map[string]bool{}
	for _, m := range matches {
		key := m[1]
		seen[key] = true
		if !declared[key] {
			t.Errorf("%s sends %q, which wireConfig does not declare — loadConfig sets DisallowUnknownFields, "+
				"so this runner would refuse to start and this Mac would never attach", envTS, key)
		}
	}

	// And the other direction for the fields that are not optional: a required
	// field the supervisor stopped sending is the same outage wearing the other
	// hat.
	for _, required := range []string{
		"tm_base_url", "runner_token", "tenant_id", "member_uid",
		"workspace_dir", "git_bin",
	} {
		if !seen[required] {
			t.Errorf("%s no longer sends %q, which loadConfig requires", envTS, required)
		}
	}
}
