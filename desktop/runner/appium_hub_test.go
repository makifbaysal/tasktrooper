package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as the fake appium: TestMain hands it to fakeAppium
// when it is started with Appium's own first flag, and it serves /status on the
// address it was given. What it should do instead — fail, or take a while — is
// read from files in the directory fakeAppiumDirEnv names, which is how each
// test steers its own hub. The env var reaches it because the hub inherits this
// process's environment.
const fakeAppiumDirEnv = "TT_FAKE_APPIUM_DIR"

func fakeAppium(args []string) int {
	flags := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			flags[args[i]] = args[i+1]
		}
	}
	dir := os.Getenv(fakeAppiumDirEnv)
	appendTo(filepath.Join(dir, "spawns.log"), strconv.Itoa(os.Getpid())+" "+strings.Join(args, " "))
	appendTo(filepath.Join(dir, "env.log"), "ANDROID_HOME="+os.Getenv("ANDROID_HOME")+
		" ANDROID_SDK_ROOT="+os.Getenv("ANDROID_SDK_ROOT")+" GITHUB_TOKEN="+os.Getenv("GITHUB_TOKEN")+
		" PATH_SET="+strconv.FormatBool(os.Getenv("PATH") != ""))
	if msg, err := os.ReadFile(filepath.Join(dir, "fail")); err == nil {
		fmt.Println("[Appium] Welcome to the fake Appium")
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(msg)))
		return 1
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "delay")); err == nil {
		d, _ := time.ParseDuration(strings.TrimSpace(string(raw)))
		time.Sleep(d)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(flags["--address"], flags["--port"]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	mux := http.NewServeMux()
	mux.HandleFunc(flags["--base-path"]+"/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true}}`))
	})
	_ = http.Serve(ln, mux)
	return 0
}

func appendTo(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line + "\n")
}

func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

type hubFixture struct {
	hub  *appiumHub
	dir  string
	base string
	cfg  config
}

func newHubFixture(t *testing.T, tweak func(*hubTimings)) hubFixture {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(fakeAppiumDirEnv, dir)
	timings := hubTimings{
		idleAfter:    time.Minute,
		readyTimeout: 15 * time.Second,
		reapEvery:    50 * time.Millisecond,
		cooldown:     time.Minute,
		stopGrace:    2 * time.Second,
	}
	if tweak != nil {
		tweak(&timings)
	}
	cfg := config{appiumBin: os.Args[0], appiumBaseURL: "http://127.0.0.1:" + freeLoopbackPort(t)}
	h := testHub(t, cfg, timings, dir)
	return hubFixture{hub: h, dir: dir, base: cfg.appiumBaseURL, cfg: cfg}
}

// testHub is newAppiumHub with its record in dir rather than the user's own
// cache directory.
func testHub(t *testing.T, cfg config, timings hubTimings, dir string) *appiumHub {
	t.Helper()
	h := newAppiumHub(cfg, timings)
	if h == nil {
		t.Fatal("no hub manager for a config naming appium_bin")
	}
	if h.recordPath != "" {
		h.recordPath = filepath.Join(dir, "appium-hub.json")
	}
	t.Cleanup(h.close)
	return h
}

func (f hubFixture) spawns(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "spawns.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (f hubFixture) state() *state {
	st := newState()
	st.hub = f.hub
	return st
}

func (f hubFixture) set(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hubAnswers(base string) bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(base + "/status")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

func eventually(t *testing.T, within time.Duration, cond func() bool, why string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(why)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func mustEnsure(t *testing.T, h *appiumHub) {
	t.Helper()
	if err := h.ensure(context.Background()); err != nil {
		t.Fatalf("ensure: %s: %s", err.Code, err.Message)
	}
}

func TestWithoutAppiumBinTheRunnerStartsNoHub(t *testing.T) {
	if h := newAppiumHub(config{appiumBaseURL: "http://127.0.0.1:4723"}, defaultHubTimings); h != nil {
		t.Fatal("a hub manager exists for a config with no appium_bin")
	}
	var none *appiumHub
	if err := none.ensure(context.Background()); err != nil {
		t.Fatalf("ensure on no manager = %v, want nil: the hub is whoever's runs it", err)
	}
	none.hold()()
	none.close()
}

func TestEnsureStartsTheHubOnTheConfiguredLoopbackAddress(t *testing.T) {
	f := newHubFixture(t, nil)

	mustEnsure(t, f.hub)

	if !hubAnswers(f.base) {
		t.Fatal("ensure returned and nothing answers /status")
	}
	spawns := f.spawns(t)
	if len(spawns) != 1 {
		t.Fatalf("spawns = %v, want exactly one", spawns)
	}
	port := strings.TrimPrefix(f.base, "http://127.0.0.1:")
	if !strings.HasSuffix(spawns[0], "--address 127.0.0.1 --port "+port+" --log-no-colors") {
		t.Fatalf("argv = %q, want the hub bound to loopback on the configured port", spawns[0])
	}

	mustEnsure(t, f.hub)
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("a running hub was started again (%d spawns)", got)
	}
}

func TestEnsureAdoptsAHubThatAlreadyAnswersAndNeverStopsIt(t *testing.T) {
	theirs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"value":{"ready":true}}`))
	}))
	t.Cleanup(theirs.Close)
	dir := t.TempDir()
	t.Setenv(fakeAppiumDirEnv, dir)
	h := testHub(t, config{appiumBin: os.Args[0], appiumBaseURL: theirs.URL}, hubTimings{
		idleAfter: 50 * time.Millisecond, readyTimeout: 5 * time.Second, reapEvery: 20 * time.Millisecond,
		cooldown: time.Minute, stopGrace: time.Second,
	}, dir)
	f := hubFixture{hub: h, dir: dir, base: theirs.URL}

	mustEnsure(t, h)
	time.Sleep(300 * time.Millisecond)
	h.close()

	if spawns := f.spawns(t); len(spawns) != 0 {
		t.Fatalf("a hub was answering and one was started anyway: %v", spawns)
	}
	if !hubAnswers(theirs.URL) {
		t.Fatal("the member's own hub did not survive the idle stop and the shutdown")
	}
}

func TestConcurrentCallersShareOneStart(t *testing.T) {
	f := newHubFixture(t, nil)
	f.set(t, "delay", "400ms")

	var wg sync.WaitGroup
	errs := make([]*rpcError, 8)
	for i := range errs {
		wg.Go(func() { errs[i] = f.hub.ensure(context.Background()) })
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %s", i, err.Message)
		}
	}
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d hubs were started for eight concurrent callers, want one", got)
	}
}

func TestAWaitingCallerCanGiveUpWithoutCancellingTheStart(t *testing.T) {
	f := newHubFixture(t, nil)
	f.set(t, "delay", "400ms")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.hub.ensure(ctx); err == nil || err.Code != codeCancelled {
		t.Fatalf("ensure under a short deadline = %v, want cancelled", err)
	}

	mustEnsure(t, f.hub)
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d spawns; the caller giving up cancelled the start it was waiting on", got)
	}
}

func TestAFailedStartSaysWhyAndIsNotRetriedDuringTheCooldown(t *testing.T) {
	f := newHubFixture(t, func(ht *hubTimings) { ht.cooldown = 600 * time.Millisecond })
	f.set(t, "fail", "Could not find a driver for automationName 'XCUITest'")

	err := f.hub.ensure(context.Background())
	if err == nil {
		t.Fatal("a hub that exits at once was reported started")
	}
	if err.Code != codeUpstream {
		t.Fatalf("code = %q, want %q: a broken install is a tool error, not a park", err.Code, codeUpstream)
	}
	if !strings.Contains(err.Message, "Could not find a driver for automationName") {
		t.Fatalf("message = %q, want Appium's own last lines in it", err.Message)
	}

	again := f.hub.ensure(context.Background())
	if again != err {
		t.Fatalf("second ensure = %v, want the first failure handed back during the cooldown", again)
	}
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d spawns during the cooldown, want one", got)
	}

	if err := os.Remove(filepath.Join(f.dir, "fail")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	mustEnsure(t, f.hub)
	if got := len(f.spawns(t)); got != 2 {
		t.Fatalf("%d spawns after the cooldown, want a second attempt", got)
	}
}

func TestAnIdleHubThisRunnerStartedIsStoppedAndStartedAgainOnDemand(t *testing.T) {
	f := newHubFixture(t, func(ht *hubTimings) { ht.idleAfter = 300 * time.Millisecond })

	mustEnsure(t, f.hub)
	pid, err := strconv.Atoi(strings.Fields(f.spawns(t)[0])[0])
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool { return !hubAnswers(f.base) }, "the hub outlived its idle time")
	eventually(t, 5*time.Second, func() bool { return !processAlive(pid) }, "the idle stop left the hub process running")

	mustEnsure(t, f.hub)
	if !hubAnswers(f.base) {
		t.Fatal("the next call after an idle stop did not bring the hub back")
	}
	if got := len(f.spawns(t)); got != 2 {
		t.Fatalf("%d spawns, want two", got)
	}
}

func TestACallInFlightKeepsTheHubPastItsIdleTime(t *testing.T) {
	f := newHubFixture(t, func(ht *hubTimings) { ht.idleAfter = 150 * time.Millisecond })

	release := f.hub.hold()
	mustEnsure(t, f.hub)
	time.Sleep(700 * time.Millisecond)
	if !hubAnswers(f.base) {
		t.Fatal("the hub was stopped under a call still in flight")
	}

	release()
	eventually(t, 10*time.Second, func() bool { return !hubAnswers(f.base) }, "the hub outlived its idle time once the call ended")
}

func TestCloseStopsTheHubItStarted(t *testing.T) {
	f := newHubFixture(t, nil)
	mustEnsure(t, f.hub)
	pid, err := strconv.Atoi(strings.Fields(f.spawns(t)[0])[0])
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(f.hub.close)
	}
	wg.Wait()

	if hubAnswers(f.base) {
		t.Fatal("the hub still answers after close")
	}
	if processAlive(pid) {
		t.Fatal("the hub process outlived close")
	}
	if err := f.hub.ensure(context.Background()); err == nil {
		t.Fatal("a closed manager started a hub")
	}
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d spawns, want one", got)
	}
}

func TestTheHubGetsTheSDKRootAndNoCredential(t *testing.T) {
	sdk := filepath.Join(t.TempDir(), "sdk")
	t.Setenv("GITHUB_TOKEN", "ghp_must_not_reach_appium")
	f := newHubFixture(t, nil)
	f.hub.environ = func() []string { return hubEnviron(os.Environ(), filepath.Join(sdk, "platform-tools", "adb")) }

	mustEnsure(t, f.hub)

	raw, err := os.ReadFile(filepath.Join(f.dir, "env.log"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(raw))
	for _, want := range []string{"ANDROID_HOME=" + sdk + " ", "ANDROID_SDK_ROOT=" + sdk + " ", "GITHUB_TOKEN= ", "PATH_SET=true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the hub's environment %q lacks %q", got, want)
		}
	}
}

func TestAppiumHubArgs(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{"http://127.0.0.1:4723", "--address 127.0.0.1 --port 4723 --log-no-colors"},
		{"http://localhost:4724/wd/hub/", "--address localhost --port 4724 --base-path /wd/hub --log-no-colors"},
		{"http://[::1]:4725", "--address ::1 --port 4725 --log-no-colors"},
	}
	for _, tc := range cases {
		args, err := appiumHubArgs(tc.base)
		if err != nil {
			t.Fatalf("%s: %v", tc.base, err)
		}
		if got := strings.Join(args, " "); got != tc.want {
			t.Fatalf("%s: argv %q, want %q", tc.base, got, tc.want)
		}
	}
	if _, err := appiumHubArgs("https://127.0.0.1:4723"); err == nil {
		t.Fatal("an https base was accepted for a hub this runner starts on plain http")
	}
}

func TestLoadConfigAppiumBin(t *testing.T) {
	cfg, err := loadConfig([]byte(configWith(t, map[string]any{
		"appium_base_url": "http://127.0.0.1:4723/",
		"appium_bin":      "/opt/homebrew/bin/appium",
	})))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.appiumBin != "/opt/homebrew/bin/appium" || cfg.appiumBaseURL != "http://127.0.0.1:4723" {
		t.Fatalf("appium = %q at %q", cfg.appiumBin, cfg.appiumBaseURL)
	}

	for name, changes := range map[string]map[string]any{
		"relative": {"appium_base_url": "http://127.0.0.1:4723", "appium_bin": "appium"},
		"no base":  {"appium_base_url": nil, "appium_bin": "/opt/homebrew/bin/appium"},
		"https":    {"appium_base_url": "https://127.0.0.1:4723", "appium_bin": "/opt/homebrew/bin/appium"},
	} {
		if _, err := loadConfig([]byte(configWith(t, changes))); err == nil {
			t.Fatalf("%s: loadConfig accepted %v", name, changes)
		}
	}
}

// --- through the runner's own surface ---------------------------------------

func TestTheProxyStartsTheHubBeforeForwarding(t *testing.T) {
	f := newHubFixture(t, nil)

	res := request(t, f.cfg, f.state(), http.MethodGet, appiumPrefix+"/status", "")

	if res.status != http.StatusOK || !strings.Contains(res.body, `"ready":true`) {
		t.Fatalf("status %d body %q, want the started hub's own answer", res.status, res.body)
	}
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d spawns, want one", got)
	}
}

func TestTheProxyAnswersAHubThatWillNotStartWithItsReason(t *testing.T) {
	f := newHubFixture(t, nil)
	f.set(t, "fail", "Error: listen EADDRINUSE")

	res := request(t, f.cfg, f.state(), http.MethodPost, appiumPrefix+"/session", `{}`)

	if res.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.status)
	}
	if !strings.Contains(res.body, `"code":"upstream"`) || !strings.Contains(res.body, "EADDRINUSE") {
		t.Fatalf("body = %q, want upstream with Appium's own words", res.body)
	}
}

func TestTheDevicesProbeNeverStartsTheHub(t *testing.T) {
	f := newHubFixture(t, nil)
	c := &call{ctx: t.Context(), id: "devices", cfg: f.cfg, state: f.state(), params: []byte(`{}`)}

	raw, callErr := mobileDevices(c)
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	appium := raw.(mobileDevicesResult).Capabilities.Appium

	if spawns := f.spawns(t); len(spawns) != 0 {
		t.Fatalf("the probe started a hub: %v", spawns)
	}
	if !appium.Configured || !appium.Reachable || !appium.OnDemand {
		t.Fatalf("appium = %+v; a stopped on-demand hub read as down would park a task on a hub only its own run starts", appium)
	}

	f.set(t, "fail", "appium: command not found")
	_ = f.hub.ensure(context.Background())
	raw, callErr = mobileDevices(c)
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	appium = raw.(mobileDevicesResult).Capabilities.Appium
	if appium.Reachable || !strings.Contains(appium.Detail, "command not found") {
		t.Fatalf("appium = %+v after a failed start, want unreachable with the reason", appium)
	}
	if got := len(f.spawns(t)); got != 1 {
		t.Fatalf("%d spawns, want only the one ensure made", got)
	}
}

func TestTheDevicesProbeSaysARunningOnDemandHubIsOnDemand(t *testing.T) {
	f := newHubFixture(t, nil)
	mustEnsure(t, f.hub)
	c := &call{ctx: t.Context(), id: "devices", cfg: f.cfg, state: f.state(), params: []byte(`{}`)}

	raw, callErr := mobileDevices(c)
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	if appium := raw.(mobileDevicesResult).Capabilities.Appium; !appium.Reachable || !appium.OnDemand || appium.Detail != "" {
		t.Fatalf("appium = %+v, want a reachable on-demand hub with nothing to explain", appium)
	}
}
