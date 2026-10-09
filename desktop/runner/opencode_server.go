package main

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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// OpenCode 2.x (npm @opencode/cli, `--version` prints "opencode v2.…") runs
// every `opencode run` on a shared background service by default. That
// service was started with somebody else's environment, so it never sees this
// run's OPENCODE_CONFIG_CONTENT and the session has no tasktrooper tools. On
// 2.x a run therefore gets a private `opencode serve --stdio` started with the
// config, waits until the tasktrooper MCP server has connected in the run's
// location, and attaches with `run --server`. 1.x reads the env var in its own
// process and keeps doing exactly that.

const (
	opencodeVersionTimeout     = 15 * time.Second
	opencodeServerReadyTimeout = time.Minute
	opencodeMCPReadyTimeout    = 20 * time.Second
	opencodeMCPPollInterval    = 100 * time.Millisecond
	// opencodeMCPSettle outlasts 2.x's 100ms debounce between a server
	// reporting connected and its tools landing in the registry a session
	// snapshots when the prompt arrives.
	opencodeMCPSettle  = 500 * time.Millisecond
	opencodeServerUser = "opencode"
)

var opencodeSemver = regexp.MustCompile(`(\d+)\.\d+\.\d+`)

func opencodeMajor(out string) (int, bool) {
	m := opencodeSemver.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	major, err := strconv.Atoi(m[1])
	return major, err == nil
}

// opencodeGeneration remembers which opencode a binary is, keyed on the file's
// identity so an in-place upgrade from 1.x to 2.x is noticed on the next run
// without restarting the runner.
type opencodeGeneration struct {
	mu    sync.Mutex
	key   string
	major int
}

var opencodeGen = &opencodeGeneration{}

func (g *opencodeGeneration) serverBased(ctx context.Context, bin string) bool {
	key := bin
	if info, err := os.Stat(bin); err == nil {
		key = fmt.Sprintf("%s:%d:%d", bin, info.Size(), info.ModTime().UnixNano())
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if key != g.key {
		g.key, g.major = key, readOpencodeMajor(ctx, bin)
	}
	return g.major >= 2
}

func readOpencodeMajor(ctx context.Context, bin string) int {
	ctx, cancel := context.WithTimeout(ctx, opencodeVersionTimeout)
	defer cancel()
	l, err := launcherFor(bin)
	if err != nil {
		log.Warn().Err(err).Str("binary", bin).Msg("could not read the opencode version; running it the 1.x way")
		return 1
	}
	cmd := l.commandContext(ctx, "--version")
	cmd.Dir = os.TempDir()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Run()
	major, ok := opencodeMajor(buf.String())
	if err != nil || !ok {
		log.Warn().Err(err).Str("binary", bin).Str("output", strings.TrimSpace(buf.String())).
			Msg("could not read the opencode version; running it the 1.x way")
		return 1
	}
	if major >= 2 {
		log.Info().Str("binary", bin).Int("major", major).
			Msg("opencode 2.x: each session runs on a private opencode server so it gets the tasktrooper tools")
	}
	return major
}

// opencodeLaunch is what one run adds on top of its prepared args and env.
type opencodeLaunch struct {
	args []string
	env  []string
	stop func()
}

func (s *runnerServer) launchOpencode(ctx context.Context, callID string, p preparedOpencodeRun) (opencodeLaunch, *rpcError) {
	if !opencodeGen.serverBased(ctx, s.cfg.opencodeBin) {
		env := p.env
		if p.mcp != nil {
			content, err := opencodeMCPEnv(p.mcp)
			if err != nil {
				return opencodeLaunch{}, failure(codeInternal, "could not encode mcp config: %v", err)
			}
			env = append(env, "OPENCODE_CONFIG_CONTENT="+content)
		}
		return opencodeLaunch{args: p.args, env: env, stop: func() {}}, nil
	}

	// 2.x roots the session at $PWD before its working directory, and this
	// process's own PWD would otherwise be inherited.
	env := append(append([]string{}, p.env...), "PWD="+p.dir)
	serverEnv := env
	if p.mcp != nil {
		content, err := opencodeServersMCPEnv(p.mcp)
		if err != nil {
			return opencodeLaunch{}, failure(codeInternal, "could not encode mcp config: %v", err)
		}
		serverEnv = append(append([]string{}, env...), "OPENCODE_CONFIG_CONTENT="+content)
	}
	srv, err := startOpencodeServer(ctx, s.cfg.opencodeBin, p.dir, serverEnv)
	if err != nil {
		return opencodeLaunch{}, failure(codeInternal, "could not start a private OpenCode server: %v", err)
	}
	if p.mcp != nil {
		if err := srv.waitForMCP(ctx, p.mcp.serverName, opencodeLocation(p.dir)); err != nil {
			if ctx.Err() != nil {
				srv.stop()
				return opencodeLaunch{}, failure(codeCancelled, "the call was cancelled before the session started")
			}
			log.Warn().Str("call", callID).Err(err).
				Msg("opencode session starts before its tasktrooper tools connected; its first turn may not see them")
		}
	}
	return opencodeLaunch{
		args: append(append([]string{}, p.args...), "--server", srv.url),
		env:  append(env, "OPENCODE_PASSWORD="+srv.password),
		stop: srv.stop,
	}, nil
}

// opencodeServersMCPEnv is opencodeMCPEnv in 2.x's shape. The run token is a
// plain bearer, so OAuth discovery is off, and code mode is off so the tools
// stay direct <server>_* tools as they are on 1.x.
func opencodeServersMCPEnv(cfg *mcpConfig) (string, error) {
	b, err := json.Marshal(map[string]any{
		"mcp": map[string]any{
			"servers": map[string]any{
				cfg.serverName: map[string]any{
					"type":     "remote",
					"url":      cfg.url,
					"headers":  map[string]string{"Authorization": "Bearer " + cfg.token},
					"oauth":    false,
					"codemode": false,
				},
			},
		},
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// opencodeServer is an `opencode serve --stdio` owned by one run; EOF on its
// stdin ends its lease.
type opencodeServer struct {
	group    *processGroup
	stdin    io.WriteCloser
	url      string
	password string
	done     chan struct{}
}

func startOpencodeServer(ctx context.Context, bin, dir string, env []string) (*opencodeServer, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)

	cmd, err := commandFor(bin, "serve", "--stdio", "--port", "0", "--hostname", "127.0.0.1")
	if err != nil {
		return nil, err
	}
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), env...), "OPENCODE_PASSWORD="+password)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	ready := &opencodeReadyLine{line: make(chan string, 1)}
	var stderr bytes.Buffer
	cmd.Stdout = ready
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 4 << 10}
	cmd.WaitDelay = claudeGrace
	group, err := startProcessGroup(cmd)
	if err != nil {
		return nil, err
	}
	s := &opencodeServer{group: group, stdin: stdin, password: password, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		group.release()
		close(s.done)
	}()

	timer := time.NewTimer(opencodeServerReadyTimeout)
	defer timer.Stop()
	select {
	case line := <-ready.line:
		var info struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(line), &info); err != nil || info.URL == "" {
			s.stop()
			return nil, fmt.Errorf("it did not report its address (%q): %s", line, strings.TrimSpace(stderr.String()))
		}
		s.url = strings.TrimRight(info.URL, "/")
		return s, nil
	case <-s.done:
		return nil, fmt.Errorf("it exited before it was ready: %s", strings.TrimSpace(stderr.String()))
	case <-timer.C:
		s.stop()
		return nil, fmt.Errorf("it was not ready after %s", opencodeServerReadyTimeout)
	case <-ctx.Done():
		s.stop()
		return nil, ctx.Err()
	}
}

func (s *opencodeServer) waitForMCP(ctx context.Context, serverName, directory string) error {
	query := url.Values{}
	query.Set("location[directory]", directory)
	endpoint := s.url + "/api/mcp?" + query.Encode()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(opencodeMCPReadyTimeout)
	last := "not listed"
	for {
		status, detail, err := s.mcpStatus(ctx, client, endpoint, serverName)
		switch {
		case err != nil:
			last = err.Error()
		case status == "connected":
			return sleepContext(ctx, opencodeMCPSettle)
		case status != "" && status != "pending":
			return fmt.Errorf("the %s mcp server is %s: %s", serverName, status, detail)
		case status == "pending":
			last = "pending"
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the %s mcp server was still %s after %s", serverName, last, opencodeMCPReadyTimeout)
		}
		if err := sleepContext(ctx, opencodeMCPPollInterval); err != nil {
			return err
		}
	}
}

func (s *opencodeServer) mcpStatus(ctx context.Context, client *http.Client, endpoint, serverName string) (status, detail string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.SetBasicAuth(opencodeServerUser, s.password)
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
		if server.Name == serverName {
			return server.Status.Status, server.Status.Error, nil
		}
	}
	return "", "", nil
}

func (s *opencodeServer) stop() {
	_ = s.stdin.Close()
	select {
	case <-s.done:
		return
	case <-time.After(claudeGrace):
	}
	killProcessTree(s.group)
	<-s.done
}

// opencodeLocation is the directory 2.x keys the run's location on: `opencode
// run` chdirs into $PWD and reads it back, which resolves symlinks, so polling
// the unresolved path would boot a different location.
func opencodeLocation(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// opencodeReadyLine hands the server's readiness line over and discards the
// rest, so a server that keeps writing to stdout never blocks on a full pipe.
type opencodeReadyLine struct {
	line chan string
	buf  []byte
	sent bool
}

func (w *opencodeReadyLine) Write(p []byte) (int, error) {
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

// limitedBuffer keeps the first max bytes, enough to quote why a server
// would not start.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}
