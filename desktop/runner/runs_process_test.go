//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// countingClaude prints its pid, then n numbered lines a little apart, then
// "finished", and exits 0: a run long enough to lose a stream in the middle
// of, short enough to finish inside a test.
func countingClaude(t *testing.T, n int, pause string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo pid:$$\n" +
		"i=1\n" +
		"while [ $i -le " + strconv.Itoa(n) + " ]; do echo line-$i; sleep " + pause + "; i=$((i+1)); done\n" +
		"echo finished\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}
	return path
}

// readUntil reads frames off a raw NDJSON body until one whose data starts
// with prefix, and returns every frame read.
func readUntil(t *testing.T, reader *bufio.Reader, prefix string, within time.Duration) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(within)
	var seen []map[string]any
	for time.Now().Before(deadline) {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("the stream ended before %q: %v; frames: %v", prefix, err, seen)
		}
		var f map[string]any
		if err := json.Unmarshal(line, &f); err != nil {
			t.Fatalf("a frame is not JSON: %q", line)
		}
		seen = append(seen, f)
		if data, _ := f["data"].(string); strings.HasPrefix(data, prefix) {
			return seen
		}
	}
	t.Fatalf("no %q within %s; frames: %v", prefix, within, seen)
	return nil
}

// assertWholeRun checks a run's frames as one caller saw them across any
// number of streams: seq 1..n with none twice and none missing, started
// first, the lines in the order the CLI wrote them, and one done, last.
func assertWholeRun(t *testing.T, frames []map[string]any, lines int) map[string]any {
	t.Helper()
	if len(frames) == 0 || frames[0]["event"] != "started" {
		t.Fatalf("the run does not begin with started: %v", frames)
	}
	next := 1
	for i, f := range frames {
		if got := seqOf(t, f); got != int64(i+1) {
			t.Fatalf("frame %d has seq %d, want %d — a frame was lost or delivered twice", i, got, i+1)
		}
		if data, _ := f["data"].(string); strings.HasPrefix(data, "line-") {
			if data != "line-"+strconv.Itoa(next) {
				t.Fatalf("frame %d is %q, want line-%d", i, data, next)
			}
			next++
		}
	}
	if next != lines+1 {
		t.Fatalf("saw %d numbered lines, want %d", next-1, lines)
	}
	done := frames[len(frames)-1]
	if done["event"] != "done" {
		t.Fatalf("the last frame is %v, want the done", done)
	}
	for _, f := range frames[:len(frames)-1] {
		if f["event"] == "done" {
			t.Fatalf("a done arrived before the end: %v", f)
		}
	}
	return done
}

// The caller's stream breaks mid-run; it attaches with the last seq it saw and
// gets the rest — everything exactly once, in order — then the whole run again
// from the buffer.
func TestADroppedStreamIsResumedWithEveryFrameOnceInOrder(t *testing.T) {
	const lines = 40
	h := newRunHarness(t, countingClaude(t, lines, "0.05"), emptyWorkspace(t))

	res := h.start(t, `{"id":"c-resume","workspace":"repo","prompt":"go"}`)
	reader := bufio.NewReader(res.Body)
	seen := readUntil(t, reader, "line-5", 15*time.Second)
	child := 0
	for _, f := range seen {
		if pid, ok := strings.CutPrefix(fmt.Sprint(f["data"]), "pid:"); ok {
			child, _ = strconv.Atoi(pid)
		}
	}
	if err := res.Body.Close(); err != nil {
		t.Fatalf("closing the stream: %v", err)
	}
	cursor := seqOf(t, seen[len(seen)-1])

	time.Sleep(500 * time.Millisecond)
	if !alive(child) {
		t.Fatalf("the CLI (%d) died with its stream", child)
	}

	attach, err := h.client.Post(h.srv.URL+"/run.attach", "application/json",
		strings.NewReader(fmt.Sprintf(`{"id":"c-resume","after_seq":%d}`, cursor)))
	if err != nil || attach.StatusCode != http.StatusOK {
		t.Fatalf("attach: %v (status %v)", err, attach)
	}
	rest := readFrames(t, attach.Body)
	_ = attach.Body.Close()

	done := assertWholeRun(t, append(seen, rest...), lines)
	if done["ok"] != true {
		t.Fatalf("done = %v, want the CLI's own success", done)
	}

	replay, err := h.client.Post(h.srv.URL+"/run.attach", "application/json", strings.NewReader(`{"id":"c-resume"}`))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	whole := readFrames(t, replay.Body)
	_ = replay.Body.Close()
	assertWholeRun(t, whole, lines)
	if len(whole) != len(seen)+len(rest) {
		t.Fatalf("the replay has %d frames, the two streams had %d", len(whole), len(seen)+len(rest))
	}
	if st := h.st.runs.status("c-resume"); st.State != "done" || st.LastSeq != int64(len(whole)) {
		t.Fatalf("status = %+v, want done at seq %d", st, len(whole))
	}
}

// The tunnel itself drops — the load balancer's hour — and the runner
// reconnects. The CLI never noticed, and the new session attaches to it.
func TestATunnelDropDoesNotKillTheRun(t *testing.T) {
	const lines = 30
	gw := newFakeGateway(t, nil)
	cfg := testConfig(t, gw.baseURL())
	cfg.claudeBin = countingClaude(t, lines, "0.1")
	st := testState(t)
	t.Cleanup(st.runs.close)
	logs := captureLogs(t)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = run(ctx, cfg, st, fastTimings(10*time.Millisecond, time.Hour))
	}()
	t.Cleanup(func() { cancel(); <-stopped })

	first := gw.awaitSession(t, 10*time.Second)
	res, err := tunnelClient(first).Post("http://runner/claude.run", "application/json",
		strings.NewReader(`{"id":"c-tunnel","workspace":"repo","prompt":"go"}`))
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("claude.run over the tunnel: %v (%v)", err, res)
	}
	seen := readUntil(t, bufio.NewReader(res.Body), "line-3", 15*time.Second)
	child := 0
	for _, f := range seen {
		if pid, ok := strings.CutPrefix(fmt.Sprint(f["data"]), "pid:"); ok {
			child, _ = strconv.Atoi(pid)
		}
	}
	cursor := seqOf(t, seen[len(seen)-1])

	_ = first.Close()
	second := gw.awaitSession(t, 10*time.Second)
	awaitLog(t, logs, "tunnel detached", 5*time.Second)
	if !alive(child) {
		t.Fatalf("the CLI (%d) died with the tunnel", child)
	}

	client := tunnelClient(second)
	if status, body := tunnelCall(t, client, http.MethodPost, "/run.status", `{"id":"c-tunnel"}`); status != http.StatusOK || !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("status on the new session = %d %s, want running", status, body)
	}
	attach, err := client.Post("http://runner/run.attach", "application/json",
		strings.NewReader(fmt.Sprintf(`{"id":"c-tunnel","after_seq":%d}`, cursor)))
	if err != nil || attach.StatusCode != http.StatusOK {
		t.Fatalf("attach on the new session: %v (%v)", err, attach)
	}
	rest := readFrames(t, attach.Body)
	_ = attach.Body.Close()

	done := assertWholeRun(t, append(seen, rest...), lines)
	result, _ := done["result"].(map[string]any)
	if done["ok"] != true || result["exit_code"] != float64(0) {
		t.Fatalf("done = %v, want the CLI's exit 0", done)
	}
	if strings.Contains(logs.String(), testToken) {
		t.Fatal("the runner token reached the log")
	}
}
