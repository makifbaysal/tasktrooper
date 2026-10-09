package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// Log-capture helpers shared by the tests that run on every OS and the ones
// that drive shell stand-ins (tagged !windows), so they live in neither.

// syncBuffer is a log sink that survives being written from the reconnect
// loop's goroutines while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// captureLogs points the log sink at a buffer for one test. It moves the
// sink's destination rather than the logger itself (see testSink), because
// yamux's goroutines log through the same logger and can still be finishing a
// line after the test that started them is over. A test using this must not
// run in parallel.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	testSink.redirect(buf)
	t.Cleanup(func() { testSink.redirect(io.Discard) })
	return buf
}

// awaitLog blocks until want shows up in the captured log. The gateway having
// a session is not the same thing as the runner having attached one — the
// upgrade completes on the server side first — so anything that depends on the
// runner being attached waits for the runner to say so.
func awaitLog(t *testing.T, logs *syncBuffer, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if strings.Contains(logs.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q never appeared in the log within %s:\n%s", want, within, logs.String())
		}
		time.Sleep(time.Millisecond)
	}
}

// logRecords returns the parsed log lines, skipping anything that is not JSON.
func logRecords(t *testing.T, out string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}
