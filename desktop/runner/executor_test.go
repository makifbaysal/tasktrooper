package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The executor tests drive a REAL child process speaking the executor's stdin
// and HTTP contract: this test binary, started with no arguments and
// fakeExecutorEnv set (TestMain hands it to fakeExecutor). The supervision,
// the stdin config line, the listening line, the bearer, the restart and the
// stop are all exercised against a process, never a stub.

const (
	fakeExecutorEnv     = "TT_FAKE_EXECUTOR"
	fakeExecutorDumpEnv = "TT_FAKE_EXECUTOR_DUMP"
	fakeProviderKey     = "sk-fake-provider-0123456789abcdef"
)

// fakeExecutor is the stand-in executor. Modes: "ok" serves until stdin
// closes; "exit" exits shortly after it is ready, so the runner restarts it;
// "protocol2" answers its health check with a protocol this runner does not
// speak; "silent" never prints its listening line.
func fakeExecutor(mode string) int {
	stdin := bufio.NewReader(os.Stdin)
	line, err := stdin.ReadBytes('\n')
	if err != nil {
		return 2
	}
	var cfg executorConfig
	if err := json.Unmarshal(line, &cfg); err != nil {
		return 2
	}
	dump := os.Getenv(fakeExecutorDumpEnv)
	record := func(name, text string) {
		if dump == "" {
			return
		}
		f, err := os.OpenFile(filepath.Join(dump, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_, _ = f.WriteString(text + "\n")
		_ = f.Close()
	}
	record("starts", string(line[:len(line)-1]))

	if mode == "silent" {
		_, _ = io.Copy(io.Discard, stdin)
		return 0
	}

	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+cfg.Token {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	protocol := 1
	if mode == "protocol2" {
		protocol = 2
	}
	key := ""
	if len(cfg.Providers) > 0 {
		key = cfg.Providers[0].APIKey
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /exec/health", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "fake", "protocol": protocol})
	})
	mux.HandleFunc("POST /exec/agent.run", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if strings.Contains(body, "refuse") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"bad_request","message":"refused ` + key + `"}}`))
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		send := func(s string) {
			_, _ = w.Write([]byte(s + "\n"))
			flusher.Flush()
		}
		send(`{"type":"started"}`)
		send(`{"type":"event","event":{"kind":"text","text":"root=` + cfg.WorkspaceRoot + `"}}`)
		if strings.Contains(body, "leak") {
			send(`{"type":"event","event":{"kind":"tool_result","text":"` + key + `"}}`)
		}
		if strings.Contains(body, "hang") {
			<-r.Context().Done()
			return
		}
		if strings.Contains(body, "nodone") {
			return
		}
		send(`{"type":"done","ok":true,"result":{"final_text":"ok","usage":{},"tool_usage":[],"duration_ms":1}}`)
	})
	mux.HandleFunc("POST /exec/llm.complete", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		text := "echo:" + req.Model
		if strings.Contains(string(raw), "leak") {
			text += " " + key
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": text, "usage": map[string]int{"input_tokens": 1}})
	})
	mux.HandleFunc("POST /exec/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		var req struct {
			RunID string `json:"run_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		record("cancels", req.RunID)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 3
	}
	go func() { _ = http.Serve(ln, mux) }()
	fmt.Fprintln(os.Stderr, "fake executor starting")
	fmt.Printf("%shttp://%s\n", executorListeningPrefix, ln.Addr())

	if mode == "exit" {
		time.Sleep(300 * time.Millisecond)
		return 4
	}
	_, _ = io.Copy(io.Discard, stdin)
	return 0
}

var fastExecutorTimings = executorTimings{
	listenTimeout: 5 * time.Second,
	readyWait:     10 * time.Second,
	stopGrace:     3 * time.Second,
	minBackoff:    100 * time.Millisecond,
	maxBackoff:    300 * time.Millisecond,
	stableAfter:   time.Minute,
}

type executorHarness struct {
	cfg  config
	st   *state
	sup  *executorSupervisor
	dump string
}

func startFakeExecutor(t *testing.T, mode string, timings executorTimings) *executorHarness {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dump := t.TempDir()
	t.Setenv(fakeExecutorEnv, mode)
	t.Setenv(fakeExecutorDumpEnv, dump)

	cfg := config{
		workspaceDir:    t.TempDir(),
		executorBin:     bin,
		executorDataDir: filepath.Join(t.TempDir(), "executor"),
		providers: []providerConfig{
			{ID: "openai-main", Type: "openai", APIKey: fakeProviderKey, Models: []string{"gpt-4.1"}},
			{ID: "local", Type: "openai", BaseURL: "http://127.0.0.1:1234/v1"},
		},
	}
	st := newState()
	st.executor = newExecutorSupervisor(cfg, func() string { return "" }, timings)
	st.executor.start()
	t.Cleanup(st.executor.close)
	return &executorHarness{cfg: cfg, st: st, sup: st.executor, dump: dump}
}

func (h *executorHarness) lines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dump, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (h *executorHarness) awaitReady(t *testing.T) *executorProcess {
	t.Helper()
	p, err := h.sup.ready(context.Background())
	if err != nil {
		t.Fatalf("the executor never became ready: %s", err.Message)
	}
	return p
}

func TestTheExecutorIsHandedTheWorkspaceRootTheProvidersAndAFreshBearer(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	starts := h.lines(t, "starts")
	if len(starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(starts))
	}
	var got executorConfig
	if err := json.Unmarshal([]byte(starts[0]), &got); err != nil {
		t.Fatalf("the config line is not JSON: %v", err)
	}
	if got.WorkspaceRoot != h.cfg.workspaceDir {
		t.Errorf("workspace_root = %q, want the runner's own root %q", got.WorkspaceRoot, h.cfg.workspaceDir)
	}
	if got.DataDir != h.cfg.executorDataDir {
		t.Errorf("data_dir = %q, want %q", got.DataDir, h.cfg.executorDataDir)
	}
	if got.Listen != "127.0.0.1:0" {
		t.Errorf("listen = %q, want loopback with an OS-picked port", got.Listen)
	}
	if len(got.Token) != 2*executorTokenBytes {
		t.Errorf("token has %d characters, want %d", len(got.Token), 2*executorTokenBytes)
	}
	if len(got.Providers) != 2 || got.Providers[0].APIKey != fakeProviderKey || got.Providers[1].BaseURL == "" {
		t.Errorf("providers = %+v, want both, keys included", got.Providers)
	}
	if info, err := os.Stat(h.cfg.executorDataDir); err != nil || !info.IsDir() {
		t.Errorf("the data directory was not created: %v", err)
	}
}

func TestAgentRunIsForwardedAndStreamedUnchanged(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	res := request(t, h.cfg, h.st, http.MethodPost, "/agent.run",
		`{"run_id":"r-1","kind":"board","agent":{"name":"dev"},"prompt":"go","workspace":"repos/a/task-1"}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	want := `{"type":"started"}` + "\n" +
		`{"type":"event","event":{"kind":"text","text":"root=` + h.cfg.workspaceDir + `"}}` + "\n" +
		`{"type":"done","ok":true,"result":{"final_text":"ok","usage":{},"tool_usage":[],"duration_ms":1}}` + "\n"
	if res.body != want {
		t.Fatalf("body =\n%s\nwant\n%s", res.body, want)
	}
}

func TestForwardedOutputNeverCarriesAProviderKey(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	logs := captureLogs(t)

	run := request(t, h.cfg, h.st, http.MethodPost, "/agent.run", `{"run_id":"r-2","prompt":"leak"}`)
	complete := request(t, h.cfg, h.st, http.MethodPost, "/llm.complete", `{"provider_id":"openai-main","model":"gpt-4.1","messages":[],"system":"leak"}`)
	refused := request(t, h.cfg, h.st, http.MethodPost, "/agent.run", `{"run_id":"r-3","prompt":"refuse"}`)

	for name, body := range map[string]string{"agent.run": run.body, "llm.complete": complete.body, "a refusal": refused.body, "the log": logs.String()} {
		if strings.Contains(body, fakeProviderKey) {
			t.Errorf("%s carried the provider key: %s", name, body)
		}
	}
	if !strings.Contains(run.body, "[redacted PROVIDER_API_KEY:openai-main]") {
		t.Errorf("agent.run body = %s, want the redaction marker", run.body)
	}
	if refused.status != http.StatusBadRequest {
		t.Errorf("the executor's own refusal came back as %d, want its 400 verbatim", refused.status)
	}
}

func TestLLMCompleteIsForwarded(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	res := request(t, h.cfg, h.st, http.MethodPost, "/llm.complete",
		`{"provider_id":"openai-main","model":"gpt-4.1","system":"","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.body), &got); err != nil || got.Text != "echo:gpt-4.1" {
		t.Fatalf("body = %s (%v), want the executor's own answer", res.body, err)
	}
}

func TestCancelStopsAForwardedRunAndTellsTheExecutorByName(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	srv := httptest.NewServer(newRunnerServer(h.cfg, h.st).handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/agent.run", "application/json", strings.NewReader(`{"run_id":"r-hang","prompt":"hang"}`))
	if err != nil {
		t.Fatalf("agent.run: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	reader := bufio.NewReader(res.Body)
	for range 2 {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("reading the first lines: %v", err)
		}
	}

	cancel, err := http.Post(srv.URL+"/cancel", "application/json", strings.NewReader(`{"id":"r-hang"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	raw, _ := io.ReadAll(cancel.Body)
	_ = cancel.Body.Close()
	if !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("cancel answered %s, want cancelled:true", raw)
	}

	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), `"type":"done"`) || !strings.Contains(string(rest), codeCancelled) {
		t.Fatalf("the stream ended with %q, want one done carrying %s", rest, codeCancelled)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.lines(t, "cancels")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the executor was never told the run was cancelled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.lines(t, "cancels"); got[0] != "r-hang" {
		t.Fatalf("cancels = %v, want r-hang", got)
	}
}

func TestAStreamThatEndsWithoutDoneGetsOne(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	res := request(t, h.cfg, h.st, http.MethodPost, "/agent.run", `{"run_id":"r-4","prompt":"nodone"}`)
	lines := strings.Split(strings.TrimSpace(res.body), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, `"type":"done"`) || !strings.Contains(last, codeUpstream) {
		t.Fatalf("last line = %s, want a done carrying %s", last, codeUpstream)
	}
}

func TestAnExitedExecutorIsRestarted(t *testing.T) {
	h := startFakeExecutor(t, "exit", fastExecutorTimings)
	deadline := time.Now().Add(10 * time.Second)
	for len(h.lines(t, "starts")) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("starts = %d after 10s, want the executor restarted at least twice", len(h.lines(t, "starts")))
		}
		time.Sleep(50 * time.Millisecond)
	}
	starts := h.lines(t, "starts")
	tokens := map[string]bool{}
	for _, line := range starts {
		var c executorConfig
		_ = json.Unmarshal([]byte(line), &c)
		tokens[c.Token] = true
	}
	if len(tokens) != len(starts) {
		t.Fatalf("%d starts shared %d tokens, want a fresh bearer per start", len(starts), len(tokens))
	}
}

func TestAProtocolMismatchIsNotRetried(t *testing.T) {
	h := startFakeExecutor(t, "protocol2", fastExecutorTimings)
	_, err := h.sup.ready(context.Background())
	if err == nil || err.Code != codeNotReady || !strings.Contains(err.Message, "protocol") {
		t.Fatalf("ready = %+v, want not_ready naming the protocol", err)
	}
	time.Sleep(4 * fastExecutorTimings.maxBackoff)
	if n := len(h.lines(t, "starts")); n != 1 {
		t.Fatalf("starts = %d, want exactly one: a protocol mismatch is not retried", n)
	}
}

func TestAnExecutorThatNeverListensIsReportedAndStopped(t *testing.T) {
	timings := fastExecutorTimings
	timings.listenTimeout = 300 * time.Millisecond
	h := startFakeExecutor(t, "silent", timings)
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.sup.mu.Lock()
		lastErr, cur := h.sup.lastErr, h.sup.cur
		h.sup.mu.Unlock()
		if cur != nil {
			t.Fatal("an executor that never printed its listening line was published")
		}
		if strings.Contains(lastErr, "EXECUTOR_LISTENING") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lastErr = %q after 10s, want it to name the missing line", lastErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCloseStopsTheExecutorThroughItsStdin(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	p := h.awaitReady(t)
	began := time.Now()
	h.sup.close()
	select {
	case <-p.exited:
	default:
		t.Fatal("close returned with the executor still running")
	}
	if took := time.Since(began); took >= fastExecutorTimings.stopGrace {
		t.Fatalf("close took %s, want the stdin EOF to be enough rather than the kill", took)
	}
	if _, err := h.sup.ready(context.Background()); err == nil || err.Code != codeCancelled {
		t.Fatalf("ready after close = %+v, want cancelled", err)
	}
}

func TestExecutorMethodsAreNotReadyWithoutAnExecutor(t *testing.T) {
	cfg := config{workspaceDir: t.TempDir()}
	for _, path := range []string{"/agent.run", "/llm.complete"} {
		res := request(t, cfg, newState(), http.MethodPost, path, `{"run_id":"r-5"}`)
		if res.status != http.StatusConflict || res.code() != codeNotReady {
			t.Errorf("%s = %d %s, want 409 not_ready", path, res.status, res.body)
		}
	}
}

func TestAgentRunRefusesBeforeForwarding(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	for name, body := range map[string]string{
		"no run id":           `{"prompt":"go"}`,
		"a flag for a run id": `{"run_id":"-x","prompt":"go"}`,
		"workspace outside":   `{"run_id":"r-6","workspace":"../../etc"}`,
		"mcp over http":       `{"run_id":"r-7","mcp":{"url":"http://example.com/mcp","token":"t0123456789","server_name":"tasktrooper"}}`,
		"not an object":       `[1]`,
	} {
		t.Run(name, func(t *testing.T) {
			res := request(t, h.cfg, h.st, http.MethodPost, "/agent.run", body)
			if res.status != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400 before the executor sees it", res.status, res.body)
			}
		})
	}
}

func TestLoadConfigExecutorFields(t *testing.T) {
	ok := map[string]any{
		"executor_bin":      "/Applications/TaskTrooper.app/Contents/Resources/bin/executor",
		"executor_data_dir": "/Users/x/Library/Application Support/TaskTrooper/executor",
		"providers": []map[string]any{
			{"id": "openai-main", "type": "openai", "api_key": "sk-1", "models": []string{"gpt-4.1"}},
			{"id": "lmstudio", "type": "openai", "base_url": "http://127.0.0.1:1234/v1"},
			{"id": "proxy", "type": "anthropic", "base_url": "https://llm.example.com", "api_key": "k"},
		},
	}
	cfg, err := loadConfig([]byte(configWith(t, ok)))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.executorBin == "" || len(cfg.providers) != 3 || cfg.providers[0].APIKey != "sk-1" {
		t.Fatalf("cfg = %+v", cfg)
	}

	for name, changes := range map[string]map[string]any{
		"relative executor":       {"executor_bin": "bin/executor", "executor_data_dir": "/d"},
		"executor without a dir":  {"executor_bin": "/bin/executor"},
		"relative data dir":       {"executor_bin": "/bin/executor", "executor_data_dir": "d"},
		"duplicate provider":      {"providers": []map[string]any{{"id": "a", "type": "openai"}, {"id": "a", "type": "openai"}}},
		"provider id a flag":      {"providers": []map[string]any{{"id": "-a", "type": "openai"}}},
		"key over plain http":     {"providers": []map[string]any{{"id": "a", "type": "openai", "base_url": "http://llm.example.com"}}},
		"newline in a key":        {"providers": []map[string]any{{"id": "a", "type": "openai", "api_key": "k\nX-Evil: 1"}}},
		"unknown provider field":  {"providers": []map[string]any{{"id": "a", "type": "openai", "secret": "x"}}},
		"credentials in base url": {"providers": []map[string]any{{"id": "a", "type": "openai", "base_url": "https://u:p@llm.example.com"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig([]byte(configWith(t, changes))); err == nil {
				t.Fatal("loadConfig accepted it")
			}
		})
	}
}

// A provider's key is in the stdin document and nowhere else this program
// writes: not in a log line while the executor starts and serves.
func TestProviderKeysNeverReachTheLog(t *testing.T) {
	logs := captureLogs(t)
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	_ = request(t, h.cfg, h.st, http.MethodPost, "/agent.run", `{"run_id":"r-8","prompt":"leak"}`)
	h.sup.close()
	if strings.Contains(logs.String(), fakeProviderKey) {
		t.Fatalf("a provider key reached the log:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "local executor ready") {
		t.Fatalf("the log never said the executor was ready:\n%s", logs.String())
	}
}
