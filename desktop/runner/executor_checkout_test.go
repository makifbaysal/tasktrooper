package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fakeGitHubToken = "ghs_fake-installation-token-0123456789"

// registerFakeCheckout gives the stand-in executor the post-run routes. Each
// records what it was sent, so a test can see the body arrived unchanged.
func registerFakeCheckout(mux *http.ServeMux, authorized func(http.ResponseWriter, *http.Request) bool, record func(name, text string), key string) {
	mux.HandleFunc("POST /exec/verify", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		record("verify", string(raw))
		var req struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &req)
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		send := func(s string) {
			_, _ = w.Write([]byte(s + "\n"))
			flusher.Flush()
		}
		send(`{"v":1,"id":"` + req.ID + `","event":"started"}`)
		send(`{"v":1,"id":"` + req.ID + `","event":"event","payload":{"seq":1,"kind":"verify_stage","phase":"started","name":"build","command":["go","build","./..."]}}`)
		send(`{"v":1,"id":"` + req.ID + `","event":"event","payload":{"seq":2,"kind":"verify_output","stage":"build","data":"compiling with ` + key + `"}}`)
		if strings.Contains(string(raw), "hang") {
			<-r.Context().Done()
			return
		}
		send(`{"v":1,"id":"` + req.ID + `","event":"done","ok":true,"result":{"passed":false,"report":"$ go build ./...\nboom","stages":[],"advisory":{"coverage":false,"mutation":false},"duration_ms":1}}`)
	})
	mux.HandleFunc("POST /exec/scan", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		record("scan", string(raw))
		var req struct {
			ID        string `json:"id"`
			Workspace string `json:"workspace"`
		}
		_ = json.Unmarshal(raw, &req)
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		send := func(s string) {
			_, _ = w.Write([]byte(s + "\n"))
			flusher.Flush()
		}
		send(`{"v":1,"id":"` + req.ID + `","event":"started"}`)
		send(`{"v":1,"id":"` + req.ID + `","event":"event","payload":{"seq":1,"kind":"scan_stage","stage":"inventory","done":false}}`)
		send(`{"v":1,"id":"` + req.ID + `","event":"event","payload":{"seq":2,"kind":"scan_stage","stage":"inventory","done":true,"summary":"3 files listed via git, read with ` + key + `"}}`)
		if strings.Contains(req.Workspace, "hang") {
			<-r.Context().Done()
			return
		}
		send(`{"v":1,"id":"` + req.ID + `","event":"done","ok":true,"result":{"shape":"single","file_count":3,"components":[{"path":"."}],"git":{"default_branch":"main","head_sha":"abc"}}}`)
	})
	for _, route := range []string{"git.status", "git.diff", "git.log", "commit_push"} {
		mux.HandleFunc("POST /exec/"+route, func(w http.ResponseWriter, r *http.Request) {
			if !authorized(w, r) {
				return
			}
			raw, _ := io.ReadAll(r.Body)
			record(route, string(raw))
			var req struct {
				Workspace   string `json:"workspace"`
				GitHubToken string `json:"github_token"`
			}
			_ = json.Unmarshal(raw, &req)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(string(raw), "conflict") {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"v":1,"error":{"code":"conflict","message":"the checkout refuses: ` + req.GitHubToken + `"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"route": route, "workspace": req.Workspace, "echo": req.GitHubToken})
		})
	}
}

func TestVerifyIsForwardedAndStreamedScrubbed(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	body := `{"id":"c-v1","workspace":"repos/a/task-1","commands":[{"argv":["go","build","./..."]}]}`
	res := request(t, h.cfg, h.st, http.MethodPost, "/verify", body)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	lines := strings.Split(strings.TrimSpace(res.body), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want started, two events and done:\n%s", len(lines), res.body)
	}
	if !strings.Contains(lines[0], `"event":"started"`) || !strings.Contains(lines[3], `"passed":false`) {
		t.Fatalf("body = %s, want the executor's frames in order", res.body)
	}
	for _, line := range lines {
		if !strings.Contains(line, `"id":"c-v1"`) {
			t.Errorf("line %s lost the call id", line)
		}
	}
	if strings.Contains(res.body, fakeProviderKey) || !strings.Contains(res.body, "[redacted PROVIDER_API_KEY:openai-main]") {
		t.Errorf("body = %s, want the provider key scrubbed", res.body)
	}
	if got := h.lines(t, "verify"); len(got) != 1 || got[0] != body {
		t.Errorf("the executor got %v, want the body unchanged", got)
	}
}

func TestVerifyIsRefusedBeforeItIsForwarded(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	for name, body := range map[string]string{
		"no call id":             `{"workspace":"repos/a/task-1"}`,
		"a workspace outside":    `{"id":"c-v2","workspace":"../../etc"}`,
		"no workspace":           `{"id":"c-v3"}`,
		"an id that is not one":  `{"id":"-rf","workspace":"repos/a/task-1"}`,
		"a body that is not one": `[1]`,
	} {
		res := request(t, h.cfg, h.st, http.MethodPost, "/verify", body)
		if res.status != http.StatusBadRequest {
			t.Errorf("%s: status = %d body=%s, want 400", name, res.status, res.body)
		}
	}
	if got := h.lines(t, "verify"); len(got) != 0 {
		t.Errorf("refused calls reached the executor: %v", got)
	}
}

func TestCancelStopsAVerificationAndTellsTheExecutorByItsID(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	srv := httptest.NewServer(newRunnerServer(h.cfg, h.st).handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/verify", "application/json", strings.NewReader(`{"id":"c-vh","workspace":"repos/a/task-1","verify_command":"hang"}`))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	reader := bufio.NewReader(res.Body)
	for range 3 {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("reading the first lines: %v", err)
		}
	}
	cancel, err := http.Post(srv.URL+"/cancel", "application/json", strings.NewReader(`{"id":"c-vh"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	raw, _ := io.ReadAll(cancel.Body)
	_ = cancel.Body.Close()
	if !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("cancel answered %s, want cancelled:true", raw)
	}
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), `"event":"done"`) || !strings.Contains(string(rest), codeCancelled) {
		t.Fatalf("the stream ended with %q, want one done carrying %s", rest, codeCancelled)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.lines(t, "cancels")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the executor was never told the verification was cancelled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.lines(t, "cancels"); got[0] != "|c-vh" {
		t.Fatalf("cancels = %v, want the verification named by its stream id", got)
	}
}

func TestGitAndCommitPushAreForwardedAndTheTokenComesBackScrubbed(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	for _, method := range []string{"git.status", "git.diff", "git.log"} {
		body := `{"id":"c-g","workspace":"repos/a/task-1","base":"HEAD"}`
		res := request(t, h.cfg, h.st, http.MethodPost, "/"+method, body)
		if res.status != http.StatusOK || !strings.Contains(res.body, `"route":"`+method+`"`) {
			t.Errorf("%s: status = %d body=%s, want the executor's own answer", method, res.status, res.body)
		}
		if got := h.lines(t, method); len(got) != 1 || got[0] != body {
			t.Errorf("%s: the executor got %v, want the body unchanged", method, got)
		}
	}

	push := `{"id":"c-p","workspace":"repos/a/task-1","message":"feat: x","branch":"feature/tt-1","github_token":"` + fakeGitHubToken + `"}`
	res := request(t, h.cfg, h.st, http.MethodPost, "/commit_push", push)
	if res.status != http.StatusOK {
		t.Fatalf("commit_push: status = %d body=%s", res.status, res.body)
	}
	if strings.Contains(res.body, fakeGitHubToken) || !strings.Contains(res.body, "[redacted "+githubTokenLabel+"]") {
		t.Errorf("commit_push answered %s, want the token scrubbed", res.body)
	}
	if got := h.lines(t, "commit_push"); len(got) != 1 || !strings.Contains(got[0], fakeGitHubToken) {
		t.Errorf("the executor got %v, want the token forwarded to it", got)
	}

	refused := request(t, h.cfg, h.st, http.MethodPost, "/commit_push",
		`{"workspace":"repos/a/task-1","message":"conflict","branch":"main","github_token":"`+fakeGitHubToken+`"}`)
	if refused.status != http.StatusConflict || !strings.Contains(refused.body, `"code":"conflict"`) {
		t.Errorf("the executor's refusal came back as %d %s, want its 409 verbatim", refused.status, refused.body)
	}
	if strings.Contains(refused.body, fakeGitHubToken) {
		t.Errorf("a refusal carried the token: %s", refused.body)
	}
}

func TestPostRunMethodsWithoutAnExecutorAreNotReady(t *testing.T) {
	cfg := config{workspaceDir: t.TempDir()}
	for _, method := range []string{"verify", "scan", "git.status", "git.diff", "git.log", "commit_push"} {
		res := request(t, cfg, newState(), http.MethodPost, "/"+method, `{"id":"c-n","workspace":"repos/a/task-1"}`)
		if res.status != http.StatusConflict || res.code() != codeNotReady {
			t.Errorf("%s: status = %d body=%s, want 409 not_ready", method, res.status, res.body)
		}
	}
}

func TestScanIsForwardedAsADurableStreamScrubbed(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	body := `{"id":"c-s1","workspace":"repos/a/default"}`
	res := request(t, h.cfg, h.st, http.MethodPost, "/scan", body)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	lines := strings.Split(strings.TrimSpace(res.body), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want started, two scan_stage events and done:\n%s", len(lines), res.body)
	}
	if !strings.Contains(lines[0], `"event":"started"`) || !strings.Contains(lines[1], `"kind":"scan_stage"`) ||
		!strings.Contains(lines[3], `"event":"done"`) || !strings.Contains(lines[3], `"file_count":3`) {
		t.Fatalf("body = %s, want the executor's frames in order, the result in the done", res.body)
	}
	for i, line := range lines {
		if !strings.Contains(line, `"id":"c-s1"`) {
			t.Errorf("line %s lost the call id", line)
		}
		if want := `"seq":` + string(rune('1'+i)) + `}`; !strings.HasSuffix(line, want) {
			t.Errorf("line %s, want the runner's seq %s last: a scan is a durable run", line, want)
		}
	}
	if strings.Contains(res.body, fakeProviderKey) || !strings.Contains(res.body, "[redacted PROVIDER_API_KEY:openai-main]") {
		t.Errorf("body = %s, want the provider key scrubbed", res.body)
	}
	if got := h.lines(t, "scan"); len(got) != 1 || got[0] != body {
		t.Errorf("the executor got %v, want the body unchanged", got)
	}

	status := request(t, h.cfg, h.st, http.MethodPost, "/run.status", `{"id":"c-s1"}`)
	if !strings.Contains(status.body, `"state":"done"`) {
		t.Errorf("run.status = %s, want the finished scan kept for run.attach", status.body)
	}
}

func TestScanIsRefusedBeforeItIsForwarded(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)

	for name, body := range map[string]string{
		"no call id":            `{"workspace":"repos/a/default"}`,
		"a workspace outside":   `{"id":"c-s2","workspace":"../../etc"}`,
		"an absolute workspace": `{"id":"c-s3","workspace":"/etc"}`,
		"no workspace":          `{"id":"c-s4"}`,
	} {
		res := request(t, h.cfg, h.st, http.MethodPost, "/scan", body)
		if res.status != http.StatusBadRequest {
			t.Errorf("%s: status = %d body=%s, want 400", name, res.status, res.body)
		}
	}
	if got := h.lines(t, "scan"); len(got) != 0 {
		t.Errorf("refused calls reached the executor: %v", got)
	}
}

func TestCancelStopsAScanAndTellsTheExecutorByItsID(t *testing.T) {
	h := startFakeExecutor(t, "ok", fastExecutorTimings)
	h.awaitReady(t)
	srv := httptest.NewServer(newRunnerServer(h.cfg, h.st).handler())
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/scan", "application/json", strings.NewReader(`{"id":"c-sh","workspace":"repos/hang/default"}`))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	reader := bufio.NewReader(res.Body)
	for range 3 {
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("reading the first lines: %v", err)
		}
	}
	cancel, err := http.Post(srv.URL+"/cancel", "application/json", strings.NewReader(`{"id":"c-sh"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	raw, _ := io.ReadAll(cancel.Body)
	_ = cancel.Body.Close()
	if !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("cancel answered %s, want cancelled:true", raw)
	}
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), `"event":"done"`) || !strings.Contains(string(rest), codeCancelled) ||
		!strings.Contains(string(rest), "the scan was cancelled") {
		t.Fatalf("the stream ended with %q, want one done carrying %s", rest, codeCancelled)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.lines(t, "cancels")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the executor was never told the scan was cancelled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := h.lines(t, "cancels"); got[0] != "|c-sh" {
		t.Fatalf("cancels = %v, want the scan named by its stream id", got)
	}
}
