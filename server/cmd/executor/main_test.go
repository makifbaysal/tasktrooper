package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/executor"
)

const runAsExecutorEnv = "TT_EXECUTOR_TEST_RUN_MAIN"

// TestMain lets the test binary stand in for the built command: run with the
// env var set it is the executor's main and nothing else, so the process
// contract (stdin config, stdout line, stdin EOF) is tested on the real entry
// point without a separate go build.
func TestMain(m *testing.M) {
	if os.Getenv(runAsExecutorEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestTheProcessServesUntilItsStdinCloses(t *testing.T) {
	const token = "process-token-0123456789"
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), runAsExecutorEnv+"=1")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	config := fmt.Sprintf(`{"token":%q,"workspace_root":%q,"providers":[{"id":"main","type":"openai","api_key":"sk-process-test-key"}]}`,
		token, t.TempDir())
	_, err = io.WriteString(stdin, config+"\n")
	require.NoError(t, err)

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, executor.ListeningPrefix+"http://127.0.0.1:"), line)
	base := strings.TrimSpace(strings.TrimPrefix(line, executor.ListeningPrefix))

	req, err := http.NewRequest(http.MethodGet, base+"/exec/health", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	var health struct {
		OK       bool `json:"ok"`
		Protocol int  `json:"protocol"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, health.OK)
	assert.Equal(t, 1, health.Protocol)

	unauthorized, err := http.Get(base + "/exec/health")
	require.NoError(t, err)
	_ = unauthorized.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, unauthorized.StatusCode)

	require.NoError(t, stdin.Close())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the executor kept running after its stdin closed")
	}
}

func TestABadConfigExitsWithoutListening(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), runAsExecutorEnv+"=1")
	cmd.Stdin = strings.NewReader(`{"token":"short"}` + "\n")
	var stdout strings.Builder
	cmd.Stdout = &stdout

	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.ExitCode())
	assert.Empty(t, stdout.String())
}
