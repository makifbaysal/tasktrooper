//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOpencodeMajorReadsBothGenerations(t *testing.T) {
	if major, ok := opencodeMajor("1.18.33\n"); !ok || major != 1 {
		t.Fatalf("1.x: got %d %v", major, ok)
	}
	if major, ok := opencodeMajor("opencode v2.0.13\n"); !ok || major != 2 {
		t.Fatalf("2.x: got %d %v", major, ok)
	}
	if _, ok := opencodeMajor("command not found"); ok {
		t.Fatal("a line with no version must not parse")
	}
}

// fakeOpencode2 writes a stand-in for an opencode 2.x binary: `serve` reports
// the stand-in API's address and holds until its stdin closes, `run` records
// what it was given. Both write into their working directory.
func fakeOpencode2(t *testing.T, apiURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode")
	script := `#!/bin/sh
case "$1" in
--version) echo "opencode v2.0.13"; exit 0 ;;
serve)
    env | sort > serve_env.txt
    printf '{"url":"` + apiURL + `/"}\n'
    cat > /dev/null
    : > serve_stopped
    exit 0 ;;
esac
for arg in "$@"; do printf '%s\n' "$arg"; done > run_argv.txt
env | sort > run_env.txt
cat > run_prompt.txt
echo '{"type":"step_finish","part":{"reason":"stop"}}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake CLI: %v", err)
	}
	return path
}

func envLine(t *testing.T, path, name string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if value, ok := strings.CutPrefix(line, name+"="); ok {
			return value
		}
	}
	return ""
}

// On 2.x the run attaches to a private server started with the MCP config,
// and only once that server reports the tasktrooper server connected.
func TestOpencodeRunOn2xAttachesToAPrivateServer(t *testing.T) {
	var mu sync.Mutex
	statuses := []string{"pending", "connected"}
	polls := 0
	var locations []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if _, _, ok := r.BasicAuth(); !ok || r.URL.Path != "/api/mcp" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		locations = append(locations, r.URL.Query().Get("location[directory]"))
		status := statuses[min(polls, len(statuses)-1)]
		polls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{"name": "tasktrooper", "status": map[string]any{"status": status}},
		}})
	}))
	defer api.Close()

	workspace := emptyWorkspace(t)
	cfg := config{opencodeBin: fakeOpencode2(t, api.URL), gitBin: "/usr/bin/git", workspaceDir: workspace}
	res := request(t, cfg, newState(), http.MethodPost, "/opencode.run",
		`{"workspace":"repo","prompt":"what is on the board","model":"opencode/big-pickle",`+
			`"mcp":{"url":"https://example.test/api/mcp","token":"run-token","server_name":"tasktrooper"}}`)
	if res.status != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.status, res.body)
	}
	if !strings.Contains(res.body, `"exit_code":0`) {
		t.Fatalf("the session did not finish cleanly: %s", res.body)
	}

	dir := filepath.Join(workspace, "repo")
	config := envLine(t, filepath.Join(dir, "serve_env.txt"), "OPENCODE_CONFIG_CONTENT")
	for _, want := range []string{`"servers"`, `"tasktrooper"`, `"codemode":false`, `"oauth":false`, `Bearer run-token`} {
		if !strings.Contains(config, want) {
			t.Fatalf("server config %s lacks %s", config, want)
		}
	}
	password := envLine(t, filepath.Join(dir, "serve_env.txt"), "OPENCODE_PASSWORD")
	if password == "" || envLine(t, filepath.Join(dir, "run_env.txt"), "OPENCODE_PASSWORD") != password {
		t.Fatal("the run must authenticate to its server with the server's password")
	}
	if envLine(t, filepath.Join(dir, "run_env.txt"), "OPENCODE_CONFIG_CONTENT") != "" {
		t.Fatal("the run token belongs to the server, not the attaching client")
	}
	if got := envLine(t, filepath.Join(dir, "run_env.txt"), "PWD"); got != dir {
		t.Fatalf("PWD = %q, want the run's workspace %q", got, dir)
	}

	argv, _ := os.ReadFile(filepath.Join(dir, "run_argv.txt"))
	if !strings.Contains(string(argv), "--server\n"+api.URL+"\n") {
		t.Fatalf("run argv %q does not attach to the private server", argv)
	}
	if prompt, _ := os.ReadFile(filepath.Join(dir, "run_prompt.txt")); string(prompt) != "what is on the board" {
		t.Fatalf("prompt = %q, want it on stdin", prompt)
	}

	mu.Lock()
	defer mu.Unlock()
	if polls != 2 {
		t.Fatalf("polls = %d, want the run to wait out the pending answer", polls)
	}
	if locations[0] != opencodeLocation(dir) {
		t.Fatalf("polled location %q, want %q", locations[0], opencodeLocation(dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "serve_stopped")); err != nil {
		t.Fatal("the private server's lease must end with the run")
	}
}
