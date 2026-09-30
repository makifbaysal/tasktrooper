package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMajorVersionReadsBothGenerations(t *testing.T) {
	major, ok := majorVersion("1.18.33\n")
	assert.True(t, ok)
	assert.Equal(t, 1, major)

	major, ok = majorVersion("opencode v2.0.13\n")
	assert.True(t, ok)
	assert.Equal(t, 2, major)

	_, ok = majorVersion("command not found")
	assert.False(t, ok)
}

// fakeOpencodeAPI stands in for the private server's /api/mcp: it answers
// with the queued statuses in order, then keeps repeating the last one.
type fakeOpencodeAPI struct {
	url string

	mu        sync.Mutex
	statuses  []string
	polls     int
	auth      []string
	locations []string
}

func (a *fakeOpencodeAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.URL.Path != "/api/mcp" {
		http.NotFound(w, r)
		return
	}
	user, password, _ := r.BasicAuth()
	a.auth = append(a.auth, user+":"+password)
	a.locations = append(a.locations, r.URL.Query().Get("location[directory]"))
	status := a.statuses[min(a.polls, len(a.statuses)-1)]
	a.polls++
	data := []map[string]any{}
	if status != "" {
		data = append(data, map[string]any{"name": mcpServerName, "status": map[string]any{"status": status, "error": "boom"}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (a *fakeOpencodeAPI) seen() (polls int, auth, locations []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.polls, append([]string(nil), a.auth...), append([]string(nil), a.locations...)
}

func newOpencode2Executor(t *testing.T, cfg Config, statuses ...string) (*Executor, string, *fakeOpencodeAPI) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-opencode2.sh"))
	require.NoError(t, err)
	api := &fakeOpencodeAPI{statuses: statuses}
	srv := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(srv.Close)
	api.url = srv.URL

	workDir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("testdata", "success.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "fixture.jsonl"), body, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "serve_url.txt"), []byte(srv.URL+"/"), 0o600))

	cfg.Binary = script
	ex, err := New(cfg)
	require.NoError(t, err)
	return ex, workDir, api
}

func envValue(env, name string) string {
	for _, line := range strings.Split(env, "\n") {
		if value, ok := strings.CutPrefix(line, name+"="); ok {
			return value
		}
	}
	return ""
}

// On 2.x a plain `opencode run` lands on the shared background service, which
// never sees this run's config: the run gets a private server carrying the
// tasktrooper MCP server, and only attaches once that server has connected.
func TestExecuteOnOpencode2AttachesToAPrivateServerWithTheMCPConnected(t *testing.T) {
	ex, workDir, api := newOpencode2Executor(t, Config{
		MCP: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "run-token"},
	}, "", "pending", "connected")

	resp, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Message.Content)

	serveEnv := readFile(t, filepath.Join(workDir, "serve_env.txt"))
	config := envValue(serveEnv, "OPENCODE_CONFIG_CONTENT")
	var parsed struct {
		MCP struct {
			Servers map[string]map[string]any `json:"servers"`
		} `json:"mcp"`
	}
	require.NoError(t, json.Unmarshal([]byte(config), &parsed))
	server := parsed.MCP.Servers[mcpServerName]
	require.NotNil(t, server, "the private server must be started with the tasktrooper mcp server: %s", config)
	assert.Equal(t, "http://127.0.0.1:9/mcp", server["url"])
	assert.Equal(t, false, server["codemode"])
	assert.Equal(t, false, server["oauth"])
	assert.Equal(t, map[string]any{"Authorization": "Bearer run-token"}, server["headers"])

	password := envValue(serveEnv, "OPENCODE_PASSWORD")
	require.NotEmpty(t, password)

	runEnv := readFile(t, filepath.Join(workDir, "env.txt"))
	assert.Equal(t, password, envValue(runEnv, "OPENCODE_PASSWORD"))
	assert.NotContains(t, runEnv, "run-token", "the run token belongs to the server, not the attaching client")

	argv := readArgv(t, workDir)
	assert.Equal(t, "run", argv[0])
	require.Contains(t, argv, "--server")
	assert.Equal(t, api.url, argv[len(argv)-1])

	polls, auth, locations := api.seen()
	assert.Equal(t, 3, polls, "the run must wait out the not-yet-listed and pending answers")
	for _, a := range auth {
		assert.Equal(t, "opencode:"+password, a)
	}
	assert.Equal(t, locationDirectory(workDir), locations[0])

	assert.FileExists(t, filepath.Join(workDir, "serve_stopped"), "the server's lease must end with the run")
}

// A server that could not connect still gets its session: the answer without
// board tools is the CLI's to give, the same as on 1.x.
func TestExecuteOnOpencode2RunsEvenWhenTheMCPServerFailed(t *testing.T) {
	ex, workDir, api := newOpencode2Executor(t, Config{
		MCP: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "run-token"},
	}, "failed")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	polls, _, _ := api.seen()
	assert.Equal(t, 1, polls)
	assert.Contains(t, readArgv(t, workDir), "--server")
}

func TestExecuteOnOpencode2WithoutMCPDoesNotPoll(t *testing.T) {
	ex, workDir, api := newOpencode2Executor(t, Config{}, "connected")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	polls, _, _ := api.seen()
	assert.Zero(t, polls)
	assert.Empty(t, envValue(readFile(t, filepath.Join(workDir, "serve_env.txt")), "OPENCODE_CONFIG_CONTENT"))
	assert.Contains(t, readArgv(t, workDir), "--server")
}

func TestExecuteOnOpencode1KeepsTheInlineConfig(t *testing.T) {
	ex, workDir := newTestExecutor(t, Config{
		MCP: MCPConfig{URL: "http://127.0.0.1:9/mcp", Token: "run-token"},
	}, "success.jsonl")

	_, err := ex.Execute(context.Background(), taskExecution(workDir))
	require.NoError(t, err)
	assert.NotContains(t, readArgv(t, workDir), "--server")
	assert.NoFileExists(t, filepath.Join(workDir, "serve_argv.txt"))
}
