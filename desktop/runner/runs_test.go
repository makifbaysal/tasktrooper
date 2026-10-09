package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The durable-run registry and its buffer, driven with synthetic runs whose
// bodies write frames directly — so what is under test is the numbering, the
// buffer and the routes, on every OS. The tests with real processes and a
// real tunnel are in runs_process_test.go.

func outputLine(id string, n int) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"id":%q,"event":"output","stream":"stdout","data":"line-%d"}`, id, n))
}

func doneLine(id string) []byte {
	return []byte(fmt.Sprintf(`{"v":1,"id":%q,"event":"done","ok":true,"result":{"exit_code":0}}`, id))
}

func startSynthetic(t *testing.T, reg *runRegistry, id string, body func(ctx context.Context, frames io.Writer)) *durableRun {
	t.Helper()
	run, rpcErr := reg.reserve(id, "test.run", context.Background())
	if rpcErr != nil {
		t.Fatalf("reserve %s: %s", id, rpcErr.Message)
	}
	if rpcErr := run.start(body); rpcErr != nil {
		t.Fatalf("start %s: %s", id, rpcErr.Message)
	}
	return run
}

func awaitState(t *testing.T, reg *runRegistry, id, want string, within time.Duration) runStatus {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		st := reg.status(id)
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s is %+v after %s, want %s", id, st, within, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readFrames(t *testing.T, r io.Reader) []map[string]any {
	t.Helper()
	var out []map[string]any
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var f map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &f); err != nil {
			t.Fatalf("a frame is not JSON: %q (%v)", scanner.Text(), err)
		}
		out = append(out, f)
	}
	return out
}

func seqOf(t *testing.T, f map[string]any) int64 {
	t.Helper()
	n, ok := f["seq"].(float64)
	if !ok {
		t.Fatalf("frame %v carries no seq", f)
	}
	return int64(n)
}

func postJSON(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	res, err := srv.Client().Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

func TestWithSeqAddsATopLevelFieldAndKeepsTheBytes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"v":1,"id":"c","event":"started"}`, `{"v":1,"id":"c","event":"started","seq":7}`},
		{`{"v":1,"payload":{"seq":1}}`, `{"v":1,"payload":{"seq":1},"seq":7}`},
		{`{}`, `{"seq":7}`},
		{`{"a":1}  `, `{"a":1,"seq":7}`},
		{`{"a":"cut here`, `{"seq":7,"a":"cut here`},
		{`not json`, `not json`},
	}
	for _, tc := range cases {
		got := string(withSeq([]byte(tc.in), 7))
		if got != tc.want+"\n" {
			t.Errorf("withSeq(%q) = %q, want %q", tc.in, got, tc.want+"\n")
		}
	}
	var decoded struct {
		Seq     int64 `json:"seq"`
		Payload struct {
			Seq int64 `json:"seq"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(withSeq([]byte(`{"seq":99,"payload":{"seq":1}}`), 7), &decoded); err != nil || decoded.Seq != 7 || decoded.Payload.Seq != 1 {
		t.Fatalf("decoded = %+v (%v), want the runner's seq to win at the top and the payload's kept", decoded, err)
	}
}

// The ring holds the newest frames within its limit, on disk, in whole
// segments; the oldest go first and the newest is always kept.
func TestTheRingStaysWithinItsLimitOnDisk(t *testing.T) {
	dir := t.TempDir()
	const limit = 8 * 1024
	ring := newFrameRing(dir, limit)
	for i := int64(1); i <= 2000; i++ {
		ring.append(i, withSeq(outputLine("r", int(i)), i))
	}

	var onDisk int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		onDisk += info.Size()
	}
	if onDisk > limit || onDisk < limit/2 {
		t.Fatalf("%d bytes on disk in %d segments, want at most %d and more than half of it", onDisk, len(entries), limit)
	}

	from, _, _ := ring.read(0, 1<<20)
	if from <= 1 {
		t.Fatalf("the oldest frame kept is %d; nothing was evicted", from)
	}
	cursor := from - 1
	for cursor < 2000 {
		got, chunk, end := ring.read(cursor, 512)
		if got != cursor+1 {
			t.Fatalf("read after %d started at %d, want %d", cursor, got, cursor+1)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(chunk)), "\n") {
			var f struct {
				Seq int64 `json:"seq"`
			}
			if err := json.Unmarshal([]byte(line), &f); err != nil || f.Seq != cursor+1 {
				t.Fatalf("frame %q, want seq %d", line, cursor+1)
			}
			cursor++
		}
		if end != cursor {
			t.Fatalf("read reported last %d, frames end at %d", end, cursor)
		}
	}

	ring.append(2001, withSeq([]byte(`{"big":"`+strings.Repeat("x", 4*limit)+`"}`), 2001))
	if got, _, end := ring.read(2000, 1); got != 2001 || end != 2001 {
		t.Fatalf("a frame over the limit was not kept as the newest: got %d..%d", got, end)
	}

	ring.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the ring's directory is still there after close (%v)", err)
	}
}

func TestStatusIsUnknownRunningThenDone(t *testing.T) {
	st := newState()
	st.runs = newRunRegistry(t.TempDir(), defaultRunBufferBytes, runRetention)
	t.Cleanup(st.runs.close)

	res := request(t, config{}, st, http.MethodPost, "/run.status", `{"id":"nobody"}`)
	var status runStatus
	if err := json.Unmarshal([]byte(res.body), &status); err != nil || res.status != http.StatusOK || status.State != "unknown" || status.LastSeq != 0 {
		t.Fatalf("status of an unknown run = %d %s, want 200 unknown with last_seq 0", res.status, res.body)
	}

	release := make(chan struct{})
	startSynthetic(t, st.runs, "r-1", func(_ context.Context, frames io.Writer) {
		_, _ = frames.Write(outputLine("r-1", 1))
		_, _ = frames.Write(outputLine("r-1", 2))
		<-release
		_, _ = frames.Write(doneLine("r-1"))
	})
	awaitStatus := func(want string, seq int64) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			res := request(t, config{}, st, http.MethodPost, "/run.status", `{"id":"r-1"}`)
			var got runStatus
			_ = json.Unmarshal([]byte(res.body), &got)
			if got.State == want && got.LastSeq == seq {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("status = %s, want %s at seq %d", res.body, want, seq)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	awaitStatus("running", 2)
	close(release)
	awaitStatus("done", 3)

	if res := request(t, config{}, st, http.MethodPost, "/run.attach", `{"id":"nobody","after_seq":0}`); res.status != http.StatusNotFound || res.code() != codeUnknownRun {
		t.Fatalf("attach to an unknown run = %d %s, want 404 %s", res.status, res.body, codeUnknownRun)
	}
	if res := request(t, config{}, st, http.MethodPost, "/run.attach", `{"id":"r-1","after_seq":-1}`); res.status != http.StatusBadRequest {
		t.Fatalf("attach with a negative after_seq = %d, want 400", res.status)
	}
}

// A finished run is replayed whole, by anybody, as often as asked, until its
// retention ends; then it is unknown and its buffer is off the disk.
func TestAFinishedRunIsReplayedThenForgotten(t *testing.T) {
	dir := t.TempDir()
	st := newState()
	st.runs = newRunRegistry(dir, defaultRunBufferBytes, 300*time.Millisecond)
	t.Cleanup(st.runs.close)

	startSynthetic(t, st.runs, "r-gc", func(_ context.Context, frames io.Writer) {
		for i := 1; i <= 5; i++ {
			_, _ = frames.Write(outputLine("r-gc", i))
		}
		_, _ = frames.Write(doneLine("r-gc"))
		_, _ = frames.Write(outputLine("r-gc", 99))
	})
	awaitState(t, st.runs, "r-gc", "done", 5*time.Second)

	for _, after := range []int64{0, 3} {
		res := request(t, config{}, st, http.MethodPost, "/run.attach", fmt.Sprintf(`{"id":"r-gc","after_seq":%d}`, after))
		if res.status != http.StatusOK {
			t.Fatalf("attach after %d = %d %s", after, res.status, res.body)
		}
		got := readFrames(t, strings.NewReader(res.body))
		if int64(len(got)) != 6-after {
			t.Fatalf("attach after %d replayed %d frames, want %d: %s", after, len(got), 6-after, res.body)
		}
		for i, f := range got {
			if seqOf(t, f) != after+int64(i)+1 {
				t.Fatalf("attach after %d: frame %d has seq %v", after, i, f["seq"])
			}
		}
		if got[len(got)-1]["event"] != "done" {
			t.Fatalf("the replay does not end with the done: %s", res.body)
		}
	}
	if subdirs, _ := os.ReadDir(dir); len(subdirs) != 1 {
		t.Fatalf("%d buffers on disk, want the one run's", len(subdirs))
	}

	awaitState(t, st.runs, "r-gc", "unknown", 5*time.Second)
	if subdirs, _ := os.ReadDir(dir); len(subdirs) != 0 {
		t.Fatalf("the buffer is still on disk after its retention: %v", subdirs)
	}
	if res := request(t, config{}, st, http.MethodPost, "/run.attach", `{"id":"r-gc"}`); res.status != http.StatusNotFound {
		t.Fatalf("attach after the retention = %d, want 404", res.status)
	}
}

// A caller that fell further behind than the buffer holds is told what it
// missed, then gets everything after it, in order.
func TestAnAttachBehindTheBufferIsToldWhatItMissed(t *testing.T) {
	st := newState()
	st.runs = newRunRegistry(t.TempDir(), minRunBufferBytes, runRetention)
	t.Cleanup(st.runs.close)

	startSynthetic(t, st.runs, "r-gap", func(_ context.Context, frames io.Writer) {
		for i := 1; i <= 3000; i++ {
			_, _ = frames.Write(outputLine("r-gap", i))
		}
		_, _ = frames.Write(doneLine("r-gap"))
	})
	awaitState(t, st.runs, "r-gap", "done", 5*time.Second)

	res := request(t, config{}, st, http.MethodPost, "/run.attach", `{"id":"r-gap","after_seq":0}`)
	got := readFrames(t, strings.NewReader(res.body))
	if got[0]["event"] != "gap" || got[0]["from_seq"] != float64(1) {
		t.Fatalf("first line = %v, want a gap from seq 1", got[0])
	}
	if _, has := got[0]["seq"]; has {
		t.Fatalf("the gap notice carries a seq of its own: %v", got[0])
	}
	next := int64(got[0]["to_seq"].(float64)) + 1
	for _, f := range got[1:] {
		if seqOf(t, f) != next {
			t.Fatalf("frame seq %v, want %d", f["seq"], next)
		}
		next++
	}
	if next != 3002 {
		t.Fatalf("the replay ended at seq %d, want 3001", next-1)
	}
}

func TestOneLiveIDAtATimeAndAFinishedOneIsReplaced(t *testing.T) {
	reg := newRunRegistry(t.TempDir(), defaultRunBufferBytes, runRetention)
	t.Cleanup(reg.close)

	release := make(chan struct{})
	startSynthetic(t, reg, "r-dup", func(_ context.Context, frames io.Writer) {
		<-release
		_, _ = frames.Write(doneLine("r-dup"))
	})
	if _, rpcErr := reg.reserve("r-dup", "test.run", context.Background()); rpcErr == nil || rpcErr.Code != codeBadRequest {
		t.Fatalf("a second run under a live id was %v, want bad_request", rpcErr)
	}
	close(release)
	awaitState(t, reg, "r-dup", "done", 5*time.Second)

	startSynthetic(t, reg, "r-dup", func(_ context.Context, frames io.Writer) {
		_, _ = frames.Write(outputLine("r-dup", 1))
		_, _ = frames.Write(doneLine("r-dup"))
	})
	if st := awaitState(t, reg, "r-dup", "done", 5*time.Second); st.LastSeq != 2 {
		t.Fatalf("the replacing run is %+v, want its own two frames", st)
	}
}

// The registry keeps a bounded number of finished runs, so a stream of short
// ones cannot hold the disk for the whole retention.
func TestOnlyTheNewestFinishedRunsAreKept(t *testing.T) {
	reg := newRunRegistry(t.TempDir(), defaultRunBufferBytes, runRetention)
	t.Cleanup(reg.close)
	for i := 0; i < maxFinishedRuns+3; i++ {
		id := fmt.Sprintf("r-%d", i)
		startSynthetic(t, reg, id, func(_ context.Context, frames io.Writer) { _, _ = frames.Write(doneLine(id)) })
		awaitState(t, reg, id, "done", 5*time.Second)
	}
	for i := 0; i < 3; i++ {
		if st := reg.status(fmt.Sprintf("r-%d", i)); st.State != "unknown" {
			t.Fatalf("r-%d is %s, want it dropped for newer runs", i, st.State)
		}
	}
	if st := reg.status(fmt.Sprintf("r-%d", maxFinishedRuns+2)); st.State != "done" {
		t.Fatalf("the newest run is %s, want done", st.State)
	}
}

// Shutdown is the third way a run ends: every live one is cancelled, still
// gets its done, and the runner refuses new ones.
func TestShutdownCancelsLiveRunsAndRefusesNewOnes(t *testing.T) {
	reg := newRunRegistry(t.TempDir(), defaultRunBufferBytes, runRetention)
	run := startSynthetic(t, reg, "r-live", func(ctx context.Context, frames io.Writer) {
		_, _ = frames.Write(outputLine("r-live", 1))
		<-ctx.Done()
	})
	reg.shutdown()
	if !reg.wait(5 * time.Second) {
		t.Fatal("a cancelled run did not return")
	}
	b := run.read(1, runReadBatch)
	var done doneEvent
	if err := json.Unmarshal(b.frames, &done); err != nil || done.Event != "done" || done.Error == nil || done.Error.Code != codeCancelled {
		t.Fatalf("the run ended with %q (%v), want a done carrying %s", b.frames, err, codeCancelled)
	}
	if _, rpcErr := reg.reserve("r-new", "test.run", context.Background()); rpcErr == nil || rpcErr.Code != codeCancelled {
		t.Fatalf("a run reserved during shutdown was %v, want cancelled", rpcErr)
	}
	reg.close()
}

// POST /cancel finds a durable run the way it found a session-bound one.
func TestCancelFindsADurableRun(t *testing.T) {
	st := newState()
	t.Cleanup(st.runs.close)
	startSynthetic(t, st.runs, "r-c", func(ctx context.Context, frames io.Writer) {
		<-ctx.Done()
	})
	res := request(t, config{}, st, http.MethodPost, "/cancel", `{"id":"r-c"}`)
	if !strings.Contains(res.body, `"cancelled":true`) {
		t.Fatalf("cancel = %s, want cancelled:true", res.body)
	}
	awaitState(t, st.runs, "r-c", "done", 5*time.Second)
	if res := request(t, config{}, st, http.MethodPost, "/cancel", `{"id":"r-c"}`); !strings.Contains(res.body, `"cancelled":false`) {
		t.Fatalf("a second cancel = %s, want cancelled:false", res.body)
	}
}

func TestLoadConfigRunBufferFields(t *testing.T) {
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{"runner_data_dir": "/Users/x/Library/TaskTrooper/runner", "run_buffer_max_bytes": 1 << 20})))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.runnerDataDir != "/Users/x/Library/TaskTrooper/runner" || cfg.runBufferBytes != 1<<20 {
		t.Fatalf("runnerDataDir=%q runBufferBytes=%d", cfg.runnerDataDir, cfg.runBufferBytes)
	}
	if got := runsDir(cfg); got != filepath.Join("/Users/x/Library/TaskTrooper/runner", runsDirName) {
		t.Fatalf("runsDir = %q", got)
	}
	cfg, err = loadConfig([]byte(validConfig))
	if err != nil || cfg.runBufferBytes != defaultRunBufferBytes {
		t.Fatalf("the default cap = %d (%v), want %d", cfg.runBufferBytes, err, defaultRunBufferBytes)
	}
	for _, bad := range []map[string]any{
		{"runner_data_dir": "relative/dir"},
		{"run_buffer_max_bytes": 1024},
		{"run_buffer_max_bytes": int64(maxRunBufferBytes) + 1},
	} {
		if _, err := loadConfig([]byte(configWith(t, bad))); err == nil {
			t.Fatalf("loadConfig accepted %v", bad)
		}
	}
}

func TestLeftoverBuffersAreSweptAtStartup(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, strings.Repeat("ab", 16))
	other := filepath.Join(dir, "not-ours")
	for _, d := range []string{stale, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	reg := newRunRegistry(dir, defaultRunBufferBytes, runRetention)
	t.Cleanup(reg.close)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("an earlier runner's buffer survived startup (%v)", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("a directory that is not a run buffer was removed: %v", err)
	}
}
