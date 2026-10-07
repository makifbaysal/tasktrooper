package appiumhub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
)

// The test binary doubles as the fake appium: started with Appium's own flags
// it serves /status on the address it was given. What it should do instead —
// fail, or take a while — is read from files in its working directory, the
// manager's WorkDir, so each test steers its own hub.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--address" {
		os.Exit(fakeAppium(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeAppium(args []string) int {
	flags := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			flags[args[i]] = args[i+1]
		}
	}
	appendLine("spawns.log", strconv.Itoa(os.Getpid())+" "+strings.Join(args, " "))
	appendLine("env.log", "ANDROID_HOME="+os.Getenv("ANDROID_HOME")+" APPIUM_HOME="+os.Getenv("APPIUM_HOME")+
		" SERVER_API_KEY="+os.Getenv("SERVER_API_KEY"))
	if msg, err := os.ReadFile("fail"); err == nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(msg)))
		return 1
	}
	if raw, err := os.ReadFile("delay"); err == nil {
		d, _ := time.ParseDuration(strings.TrimSpace(string(raw)))
		time.Sleep(d)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(flags["--address"], flags["--port"]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("[Appium] Welcome to the fake Appium")
	mux := http.NewServeMux()
	mux.HandleFunc(flags["--base-path"]+"/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true}}`))
	})
	_ = http.Serve(ln, mux)
	return 0
}

func appendLine(name, line string) {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

var testEnviron = []string{
	"PATH=/usr/bin:/bin",
	"HOME=/home/tester",
	"ANDROID_HOME=/sdk",
	"APPIUM_HOME=/appium-home",
	"SERVER_API_KEY=must-not-reach-appium",
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

type fixture struct {
	m   *Manager
	dir string
	hub string
}

func newFixture(t *testing.T, tweak func(*Config)) fixture {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		Bin:          os.Args[0],
		HubURL:       "http://127.0.0.1:" + freePort(t),
		WorkDir:      dir,
		ReadyTimeout: 15 * time.Second,
		ReapEvery:    50 * time.Millisecond,
		Environ:      func() []string { return testEnviron },
	}
	if tweak != nil {
		tweak(&cfg)
	}
	m := New(cfg)
	m.killLeftovers = func(string, time.Duration) (int, error) { return 0, nil }
	t.Cleanup(m.Close)
	return fixture{m: m, dir: dir, hub: cfg.HubURL}
}

func (f fixture) spawns(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "spawns.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func answers(base string) bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(strings.TrimRight(base, "/") + "/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestUnsetBinLeavesTheHubToWhoeverRunsIt(t *testing.T) {
	m := New(Config{HubURL: "http://127.0.0.1:1"})
	t.Cleanup(m.Close)
	m.Start()

	assert.False(t, m.Manages("http://127.0.0.1:1"))
	began := time.Now()
	require.NoError(t, m.Ensure(context.Background(), "http://127.0.0.1:1"))
	assert.Less(t, time.Since(began), 100*time.Millisecond, "an unmanaged hub is not even probed")
}

func TestEnsureAdoptsAHubThatAlreadyAnswersAndNeverStopsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true}}`))
	}))
	t.Cleanup(srv.Close)
	f := newFixture(t, func(c *Config) {
		c.HubURL = srv.URL
		c.IdleAfter = 50 * time.Millisecond
	})
	f.m.Start()

	require.NoError(t, f.m.Ensure(context.Background(), srv.URL))
	time.Sleep(300 * time.Millisecond)
	f.m.Close()

	assert.Empty(t, f.spawns(t), "a hub answered, so none was started")
	assert.True(t, answers(srv.URL), "the user's hub survived the idle reaper and the shutdown")
}

func TestEnsureStartsTheHubWhenNothingAnswers(t *testing.T) {
	f := newFixture(t, nil)

	require.NoError(t, f.m.Ensure(context.Background(), f.hub))

	assert.True(t, answers(f.hub))
	spawns := f.spawns(t)
	require.Len(t, spawns, 1)
	port := strings.TrimPrefix(f.hub, "http://127.0.0.1:")
	assert.Contains(t, spawns[0], "--address 127.0.0.1 --port "+port+" --log-no-colors")
}

func TestEnsureLeavesAHubAtAnotherAddressAlone(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, f.m.Ensure(context.Background(), "http://127.0.0.1:"+freePort(t)))
	assert.Empty(t, f.spawns(t))
}

func TestConcurrentCallersShareOneStart(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "delay"), []byte("400ms"), 0o600))

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { errs[i] = f.m.Ensure(context.Background(), f.hub) })
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, f.spawns(t), 1)
}

func TestAWaitingCallerCanGiveUpWithoutCancellingTheStart(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "delay"), []byte("400ms"), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, f.m.Ensure(ctx, f.hub), context.DeadlineExceeded)

	require.NoError(t, f.m.Ensure(context.Background(), f.hub))
	assert.Len(t, f.spawns(t), 1)
}

func TestAnIdleHubIsStoppedAndStartedAgainOnDemand(t *testing.T) {
	f := newFixture(t, func(c *Config) { c.IdleAfter = 300 * time.Millisecond })
	f.m.Start()

	require.NoError(t, f.m.Ensure(context.Background(), f.hub))
	require.True(t, answers(f.hub))
	require.Eventually(t, func() bool { return !answers(f.hub) }, 10*time.Second, 50*time.Millisecond,
		"the hub outlived its idle time")

	require.NoError(t, f.m.Ensure(context.Background(), f.hub))
	assert.True(t, answers(f.hub))
	assert.Len(t, f.spawns(t), 2)
}

func TestAnOpenLeaseKeepsTheHubPastItsIdleTime(t *testing.T) {
	f := newFixture(t, func(c *Config) { c.IdleAfter = 150 * time.Millisecond })
	var leased atomic.Bool
	leased.Store(true)
	f.m.SetInUse(leased.Load)
	f.m.Start()

	require.NoError(t, f.m.Ensure(context.Background(), f.hub))
	time.Sleep(700 * time.Millisecond)
	require.True(t, answers(f.hub), "the hub was stopped under an open lease")

	leased.Store(false)
	assert.Eventually(t, func() bool { return !answers(f.hub) }, 10*time.Second, 50*time.Millisecond)
}

func TestCloseStopsTheHubItStarted(t *testing.T) {
	f := newFixture(t, nil)
	f.m.Start()
	require.NoError(t, f.m.Ensure(context.Background(), f.hub))
	pid, err := strconv.Atoi(strings.Fields(f.spawns(t)[0])[0])
	require.NoError(t, err)

	f.m.Close()

	assert.False(t, answers(f.hub))
	assert.False(t, processAlive(pid), "the hub process outlived Close")
	assert.Error(t, f.m.Ensure(context.Background(), f.hub), "a closed manager starts nothing")
}

func TestAFailedStartSaysWhyAndIsNotRetriedAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, os.WriteFile(filepath.Join(f.dir, "fail"), []byte("Could not find a driver for automationName"), 0o600))

	err := f.m.Ensure(context.Background(), f.hub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not find a driver for automationName")

	again := f.m.Ensure(context.Background(), f.hub)
	assert.Equal(t, err, again)
	assert.Len(t, f.spawns(t), 1)
}

func TestTheHubGetsTheToolchainEnvironmentAndNoSecrets(t *testing.T) {
	f := newFixture(t, nil)
	require.NoError(t, f.m.Ensure(context.Background(), f.hub))

	raw, err := os.ReadFile(filepath.Join(f.dir, "env.log"))
	require.NoError(t, err)
	got := strings.TrimSpace(string(raw))
	assert.Contains(t, got, "ANDROID_HOME=/sdk")
	assert.Contains(t, got, "APPIUM_HOME=/appium-home")
	assert.True(t, strings.HasSuffix(got, "SERVER_API_KEY="), "a secret reached the hub: %s", got)
}

func TestStartStopsAHubAPreviousServerLeftBehind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows the job object ends the hub with the server that started it")
	}
	f := newFixture(t, nil)
	f.m.killLeftovers = proctree.KillProcessesUnder

	port := strings.TrimPrefix(f.hub, "http://127.0.0.1:")
	orphan := exec.Command(os.Args[0], "--address", "127.0.0.1", "--port", port, "--log-no-colors")
	orphan.Dir = f.dir
	tree, err := proctree.Start(orphan)
	require.NoError(t, err)
	t.Cleanup(tree.Close)
	go func() { _ = orphan.Wait() }()
	require.Eventually(t, func() bool { return answers(f.hub) }, 10*time.Second, 50*time.Millisecond)

	f.m.Start()
	require.NoError(t, f.m.Ensure(context.Background(), f.hub))

	spawns := f.spawns(t)
	require.Len(t, spawns, 2, "the leftover was adopted instead of replaced")
	assert.Eventually(t, func() bool { return !processAlive(orphan.Process.Pid) }, 5*time.Second, 50*time.Millisecond)
	assert.True(t, answers(f.hub))
}

func TestOnlyALoopbackHTTPAddressIsServed(t *testing.T) {
	cases := []struct {
		url  string
		args []string
	}{
		{"http://127.0.0.1:4723", []string{"--address", "127.0.0.1", "--port", "4723", "--log-no-colors"}},
		{"http://localhost:4724/wd/hub/", []string{"--address", "localhost", "--port", "4724", "--base-path", "/wd/hub", "--log-no-colors"}},
		{"http://[::1]:4725", []string{"--address", "::1", "--port", "4725", "--log-no-colors"}},
		{"http://10.0.0.9:4723", nil},
		{"https://127.0.0.1:4723", nil},
		{"", nil},
	}
	for _, c := range cases {
		m := New(Config{Bin: "/usr/local/bin/appium", HubURL: c.url})
		assert.Equal(t, c.args != nil, m.Manages(c.url), c.url)
		assert.Equal(t, c.args, m.args, c.url)
		m.Close()
	}

	m := New(Config{Bin: "/usr/local/bin/appium", HubURL: "http://127.0.0.1:4723"})
	defer m.Close()
	assert.True(t, m.Manages("http://127.0.0.1:4723/"))
	assert.False(t, m.Manages("http://127.0.0.1:4724"))
}
