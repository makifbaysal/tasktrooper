//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Cancellation, tested against real processes.
//
// This is the one behaviour in this program that cannot be checked by reading
// it. "The CLI process must actually die" is a statement about a process tree,
// a signal disposition and a process group — not about a function returning —
// and this project has already shipped the bug once: a stopped task that left
// a live `claude` session behind, holding a checkout open and burning a plan's
// quota, because only the parent had been signalled.
//
// So the fake `claude` here is a shell script that behaves like the worst real
// one: it IGNORES SIGTERM, and it spawns a child of its own. Nothing but
// SIGKILL to the whole process group takes it down, which is exactly what the
// implementation has to do and exactly what a signal aimed at the parent alone
// would fail to do. The assertions are made with `kill(pid, 0)` against the
// GRANDCHILD, because the grandchild is what the old bug left running.

// stubbornClaude writes a fake CLI that ignores SIGTERM, starts a background
// child, announces both, and then blocks forever.
//
// `trap ” TERM` sets SIG_IGN, and an ignored disposition is inherited across
// fork and exec — so the `sleep` is deaf to SIGTERM too. That is deliberate:
// it makes the test fail unless the implementation escalates to SIGKILL and
// aims it at the group rather than at the process.
func stubbornClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"trap '' TERM\n" +
		"sleep 300 &\n" +
		"echo grandchild:$!\n" +
		"echo ready\n" +
		"wait\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}
	return path
}

// alive reports whether a pid still names a live process. Signal 0 performs
// the existence and permission checks and delivers nothing, which is the
// portable way to ask.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func awaitGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d was still running %s after the session was cancelled — a stopped task has left a live process behind", pid, within)
}

// testCall builds a call whose output goes to a buffer, for the tests that do
// not need a stream.
func testCall(t *testing.T, ctx context.Context, claudeBin, workspace string, w *syncBuffer) *call {
	t.Helper()
	return &call{
		ctx: ctx,
		id:  "test-call",
		cfg: config{claudeBin: claudeBin, gitBin: "/usr/bin/git", workspaceDir: workspace},
		w:   w,
	}
}

func emptyWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "repo"), 0o755); err != nil {
		t.Fatalf("creating the workspace: %v", err)
	}
	return dir
}

// grandchildPID waits for the fake CLI to announce the pid it spawned, reading
// it out of the output frames the runner has emitted so far. That is the same
// path a real caller reads output on, so a pass also proves output is streamed
// rather than buffered until the end.
func grandchildPID(t *testing.T, out *syncBuffer, within time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var frame outputEvent
			if err := json.Unmarshal([]byte(line), &frame); err != nil {
				continue
			}
			if pid, ok := strings.CutPrefix(frame.Data, "grandchild:"); ok {
				n, err := strconv.Atoi(strings.TrimSpace(pid))
				if err == nil {
					return n
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the fake session never announced its child within %s; output so far:\n%s", within, out.String())
	return 0
}

// The core of it: a cancelled call takes the whole process tree with it, even
// when every process in that tree ignores SIGTERM.
// A computer without Claude Code — an API-key-only member — still runs a
// runner; its claude.run is not_ready, never an exec of "".
func TestClaudeRunNotReadyWithoutTheBinary(t *testing.T) {
	cfg := config{gitBin: "/usr/bin/git", workspaceDir: emptyWorkspace(t)}
	res := request(t, cfg, newState(), http.MethodPost, "/claude.run", `{"workspace":"repo","prompt":"go"}`)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.status)
	}
	if res.code() != codeNotReady {
		t.Fatalf("code = %q, want %s", res.code(), codeNotReady)
	}
}

func TestCancellingASessionKillsTheWholeProcessTree(t *testing.T) {
	workspace := emptyWorkspace(t)
	claudeBin := stubbornClaude(t)
	out := &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testCall(t, ctx, claudeBin, workspace, out)

	finished := make(chan struct{})
	var callErr *rpcError
	go func() {
		defer close(finished)
		_, callErr = spawnClaude(ctx, c, filepath.Join(workspace, "repo"), nil, "do the task", nil)
	}()

	child := grandchildPID(t, out, 10*time.Second)
	if !alive(child) {
		t.Fatalf("the fake session's child (%d) was not running before the cancel", child)
	}

	cancel()

	select {
	case <-finished:
	case <-time.After(claudeGrace + claudeReapTimeout + 5*time.Second):
		t.Fatal("spawnClaude never returned after its context was cancelled")
	}

	// The assertion that matters. The parent ignoring SIGTERM is not enough to
	// save it, and neither is being a child of a process that is already gone.
	awaitGone(t, child, 5*time.Second)

	if callErr != nil && callErr.Code != codeCancelled {
		t.Fatalf("a cancelled session reported %s: %s", callErr.Code, callErr.Message)
	}
}

// ---------------------------------------------------------------------------
// the same thing over the wire, which is how it will actually happen
// ---------------------------------------------------------------------------

// runHarness is a real HTTP server running the real handler, plus a client.
//
// A real server rather than a hand-driven handler, because everything under
// test here lives BETWEEN the two: that the response is flushed line by line
// instead of buffered, that a second request can cancel a run in flight, and
// that a client closing the body reaches the handler as a cancelled context.
type runHarness struct {
	srv    *httptest.Server
	client *http.Client
}

func newRunHarness(t *testing.T, claudeBin, workspace string) *runHarness {
	t.Helper()
	cfg := config{
		claudeBin:         claudeBin,
		gitBin:            "/usr/bin/git",
		workspaceDir:      workspace,
		embeddingsBaseURL: "http://127.0.0.1:1234/v1",
		embeddingModel:    "nomic-embed-text-v1.5",
	}
	srv := httptest.NewServer(newRunnerServer(cfg, newState()).handler())
	t.Cleanup(srv.Close)
	return &runHarness{srv: srv, client: srv.Client()}
}

// start posts a claude.run and returns the live response, unread.
func (h *runHarness) start(t *testing.T, body string) *http.Response {
	t.Helper()
	res, err := h.client.Post(h.srv.URL+"/claude.run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /claude.run: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /claude.run answered %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", got)
	}
	return res
}

func (h *runHarness) cancel(t *testing.T, id string) {
	t.Helper()
	res, err := h.client.Post(h.srv.URL+"/cancel", "application/json", strings.NewReader(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("POST /cancel: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST /cancel answered %d", res.StatusCode)
	}
	var parsed cancelResponse
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("decoding the cancel response: %v", err)
	}
	if !parsed.Cancelled {
		t.Fatalf("POST /cancel did not find call %q", id)
	}
}

// frames reads the NDJSON body line by line onto a channel, so a test can act
// on the first line without waiting for the last one — which is the whole
// property this response is supposed to have.
func frames(res *http.Response) <-chan map[string]any {
	out := make(chan map[string]any, 64)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var parsed map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &parsed); err == nil {
				out <- parsed
			}
		}
	}()
	return out
}

func nextFrame(t *testing.T, ch <-chan map[string]any, within time.Duration) map[string]any {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("the response ended before the frame arrived")
		}
		return f
	case <-time.After(within):
		t.Fatalf("no frame arrived within %s", within)
	}
	return nil
}

// awaitGrandchild reads frames until the fake CLI announces the pid it spawned.
// Reaching it at all proves the response is flushed as it happens: the process
// has not exited and will not, so a buffered response would deliver nothing.
func awaitGrandchild(t *testing.T, ch <-chan map[string]any, within time.Duration) (pid int, seen []map[string]any) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("the response ended before the session started; frames so far: %v", seen)
			}
			seen = append(seen, f)
			data, _ := f["data"].(string)
			if raw, found := strings.CutPrefix(data, "grandchild:"); found {
				n, err := strconv.Atoi(strings.TrimSpace(raw))
				if err != nil {
					t.Fatalf("could not read the child pid from %v", f)
				}
				return n, seen
			}
		case <-deadline:
			t.Fatalf("the session never announced its child within %s; frames so far: %v", within, seen)
		}
	}
}

// The first trigger: POST /cancel naming the call.
//
// Driven over real HTTP because the parts that could break are the ones between
// the two requests — that the run registered its id before it wrote a byte,
// that a second request can find it, and that exactly one `done` still arrives
// on the first response afterwards.
func TestCancelEndpointStopsTheSession(t *testing.T) {
	workspace := emptyWorkspace(t)
	h := newRunHarness(t, stubbornClaude(t), workspace)

	res := h.start(t, `{"id":"c-1","workspace":"repo","prompt":"do the task"}`)
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)

	// The first line carries the call id, always — including one this side
	// generated, which is the only way a caller that did not choose one could
	// ever cancel.
	first := nextFrame(t, ch, 15*time.Second)
	if first["event"] != "started" || first["id"] != "c-1" {
		t.Fatalf("first frame = %v, want a started event carrying the call id", first)
	}

	child, _ := awaitGrandchild(t, ch, 15*time.Second)
	if !alive(child) {
		t.Fatalf("the fake session's child (%d) was not running before the cancel", child)
	}

	h.cancel(t, "c-1")

	done, count := drainToDone(t, ch, claudeGrace+claudeReapTimeout+10*time.Second)
	if done["ok"] != false {
		t.Fatalf("a cancelled call reported ok=%v", done["ok"])
	}
	if e, _ := done["error"].(map[string]any); e == nil || e["code"] != codeCancelled {
		t.Fatalf("a cancelled call reported %v, want code %s", done["error"], codeCancelled)
	}
	if count != 1 {
		t.Fatalf("the response carried %d done frames, want exactly one", count)
	}

	// And the process is genuinely gone, which is the whole point of the cancel
	// having been sent.
	awaitGone(t, child, 5*time.Second)
}

// The second trigger, and the one the protocol change added: the client closes
// the response body.
//
// With HTTP there is no half-close to protect any more — a request either has a
// body or it does not, and it is complete before the response starts — so the
// old rule that EOF must never cancel has nothing left to protect. What is left
// is the failure it would cause: a tunnel that drops mid-run leaving a `claude`
// alive on somebody's Mac, chewing through a repository nobody is watching.
func TestClosingTheResponseBodyStopsTheSession(t *testing.T) {
	workspace := emptyWorkspace(t)
	h := newRunHarness(t, stubbornClaude(t), workspace)

	res := h.start(t, `{"workspace":"repo","prompt":"do the task"}`)
	ch := frames(res)

	first := nextFrame(t, ch, 15*time.Second)
	if first["event"] != "started" {
		t.Fatalf("first frame = %v, want a started event", first)
	}
	// The id was generated here, not chosen by the caller — and the first line
	// is how the caller learns it.
	if id, _ := first["id"].(string); id == "" {
		t.Fatalf("the started frame carried no call id: %v", first)
	}

	child, _ := awaitGrandchild(t, ch, 15*time.Second)
	if !alive(child) {
		t.Fatalf("the fake session's child (%d) was not running before the close", child)
	}

	// No cancel, no request. Just a caller that stopped listening — which is
	// what a dropped tunnel looks like from this side.
	if err := res.Body.Close(); err != nil {
		t.Fatalf("closing the response body: %v", err)
	}

	awaitGone(t, child, claudeGrace+claudeReapTimeout+10*time.Second)
}

// The streaming property, stated on its own: output arrives while the session
// is still running, and the response ends with exactly one `done`.
func TestTheResponseIsStreamedAndEndsWithExactlyOneDone(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")
	// One line, then a long silence, then an exit. The silence is the whole
	// test: if the response were buffered, NOTHING would arrive until the
	// process ended, so a first line that shows up while the process is
	// demonstrably still running is proof that each write is flushed.
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo line-1\n" +
		"sleep 6\n" +
		"exit 0\n"
	if err := os.WriteFile(claudeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	h := newRunHarness(t, claudeBin, workspace)

	// Every deadline in this test is SHORTER than the six seconds the fake CLI
	// spends asleep, and that is the whole design of it: the process is still
	// running when each assertion is made, so a response that was buffered
	// until the handler returned would deliver nothing in time and every one of
	// them would fail. A generous deadline anywhere here would quietly absorb
	// exactly the bug this is written to catch.
	requested := time.Now()
	res := h.start(t, `{"id":"c-stream","workspace":"repo","prompt":"go"}`)
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)

	first := nextFrame(t, ch, 3*time.Second)
	if first["event"] != "started" || first["id"] != "c-stream" {
		t.Fatalf("first frame = %v, want a started event carrying the call id", first)
	}

	f := nextFrame(t, ch, 3*time.Second)
	if f["event"] != "output" || f["data"] != "line-1" {
		t.Fatalf("second frame = %v, want the first output line", f)
	}
	if f["stream"] != "stdout" {
		t.Fatalf("output frame = %v, want it to name the stream it came from", f)
	}
	// Belt to those braces: both frames were in hand while the child still had
	// seconds of sleeping left to do.
	if elapsed := time.Since(requested); elapsed >= 5*time.Second {
		t.Fatalf("the first two frames took %s, by which time the session had ended; the response is being buffered", elapsed)
	}

	done, count := drainToDone(t, ch, 30*time.Second)
	if count != 1 {
		t.Fatalf("the response carried %d done frames, want exactly one", count)
	}
	if done["ok"] != true {
		t.Fatalf("done = %v, want a success", done)
	}
	if done["id"] != "c-stream" {
		t.Fatalf("done frame carried id %v, want the caller's", done["id"])
	}
	result, _ := done["result"].(map[string]any)
	if result == nil || result["exit_code"] != float64(0) {
		t.Fatalf("result = %v, want exit_code 0", done["result"])
	}
}

// drainToDone reads to the end of the response and returns the terminal frame
// and how many of them there were. Counting them is the point: a method that
// answered twice would produce a stream no caller could parse, and the count is
// the only way to notice.
func drainToDone(t *testing.T, ch <-chan map[string]any, within time.Duration) (map[string]any, int) {
	t.Helper()
	deadline := time.After(within)
	var done map[string]any
	count := 0
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				if count == 0 {
					t.Fatal("the response ended without a terminal frame")
				}
				return done, count
			}
			if f["event"] == "done" {
				done = f
				count++
			} else if count > 0 {
				t.Fatalf("a %v frame arrived AFTER the terminal one", f["event"])
			}
		case <-deadline:
			// Reaching here means the RESPONSE never ended, which is a failure
			// whatever was in it.
			//
			// This used to return success when exactly one done had been seen,
			// on the theory that the connection was merely being kept alive.
			// That turned "no frame arrived after the terminal one" into "we
			// stopped looking after the terminal one" — the assertion the
			// caller is making, silently weakened to nothing. The handler's
			// response is chunked and ends when the handler returns, so the
			// body DOES close and the channel DOES close; a deadline here means
			// something is holding the run open, which is exactly the class of
			// bug this suite exists to catch.
			t.Fatalf("the response had not ended within %s (%d terminal frames so far); "+
				"a run that never closes its stream is a run the caller cannot tell from a slow one", within, count)
		}
	}
}

// An over-long line costs its own tail and NOTHING ELSE.
//
// This is the bug that shaped `forward`. A `--output-format stream-json`
// session emits one JSON document per line, with a tool result inside it, so a
// Read of a real source file or a noisy test run produces a line over any limit
// worth setting. `bufio.Scanner` stopped at ErrTooLong and forwarding of that
// stream ended for the rest of the run — and because nothing drained the pipe
// afterwards, the child blocked on its next write, `streams.Wait()` never
// returned, and the call hung forever with a live `claude` in it. Measured, not
// theorised: a 1 MiB line followed by 200 KiB of output never returned in 40
// seconds, and the 20-second reap timeout never fired because the block was
// upstream of it.
//
// Three things are asserted, and the third is the one that was actually fatal:
//
//  1. the over-long line arrives, truncated, and says so
//  2. the lines AFTER it arrive
//  3. the call ends on its own
func TestAnOverLongLineIsTruncatedAndTheRestOfTheStreamStillArrives(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")

	// Deliberately more than the pipe buffer (64 KiB) after the huge line: a
	// small tail fits in the pipe and would let a broken implementation finish
	// anyway, which is exactly how this survived the happy-path test.
	const over = outputLineLimit + 4096
	// `head -c … /dev/zero | tr` rather than awk: awk's gsub over a multi-megabyte
	// string is quadratic and takes minutes, which would make this test look
	// exactly like the hang it is written to detect.
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo before-the-big-line\n" +
		"head -c " + strconv.Itoa(over) + " /dev/zero | tr '\\0' 'x'\n" +
		"echo\n" +
		"i=0; while [ $i -lt 200 ]; do head -c 1024 /dev/zero | tr '\\0' 'y'; echo; i=$((i+1)); done\n" +
		"echo the-last-line\n" +
		"exit 0\n"
	if err := os.WriteFile(claudeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testCall(t, ctx, claudeBin, workspace, out)

	done := make(chan *rpcError, 1)
	go func() {
		_, callErr := spawnClaude(ctx, c, filepath.Join(workspace, "repo"), nil, "go", nil)
		done <- callErr
	}()

	// Shorter than claudeReapTimeout on purpose: the old failure was a hang
	// that the reap timeout could not even reach, so a deadline generous enough
	// to include it would be a deadline that absorbs the bug.
	select {
	case callErr := <-done:
		if callErr != nil {
			t.Fatalf("the run failed: %s", callErr.Message)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("spawnClaude never returned: a long line has wedged the child on a pipe nobody is draining")
	}

	text := out.String()
	if !strings.Contains(text, "before-the-big-line") {
		t.Fatal("the line before the long one never arrived")
	}
	// The one that matters: everything after the over-long line.
	if !strings.Contains(text, "the-last-line") {
		t.Fatal("the transcript stops at the over-long line; the rest of the run was silently lost")
	}
	// And the caller is TOLD, in the transcript rather than only in this
	// program's log — a line that just stops is indistinguishable from a
	// session that went quiet.
	if !strings.Contains(text, "was cut here") {
		t.Fatalf("the truncation is not visible to the caller:\n%s", firstBytes(text, 2000))
	}
	if !strings.Contains(text, "4096 bytes over") {
		t.Fatalf("the truncation notice does not say how much was cut:\n%s", firstBytes(text, 2000))
	}
}

func firstBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// A run cannot start on a session that is draining, and a cancelled call never
// spawns `claude`.
//
// Two bugs, one shape. The in-flight accounting was a sync.WaitGroup, so `Add`
// racing a blocked `Wait` panicked with "sync: WaitGroup misuse" and took the
// whole daemon down — which orphans exactly the process groups the ordered
// teardown exists to reap. And the wait for a session slot was a `select` on
// the semaphore and the request context, which picks at RANDOM when both are
// ready, so a request arriving during teardown spawned a session about half
// the time.
//
// Asserted through the real handler, because both are properties of the order
// two goroutines reach it in.
func TestADrainingSessionStartsNoNewRuns(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")
	// A marker file, so "did a session start" is a fact on disk rather than an
	// inference from a response the test may not get to read.
	marker := filepath.Join(dir, "it-ran")
	script := "#!/bin/sh\ncat >/dev/null\ntouch " + marker + "\nexit 0\n"
	if err := os.WriteFile(claudeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	cfg := config{claudeBin: claudeBin, gitBin: "/usr/bin/git", workspaceDir: workspace}
	runner := newRunnerServer(cfg, newState())
	runner.mcpRoot = t.TempDir()

	// Drain with nothing in flight: returns at once, and latches.
	runner.drain()

	srv := httptest.NewServer(runner.handler())
	defer srv.Close()

	res, err := srv.Client().Post(srv.URL+"/claude.run", "application/json",
		strings.NewReader(`{"id":"c-late","workspace":"repo","prompt":"go"}`))
	if err != nil {
		t.Fatalf("POST /claude.run: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != statusCancelled {
		t.Fatalf("status = %d, want %d — a draining session refuses new runs", res.StatusCode, statusCancelled)
	}
	var body errorBody
	if decErr := json.NewDecoder(res.Body).Decode(&body); decErr != nil {
		t.Fatalf("decoding the refusal: %v", decErr)
	}
	if body.Error.Code != codeCancelled {
		t.Fatalf("code = %q, want %q", body.Error.Code, codeCancelled)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("a session was spawned on a tunnel that had already drained")
	}
}

// drain returns only after the runs it is waiting for have returned — which,
// because claude.run reaps its process group before returning, is the moment
// the Mac is genuinely running nothing. That ordering is what "tunnel detached"
// claims, so it is asserted rather than assumed.
func TestDrainWaitsForTheRunsInFlight(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\ncat >/dev/null\necho ready\nsleep 2\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	cfg := config{claudeBin: claudeBin, gitBin: "/usr/bin/git", workspaceDir: workspace}
	runner := newRunnerServer(cfg, newState())
	runner.mcpRoot = t.TempDir()
	srv := httptest.NewServer(runner.handler())
	defer srv.Close()

	res, err := srv.Client().Post(srv.URL+"/claude.run", "application/json",
		strings.NewReader(`{"id":"c-inflight","workspace":"repo","prompt":"go"}`))
	if err != nil {
		t.Fatalf("POST /claude.run: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)
	// Wait until the run is genuinely in flight before draining.
	awaitData(t, ch, "ready", 15*time.Second)

	drained := make(chan time.Duration, 1)
	go func() {
		started := time.Now()
		runner.drain()
		drained <- time.Since(started)
	}()

	select {
	case took := <-drained:
		// The fake sleeps for two seconds after announcing itself, so a drain
		// that returned immediately did not wait for anything.
		if took < 500*time.Millisecond {
			t.Fatalf("drain returned after %s, before the run in flight had finished", took)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("drain never returned")
	}

	if runner.active != 0 {
		t.Fatalf("drain returned with %d runs still counted", runner.active)
	}
}

// A session that ends by itself reports how it ended. The exit code is the
// task's result as far as the control plane is concerned, so losing it would
// make every run look identical.
func TestASessionThatExitsReportsItsCode(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")
	if err := os.WriteFile(claudeBin, []byte("#!/bin/sh\ncat >/dev/null\necho done\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testCall(t, ctx, claudeBin, workspace, out)

	result, callErr := spawnClaude(ctx, c, filepath.Join(workspace, "repo"), nil, "hello", nil)
	if callErr != nil {
		t.Fatalf("spawnClaude: %s", callErr.Message)
	}
	got, ok := result.(claudeRunResult)
	if !ok {
		t.Fatalf("result is %T, want claudeRunResult", result)
	}
	if got.ExitCode != 3 {
		t.Fatalf("exit_code = %d, want 3", got.ExitCode)
	}
	if !strings.Contains(out.String(), "done") {
		t.Fatalf("the session's output never reached the caller:\n%s", out.String())
	}
}

// The prompt is written to stdin and never to argv.
//
// Two reasons, and this asserts the observable half of both: argv on macOS is
// world-readable, so a prompt there is a task's contents in every `ps` on the
// machine; and a prompt beginning with a dash in argv would be parsed as a
// flag. The fake CLI echoes what it read on stdin, so a pass proves the prompt
// arrived — and `claudeArgs` proves it is not in the arguments.
func TestThePromptTravelsOnStdinAndNotInArgv(t *testing.T) {
	workspace := emptyWorkspace(t)
	dir := t.TempDir()
	claudeBin := filepath.Join(dir, "claude")
	// Echoes its arguments first, then everything it was given on stdin.
	script := "#!/bin/sh\necho \"argv:$*\"\nprintf 'stdin:'\ncat\necho\n"
	if err := os.WriteFile(claudeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}

	const prompt = "--dangerously-skip-permissions and rm -rf /"
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testCall(t, ctx, claudeBin, workspace, out)

	args, argErr := claudeArgs(claudeRunParams{Prompt: prompt})
	if argErr != nil {
		t.Fatalf("claudeArgs: %s", argErr.Message)
	}
	for _, arg := range args {
		if strings.Contains(arg, "rm -rf") || strings.Contains(arg, "dangerously") {
			t.Fatalf("the prompt reached argv as %q", arg)
		}
	}

	if _, callErr := spawnClaude(ctx, c, filepath.Join(workspace, "repo"), args, prompt, nil); callErr != nil {
		t.Fatalf("spawnClaude: %s", callErr.Message)
	}
	if !strings.Contains(out.String(), "stdin:") || !strings.Contains(out.String(), "rm -rf /") {
		t.Fatalf("the prompt did not arrive on stdin:\n%s", out.String())
	}
}

// Everything a caller can put in argv goes through a grammar first. Without
// them, `model` and `permission_mode` are a way to append any flag the CLI has
// — including the ones that turn permission checks off.
func TestClaudeArgsRefusesAnythingThatCouldBecomeAFlag(t *testing.T) {
	cases := []struct {
		name   string
		params claudeRunParams
		want   string
	}{
		{"a model that is a flag", claudeRunParams{Model: "--dangerously-skip-permissions"}, "is not a model name"},
		{"a model with a space", claudeRunParams{Model: "opus --debug"}, "is not a model name"},
		{"an invented permission mode", claudeRunParams{PermissionMode: "--allow-everything"}, "is not one the CLI has"},
		{"a session id that is not a uuid", claudeRunParams{SessionID: "../../etc/passwd"}, "must be a uuid"},
		{"a resume id that is not a uuid", claudeRunParams{Resume: "-p"}, "must be a session uuid"},
		{"max_turns out of range", claudeRunParams{MaxTurns: -1}, "outside 1..1000"},
		{
			"both a session id and a resume",
			claudeRunParams{
				SessionID: "11111111-1111-1111-1111-111111111111",
				Resume:    "22222222-2222-2222-2222-222222222222",
			},
			"cannot both be set",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := claudeArgs(tc.params)
			if err == nil {
				t.Fatalf("claudeArgs(%+v) succeeded, want a refusal", tc.params)
			}
			if !strings.Contains(err.Message, tc.want) {
				t.Fatalf("claudeArgs error = %q, want it to mention %q", err.Message, tc.want)
			}
		})
	}

	// And the shape of a good one: --print with a streaming, verbose output
	// format, because anything else buffers the whole run.
	args, err := claudeArgs(claudeRunParams{Model: "opus", PermissionMode: "bypassPermissions"})
	if err != nil {
		t.Fatalf("claudeArgs on a valid request: %s", err.Message)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--print", "--output-format stream-json", "--verbose", "--model opus", "--permission-mode bypassPermissions"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv %q is missing %q", joined, want)
		}
	}
}
