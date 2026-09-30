package opencode

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
)

const (
	serverReadyTimeout = time.Minute
	mcpReadyTimeout    = 20 * time.Second
	mcpPollInterval    = 100 * time.Millisecond
	// mcpSettle outlasts 2.x's 100ms debounce between a server reporting
	// connected and its tools landing in the registry a session snapshots.
	mcpSettle       = 500 * time.Millisecond
	serverStopGrace = 5 * time.Second
	serverUser      = "opencode"
)

// privateServer is an `opencode serve` owned by one run. opencode 2.x runs
// every `opencode run` on a shared background service by default, which was
// started with somebody else's environment and never sees this run's
// OPENCODE_CONFIG_CONTENT, so the session would have no tasktrooper tools.
// With --stdio the server treats EOF on its stdin as the end of its lease.
type privateServer struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	url      string
	password string
	done     chan struct{}
}

func startPrivateServer(ctx context.Context, bin, workDir string, env []string) (*privateServer, error) {
	password, err := serverPassword()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "serve", "--stdio", "--port", "0", "--hostname", "127.0.0.1")
	cmd.Dir = workDir
	cmd.Env = append(append([]string{}, env...), "OPENCODE_PASSWORD="+password)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode server stdin: %w", err)
	}
	ready := &firstLine{line: make(chan string, 1)}
	stderr := core.NewTailWriter(core.StderrTailMax)
	cmd.Stdout = ready
	cmd.Stderr = stderr
	cmd.WaitDelay = serverStopGrace
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode server: %w", err)
	}
	s := &privateServer{cmd: cmd, stdin: stdin, password: password, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(s.done)
	}()

	timer := time.NewTimer(serverReadyTimeout)
	defer timer.Stop()
	select {
	case line := <-ready.line:
		var info struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(line), &info); err != nil || info.URL == "" {
			s.stop()
			return nil, fmt.Errorf("opencode server did not report its address (%q)%s", line, core.Tail(stderr.String()))
		}
		s.url = strings.TrimRight(info.URL, "/")
		return s, nil
	case <-s.done:
		return nil, fmt.Errorf("opencode server exited before it was ready%s", core.Tail(stderr.String()))
	case <-timer.C:
		s.stop()
		return nil, fmt.Errorf("opencode server was not ready after %s%s", serverReadyTimeout, core.Tail(stderr.String()))
	case <-ctx.Done():
		s.stop()
		return nil, ctx.Err()
	}
}

// waitForMCP holds the run until the tasktrooper server has connected in the
// run's location and its tools have settled: 2.x connects MCP servers in the
// background and snapshots the tool list when the prompt arrives, so a session
// started first answers its first turn without the board tools.
func (s *privateServer) waitForMCP(ctx context.Context, directory string) error {
	query := url.Values{}
	query.Set("location[directory]", directory)
	endpoint := s.url + "/api/mcp?" + query.Encode()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(mcpReadyTimeout)
	last := "not listed"
	for {
		status, detail, err := s.mcpStatus(ctx, client, endpoint)
		switch {
		case err != nil:
			last = err.Error()
		case status == "connected":
			return sleepCtx(ctx, mcpSettle)
		case status != "" && status != "pending":
			return fmt.Errorf("the %s mcp server is %s: %s", mcpServerName, status, detail)
		case status == "pending":
			last = "pending"
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the %s mcp server was still %s after %s", mcpServerName, last, mcpReadyTimeout)
		}
		if err := sleepCtx(ctx, mcpPollInterval); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *privateServer) mcpStatus(ctx context.Context, client *http.Client, endpoint string) (status, detail string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.SetBasicAuth(serverUser, s.password)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("mcp status answered %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			Name   string `json:"name"`
			Status struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("mcp status: %w", err)
	}
	for _, server := range body.Data {
		if server.Name == mcpServerName {
			return server.Status.Status, server.Status.Error, nil
		}
	}
	return "", "", nil
}

func (s *privateServer) stop() {
	_ = s.stdin.Close()
	select {
	case <-s.done:
		return
	case <-time.After(serverStopGrace):
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	<-s.done
}

// locationDirectory is the directory 2.x keys the run's location on: `opencode
// run` chdirs into its working directory and reads it back, which resolves
// symlinks, so polling the unresolved path would boot a different location.
func locationDirectory(workDir string) string {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return workDir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func serverPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("opencode server password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// firstLine hands the server's readiness line over and discards the rest, so
// a server that keeps writing to stdout never blocks on a full pipe.
type firstLine struct {
	line chan string
	buf  []byte
	sent bool
}

func (w *firstLine) Write(p []byte) (int, error) {
	if w.sent {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	if i := bytes.IndexByte(w.buf, '\n'); i >= 0 {
		w.line <- strings.TrimSpace(string(w.buf[:i]))
		w.sent = true
		w.buf = nil
	}
	return len(p), nil
}
