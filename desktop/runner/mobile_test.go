//go:build !windows

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The mobile methods, against real processes and a real hub.
//
// The fakes here are shell scripts and an `httptest` server rather than
// interfaces, for the same reason the rest of this suite works that way: what
// is under test is which argv is built, what the tool's own output parses into,
// and what a caller reads off a socket. A mock would assert that a function was
// called with the arguments the test itself supplied.
//
// Three properties are worth the trouble, and each has a failure mode that
// would be invisible on a passing unit test:
//
//  1. **The udid is not what the caller asked for.** An AVD is a name; the
//     serial it comes up on is allocated at boot. A caller that guessed
//     `emulator-5554` would drive whichever emulator is on that console port.
//  2. **The proxy does not serialise.** Appium's refusal of a second session
//     against one device is the lease, and it is only a lease if both requests
//     actually reach the hub. A mutex here would look correct and would silently
//     turn a cross-process lock into a per-Mac one.
//  3. **`emu kill` is only ever aimed at an emulator.** A phone plugged into
//     somebody's laptop belongs to the bridge's path and must be untouchable
//     from here.

// --- fake toolchain ---------------------------------------------------------

// fakeMobileHost writes stand-ins for xcrun, adb and emulator into one
// directory and returns a config that points at them.
//
// The three scripts share a STATE DIRECTORY and change each other's answers
// through it, which is what makes a boot testable end to end: `emulator -avd`
// writes a file, and the next `adb devices` reports a device because of it.
// Scripts that each answered from a constant would prove only that the parser
// works on a string the test wrote.
type fakeMobileHost struct {
	cfg   config
	state string
	// log is every argv, one invocation per line, in order.
	log string
}

func newFakeMobileHost(t *testing.T) *fakeMobileHost {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatalf("creating the state directory: %v", err)
	}
	logFile := filepath.Join(dir, "argv.log")

	host := &fakeMobileHost{state: state, log: logFile}
	host.cfg = config{
		workspaceDir: dir,
		xcrunBin:     host.write(t, dir, "xcrun", fakeXcrun),
		adbBin:       host.write(t, dir, "adb", fakeADB),
		emulatorBin:  host.write(t, dir, "emulator", fakeEmulator),
	}
	return host
}

func (h *fakeMobileHost) write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" +
		"STATE=" + h.state + "\n" +
		"echo \"" + name + " $*\" >> " + h.log + "\n" +
		body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake %s: %v", name, err)
	}
	return path
}

// invocations is every argv the fakes saw, in order.
func (h *fakeMobileHost) invocations(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(h.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the argv log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func (h *fakeMobileHost) sawCommand(t *testing.T, want string) bool {
	t.Helper()
	for _, line := range h.invocations(t) {
		if line == want {
			return true
		}
	}
	return false
}

const simIPhone = "11111111-2222-3333-4444-555555555555"
const simIPad = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"

// fakeXcrun answers simctl's three subcommands. The iPhone's state is read from
// the state directory, so a boot in one call is visible to the list in the next.
const fakeXcrun = `
case "$*" in
  "simctl list devices --json")
    IPHONE_STATE=Shutdown
    [ -f "$STATE/sim-booted" ] && IPHONE_STATE=Booted
    cat <<JSON
{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-17-4":[
 {"udid":"11111111-2222-3333-4444-555555555555","name":"iPhone 15","state":"$IPHONE_STATE","isAvailable":true},
 {"udid":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","name":"iPad Air","state":"Booted","isAvailable":true},
 {"udid":"99999999-9999-9999-9999-999999999999","name":"Retired","state":"Shutdown","isAvailable":false}]}}
JSON
    ;;
  "simctl boot "*) touch "$STATE/sim-booted" ;;
  "simctl bootstatus AAAAAAAA-"*) ;;
  "simctl bootstatus "*) [ -f "$STATE/sim-booted" ] || { echo "not booted" >&2; exit 1; } ;;
  "simctl shutdown "*) rm -f "$STATE/sim-booted" ;;
  *) echo "unexpected simctl: $*" >&2; exit 64 ;;
esac
exit 0
`

// fakeADB reports an emulator only once the fake emulator has been started, and
// always reports a physical phone — which is what makes "never kill a phone"
// something this suite can actually check.
const fakeADB = `
case "$*" in
  "devices")
    echo "List of devices attached"
    echo "R58M12345XY	device"
    [ -f "$STATE/avd-running" ] && echo "emulator-5560	device"
    exit 0 ;;
  "-s emulator-5560 emu avd name")
    echo Pixel_7_API_34
    echo OK
    exit 0 ;;
  "-s emulator-5560 shell getprop sys.boot_completed")
    [ -f "$STATE/avd-booted" ] && echo 1 || echo ""
    exit 0 ;;
  "-s emulator-5560 shell getprop ro.build.version.release")
    echo 14
    exit 0 ;;
  "-s emulator-5560 emu kill")
    rm -f "$STATE/avd-running" "$STATE/avd-booted"
    echo OK
    exit 0 ;;
esac
echo "unexpected adb: $*" >&2
exit 64
`

// fakeEmulator lists one AVD and, when started, makes it appear to adb — after
// a beat, so the boot wait is a wait rather than a formality.
const fakeEmulator = `
case "$*" in
  "-list-avds")
    echo "INFO    | storing crashdata in: /tmp/x"
    echo Pixel_7_API_34
    exit 0 ;;
  "-avd Pixel_7_API_34 -no-window -no-audio")
    ( sleep 0.2; touch "$STATE/avd-running"; sleep 0.2; touch "$STATE/avd-booted" ) &
    exit 0 ;;
esac
echo "unexpected emulator: $*" >&2
exit 64
`

func mobileCall(t *testing.T, cfg config, params string) *call {
	t.Helper()
	return &call{ctx: t.Context(), id: "mobile-test", cfg: cfg, params: json.RawMessage(params)}
}

// --- the inventory ----------------------------------------------------------

func TestMobileDevicesListsWhatThisMacHas(t *testing.T) {
	host := newFakeMobileHost(t)
	raw, callErr := mobileDevices(mobileCall(t, host.cfg, `{}`))
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	got := raw.(mobileDevicesResult)

	if len(got.IOS) != 2 {
		t.Fatalf("ios = %+v, want the two AVAILABLE simulators (the unavailable one is a device whose runtime was uninstalled)", got.IOS)
	}
	// Sorted by runtime then name, because simctl's own JSON is a map and Go
	// iterates one at random: a list that reshuffles on every load is one nobody
	// can point at.
	if got.IOS[0].Name != "iPad Air" || got.IOS[1].Name != "iPhone 15" {
		t.Fatalf("ios = %+v, want them sorted by runtime then name", got.IOS)
	}
	iphone := got.IOS[1]
	if iphone.UDID != simIPhone {
		t.Fatalf("the iPhone's udid = %q, want %q", iphone.UDID, simIPhone)
	}
	// The runtime identifier is unreadable and the version is what Appium's
	// capability wants; both are reported so nothing downstream has to parse
	// "com.apple.CoreSimulator.SimRuntime.iOS-17-4".
	if iphone.Runtime != "iOS 17.4" || iphone.PlatformVersion != "17.4" {
		t.Fatalf("runtime/version = %q/%q, want %q/%q",
			iphone.Runtime, iphone.PlatformVersion, "iOS 17.4", "17.4")
	}
	if iphone.State != "Shutdown" {
		t.Fatalf("state = %q, want simctl's own word", iphone.State)
	}
	if len(got.Android) != 1 || got.Android[0] != "Pixel_7_API_34" {
		t.Fatalf("android = %v, want just the AVD (the emulator binary's INFO line is not a device)", got.Android)
	}
	// Nothing is running, and a physical phone is attached. Neither is an
	// emulator this Mac may drive.
	if len(got.AndroidRunning) != 0 {
		t.Fatalf("android_running = %+v, want none — the only attached device is a physical phone", got.AndroidRunning)
	}
	if !got.Capabilities.IOSSimulators.Available || !got.Capabilities.AndroidEmulators.Available {
		t.Fatalf("capabilities = %+v, want both available on a Mac with the whole toolchain", got.Capabilities)
	}
}

// A Mac with half a toolchain says which half and why. `available:false` with no
// sentence is an answer nobody can act on, and it is the answer that sends
// somebody looking in the wrong place.
func TestMobileDevicesSaysWhichHalfIsMissingAndWhy(t *testing.T) {
	host := newFakeMobileHost(t)
	cfg := host.cfg
	cfg.xcrunBin = ""
	cfg.adbBin = ""

	raw, callErr := mobileDevices(mobileCall(t, cfg, `{}`))
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	got := raw.(mobileDevicesResult)

	for name, capability := range map[string]mobileCapability{
		"ios_simulators":    got.Capabilities.IOSSimulators,
		"android_emulators": got.Capabilities.AndroidEmulators,
	} {
		if capability.Available {
			t.Fatalf("%s reported available on a Mac with no toolchain for it", name)
		}
		if capability.Detail == "" {
			t.Fatalf("%s is unavailable and says nothing about why; a caller can only render \"unavailable\"", name)
		}
	}
	// Empty, and non-nil: "asked and has none" is not the same statement as
	// "did not answer", and only one of them survives a JSON round trip as [].
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	for _, field := range []string{`"ios":[]`, `"android":[]`, `"android_running":[]`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("the answer does not carry %s; a null there reads as \"this Mac was never asked\"", field)
		}
	}
}

// adb without the `emulator` binary is a real and usable state: somebody
// started an AVD from Android Studio and it can be driven perfectly well. What
// this Mac cannot do is START one, and the answer has to say that rather than
// claiming Android is unavailable.
func TestAMacWithADBAndNoEmulatorBinaryCanStillDriveARunningAVD(t *testing.T) {
	host := newFakeMobileHost(t)
	if err := os.WriteFile(filepath.Join(host.state, "avd-running"), nil, 0o644); err != nil {
		t.Fatalf("marking the AVD running: %v", err)
	}
	cfg := host.cfg
	cfg.emulatorBin = ""

	raw, callErr := mobileDevices(mobileCall(t, cfg, `{}`))
	if callErr != nil {
		t.Fatalf("mobile.devices: %s", callErr.Message)
	}
	got := raw.(mobileDevicesResult)
	if !got.Capabilities.AndroidEmulators.Available {
		t.Fatalf("android reported unavailable while an AVD is running and adb can reach it: %+v",
			got.Capabilities.AndroidEmulators)
	}
	if !strings.Contains(got.Capabilities.AndroidEmulators.Detail, "cannot start one") {
		t.Fatalf("detail = %q, want it to say this Mac cannot start an AVD", got.Capabilities.AndroidEmulators.Detail)
	}
	if len(got.AndroidRunning) != 1 || got.AndroidRunning[0].Serial != "emulator-5560" {
		t.Fatalf("android_running = %+v, want the running AVD with its serial", got.AndroidRunning)
	}
}

// --- the grammars -----------------------------------------------------------

// Everything a caller names here becomes argv for `xcrun`, `adb` or
// `emulator`, and all three parse their own arguments. An operand that can
// become a flag is a command.
func TestMobileRefusesEveryIDThatCouldBecomeAFlag(t *testing.T) {
	host := newFakeMobileHost(t)

	bad := []struct {
		name, params string
	}{
		{"a leading dash on an AVD", `{"kind":"android_emulator","id":"-no-snapshot-load"}`},
		{"a flag with a value", `{"kind":"android_emulator","id":"--ports=5554"}`},
		{"a path", `{"kind":"android_emulator","id":"../../etc"}`},
		{"a space", `{"kind":"android_emulator","id":"Pixel 7"}`},
		{"a comma", `{"kind":"android_emulator","id":"a,b"}`},
		{"an AVD name where a UDID belongs", `{"kind":"ios_simulator","id":"Pixel_7_API_34"}`},
		{"a truncated UDID", `{"kind":"ios_simulator","id":"11111111-2222-3333-4444"}`},
		{"a leading dash on a UDID", `{"kind":"ios_simulator","id":"-1111111-2222-3333-4444-555555555555"}`},
		{"no id at all", `{"kind":"ios_simulator"}`},
		{"no kind at all", `{"id":"` + simIPhone + `"}`},
		{"a kind nobody has", `{"kind":"ios_device","id":"` + simIPhone + `"}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			for method, fn := range map[string]func(*call) (any, *rpcError){
				"mobile.boot":     mobileBoot,
				"mobile.shutdown": mobileShutdown,
			} {
				if _, err := fn(mobileCall(t, host.cfg, tc.params)); err == nil {
					t.Fatalf("%s accepted %s", method, tc.params)
				} else if err.Code != codeBadRequest {
					t.Fatalf("%s answered %q for %s, want %q", method, err.Code, tc.params, codeBadRequest)
				}
			}
		})
	}
	// Nothing reached a binary. A refusal that happens after the exec is not a
	// refusal.
	if got := host.invocations(t); len(got) != 0 {
		t.Fatalf("a refused request still ran something: %v", got)
	}
}

// A physical phone is the bridge's path, and it is refused by NAME rather than
// as an unknown value — a message reading "unknown kind" sends somebody hunting
// for a typo in a kind that is perfectly real.
func TestAPhysicalPhoneIsRefusedByNameAndNotAsATypo(t *testing.T) {
	host := newFakeMobileHost(t)
	_, err := mobileBoot(mobileCall(t, host.cfg, `{"kind":"remote_adb","id":"100.64.1.5:5555"}`))
	if err == nil {
		t.Fatal("mobile.boot accepted a physical phone; that device is not attached to this Mac")
	}
	if !strings.Contains(err.Message, "bridge") {
		t.Fatalf("message = %q, want it to name the bridge that does handle this kind", err.Message)
	}
}

// A Mac without the toolchain refuses the call rather than exec'ing "" and
// failing with a message that names nothing.
func TestBootingWithoutTheToolchainIsNotReadyRatherThanAFailedExec(t *testing.T) {
	cfg := config{workspaceDir: t.TempDir()}
	for _, params := range []string{
		`{"kind":"ios_simulator","id":"` + simIPhone + `"}`,
		`{"kind":"android_emulator","id":"Pixel_7_API_34"}`,
	} {
		_, err := mobileBoot(mobileCall(t, cfg, params))
		if err == nil {
			t.Fatalf("mobile.boot accepted %s on a Mac with no toolchain", params)
		}
		if err.Code != codeNotReady {
			t.Fatalf("code = %q for %s, want %q", err.Code, params, codeNotReady)
		}
	}
}

// --- booting ----------------------------------------------------------------

// The property this method exists for: what Appium is handed is NOT what the
// caller asked for. An AVD is a name and the serial is allocated at boot.
func TestBootingAnEmulatorAnswersWithTheSerialItCameUpOn(t *testing.T) {
	host := newFakeMobileHost(t)

	raw, callErr := mobileBoot(mobileCall(t, host.cfg, `{"kind":"android_emulator","id":"Pixel_7_API_34","timeout_ms":20000}`))
	if callErr != nil {
		t.Fatalf("mobile.boot: %s", callErr.Message)
	}
	got := raw.(mobileBootResult)

	if got.UDID != "emulator-5560" {
		t.Fatalf("udid = %q, want the serial adb allocated — a caller that guessed emulator-5554 would drive a different device", got.UDID)
	}
	if got.ID != "Pixel_7_API_34" {
		t.Fatalf("id = %q, want the AVD name echoed back", got.ID)
	}
	if got.AlreadyBooted {
		t.Fatal("already_booted on an AVD that was not running")
	}
	if got.PlatformVersion != "14" {
		t.Fatalf("platform_version = %q, want the release adb reported", got.PlatformVersion)
	}
	if !host.sawCommand(t, "emulator -avd Pixel_7_API_34 -no-window -no-audio") {
		t.Fatalf("the emulator was not started headless; argv were %v", host.invocations(t))
	}
	// adb answers while Android is still on the boot animation, so the boot is
	// only over when the system says so.
	if !host.sawCommand(t, "adb -s emulator-5560 shell getprop sys.boot_completed") {
		t.Fatalf("nothing waited for the system to finish booting; argv were %v", host.invocations(t))
	}
}

// Idempotent, which is what makes it safe at the start of every run: a device
// that is already up is a success that says so, not a conflict.
func TestBootingADeviceThatIsUpIsASuccessThatSaysSo(t *testing.T) {
	host := newFakeMobileHost(t)

	raw, callErr := mobileBoot(mobileCall(t, host.cfg, `{"kind":"ios_simulator","id":"`+simIPad+`"}`))
	if callErr != nil {
		t.Fatalf("mobile.boot: %s", callErr.Message)
	}
	got := raw.(mobileBootResult)
	if !got.AlreadyBooted {
		t.Fatal("already_booted is false for a simulator simctl reports as Booted")
	}
	if got.UDID != simIPad || got.PlatformVersion != "17.4" {
		t.Fatalf("result = %+v, want the iPad's own udid and version", got)
	}
	if host.sawCommand(t, "xcrun simctl boot "+simIPad) {
		t.Fatal("a booted simulator was booted again")
	}
}

// A boot is what precedes a session, so it brings the hub up too — and a hub
// that will not start fails the boot with its reason, while saying the device
// itself is up, so the retry is a cheap already_booted.
func TestBootingADeviceStartsTheHubBesideIt(t *testing.T) {
	host := newFakeMobileHost(t)
	boot := func(f hubFixture) (any, *rpcError) {
		cfg := host.cfg
		cfg.appiumBin, cfg.appiumBaseURL = f.cfg.appiumBin, f.cfg.appiumBaseURL
		return mobileBoot(&call{ctx: t.Context(), id: "boot", cfg: cfg, state: f.state(),
			params: json.RawMessage(`{"kind":"ios_simulator","id":"` + simIPad + `"}`)})
	}

	working := newHubFixture(t, nil)
	if _, callErr := boot(working); callErr != nil {
		t.Fatalf("mobile.boot: %s", callErr.Message)
	}
	if !hubAnswers(working.base) || len(working.spawns(t)) != 1 {
		t.Fatalf("after a boot the hub answers=%v with %d spawns, want one running hub",
			hubAnswers(working.base), len(working.spawns(t)))
	}
	working.hub.close()

	broken := newHubFixture(t, nil)
	broken.set(t, "fail", "Could not find a driver for automationName 'XCUITest'")
	_, callErr := boot(broken)
	if callErr == nil || callErr.Code != codeUpstream {
		t.Fatalf("mobile.boot with a hub that will not start = %+v, want upstream", callErr)
	}
	if !strings.Contains(callErr.Message, simIPad+" is up") || !strings.Contains(callErr.Message, "XCUITest") {
		t.Fatalf("message = %q, want the device named as up and Appium's own reason", callErr.Message)
	}
}

// Booting a simulator waits for it to be USABLE. `simctl boot` returns as soon
// as the boot has been started, and a session created against a half-booted
// device fails in ways that read to an agent as a broken app.
func TestBootingASimulatorWaitsForBootstatus(t *testing.T) {
	host := newFakeMobileHost(t)

	if _, callErr := mobileBoot(mobileCall(t, host.cfg, `{"kind":"ios_simulator","id":"`+simIPhone+`"}`)); callErr != nil {
		t.Fatalf("mobile.boot: %s", callErr.Message)
	}
	if !host.sawCommand(t, "xcrun simctl boot "+simIPhone) {
		t.Fatalf("the simulator was never booted; argv were %v", host.invocations(t))
	}
	if !host.sawCommand(t, "xcrun simctl bootstatus "+simIPhone+" -b") {
		t.Fatalf("nothing waited for the boot to finish; argv were %v", host.invocations(t))
	}
}

func TestBootingADeviceThisMacDoesNotHaveIsABadRequest(t *testing.T) {
	host := newFakeMobileHost(t)
	for _, params := range []string{
		`{"kind":"ios_simulator","id":"22222222-2222-2222-2222-222222222222"}`,
		`{"kind":"android_emulator","id":"Pixel_9_API_99"}`,
	} {
		_, err := mobileBoot(mobileCall(t, host.cfg, params))
		if err == nil {
			t.Fatalf("mobile.boot accepted %s, which this Mac does not have", params)
		}
		if err.Code != codeBadRequest {
			t.Fatalf("code = %q for %s, want %q", err.Code, params, codeBadRequest)
		}
	}
}

// --- shutting down ----------------------------------------------------------

// `emu kill` ends a process. The one it must never end is a phone somebody
// plugged into their laptop, and the fake adb reports one on every call.
func TestShutdownNeverAimsAtAPhysicalDevice(t *testing.T) {
	host := newFakeMobileHost(t)

	raw, callErr := mobileShutdown(mobileCall(t, host.cfg, `{"kind":"android_emulator","id":"Pixel_7_API_34"}`))
	if callErr != nil {
		t.Fatalf("mobile.shutdown: %s", callErr.Message)
	}
	if got := raw.(mobileShutdownResult); got.WasRunning {
		t.Fatal("was_running is true for an AVD that is not up")
	}
	for _, line := range host.invocations(t) {
		if strings.Contains(line, "emu kill") {
			t.Fatalf("a shutdown for a stopped AVD ran %q; the only attached device is a physical phone", line)
		}
		if strings.Contains(line, "R58M12345XY") {
			t.Fatalf("a physical device's serial reached argv: %q", line)
		}
	}
}

// The other half of the same rule, and the one that is easy to delete by
// accident because the guard downstream hides its absence: the serial list
// itself contains only emulators. A physical phone's serial must not become a
// candidate at all, however carefully the next function checks.
func TestTheSerialListContainsOnlyEmulators(t *testing.T) {
	host := newFakeMobileHost(t)
	if err := os.WriteFile(filepath.Join(host.state, "avd-running"), nil, 0o644); err != nil {
		t.Fatalf("marking the AVD running: %v", err)
	}
	got, callErr := adbEmulatorSerials(mobileCall(t, host.cfg, `{}`))
	if callErr != nil {
		t.Fatalf("listing serials: %s", callErr.Message)
	}
	if len(got) != 1 || got[0] != "emulator-5560" {
		t.Fatalf("serials = %v, want only the emulator — adb also reports the physical phone R58M12345XY, "+
			"and a phone plugged into somebody's laptop belongs to the bridge's path", got)
	}
}

func TestShuttingDownARunningEmulatorKillsItByItsOwnSerial(t *testing.T) {
	host := newFakeMobileHost(t)
	if _, callErr := mobileBoot(mobileCall(t, host.cfg, `{"kind":"android_emulator","id":"Pixel_7_API_34","timeout_ms":20000}`)); callErr != nil {
		t.Fatalf("mobile.boot: %s", callErr.Message)
	}

	raw, callErr := mobileShutdown(mobileCall(t, host.cfg, `{"kind":"android_emulator","id":"Pixel_7_API_34"}`))
	if callErr != nil {
		t.Fatalf("mobile.shutdown: %s", callErr.Message)
	}
	if got := raw.(mobileShutdownResult); !got.WasRunning {
		t.Fatal("was_running is false for an AVD that was up")
	}
	if !host.sawCommand(t, "adb -s emulator-5560 emu kill") {
		t.Fatalf("the emulator was not killed by its serial; argv were %v", host.invocations(t))
	}
}

// The caller's intent is a state, not a transition. A 4xx for "it is already
// how you wanted it" makes callers treat a success as a fault.
func TestShuttingDownADeviceThatIsAlreadyDownIsASuccess(t *testing.T) {
	host := newFakeMobileHost(t)
	raw, callErr := mobileShutdown(mobileCall(t, host.cfg, `{"kind":"ios_simulator","id":"`+simIPhone+`"}`))
	if callErr != nil {
		t.Fatalf("mobile.shutdown: %s", callErr.Message)
	}
	if got := raw.(mobileShutdownResult); got.WasRunning {
		t.Fatal("was_running is true for a simulator simctl reports as Shutdown")
	}
	if host.sawCommand(t, "xcrun simctl shutdown "+simIPhone) {
		t.Fatal("a shut-down simulator was shut down again")
	}
}

// --- the Appium proxy -------------------------------------------------------

// fakeHub is a real HTTP server standing in for Appium: it records what
// arrived and answers with whatever the test set.
type fakeHub struct {
	srv *httptest.Server
	mu  sync.Mutex
	// seen is every request, as "METHOD path?query".
	seen []string
	// auth is every Authorization header value that arrived, including empties.
	auth   []string
	status int
	body   string
}

func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	hub := &fakeHub{status: http.StatusOK, body: `{"value":{"ready":true}}`}
	hub.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Path
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		hub.mu.Lock()
		hub.seen = append(hub.seen, r.Method+" "+target)
		hub.auth = append(hub.auth, r.Header.Get("Authorization"))
		status, body := hub.status, hub.body
		hub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(hub.srv.Close)
	return hub
}

func (h *fakeHub) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.seen...)
}

// The proxy is a proxy. Appium's own status and body come back untouched,
// because the body carries the W3C error the caller classifies — and the one
// that matters most is the refusal of a second session against a device that
// already has one. Rewriting that into a failure of this Mac's would turn
// "device busy, park the task" into "something broke".
func TestTheAppiumProxyPassesTheHubsOwnStatusAndBodyThrough(t *testing.T) {
	hub := newFakeHub(t)
	hub.status = http.StatusBadRequest
	hub.body = `{"value":{"error":"session not created","message":"Cannot start a new session: device emulator-5560 is already in use"}}`
	cfg := config{appiumBaseURL: hub.srv.URL}

	res := request(t, cfg, newState(), http.MethodPost, appiumPrefix+"/session", `{"capabilities":{}}`)
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want the hub's own 400 — a status of this Mac's would hide the refusal that IS the lease", res.status)
	}
	if res.body != hub.body {
		t.Fatalf("body = %q, want the hub's own bytes %q", res.body, hub.body)
	}
	if got := hub.requests(); len(got) != 1 || got[0] != "POST /session" {
		t.Fatalf("the hub saw %v, want one POST /session", got)
	}
}

// The lease, and the only property of it this side owns: BOTH requests reach
// the hub. Appium refuses the second one and that refusal is what makes the
// lease mutual across the cloud's replicas — a lock here would serialise them,
// the first would be answered, and the second would arrive to find the device
// free again.
//
// The hub deliberately answers nothing until both have arrived, so a proxy that
// serialised would deadlock and fail this test rather than passing it slowly.
func TestTheAppiumProxyDoesNotSerialiseTwoSessionCreates(t *testing.T) {
	both := make(chan struct{})
	release := make(chan struct{})
	var arrived atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if arrived.Add(1) == 2 {
			close(both)
		}
		// No timeout here on purpose. A hub that gave up after a few seconds
		// would let a SERIALISING proxy pass this test slowly — first request
		// times out, is answered, second one then arrives — which is the exact
		// implementation the test exists to reject. `release` is the test's own
		// escape hatch and is closed only during cleanup.
		select {
		case <-both:
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"value":{"sessionId":"s"}}`)
	}))
	t.Cleanup(hub.Close)

	srv := httptest.NewServer(newRunnerServer(config{appiumBaseURL: hub.URL}, newState()).handler())
	t.Cleanup(srv.Close)
	// Registered LAST so it runs FIRST: `httptest.Server.Close` waits for the
	// requests in flight, and an implementation that failed this test would have
	// one parked inside the hub. Without this the failure would be a hang rather
	// than a message.
	t.Cleanup(func() { close(release) })

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := srv.Client().Post(srv.URL+appiumPrefix+"/session", "application/json", strings.NewReader(`{}`))
			if err != nil {
				return
			}
			defer func() { _ = res.Body.Close() }()
			_, _ = io.Copy(io.Discard, res.Body)
			statuses[i] = res.StatusCode
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("two concurrent Appium calls did not both reach the hub; something on this side is serialising them, which replaces Appium's cross-process lease with a per-Mac lock")
	}
	for i, status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("request %d answered %d, want 200", i, status)
		}
	}
	if got := arrived.Load(); got != 2 {
		t.Fatalf("the hub saw %d requests, want 2", got)
	}
}

// An Appium client builds its own paths. Every one of them has to arrive with
// its method, its path and its query intact, or the caller's ordinary client
// stops working the moment it does anything past `POST /session`.
func TestTheAppiumProxyForwardsTheMethodPathAndQuery(t *testing.T) {
	hub := newFakeHub(t)
	cfg := config{appiumBaseURL: hub.srv.URL}

	for _, tc := range []struct{ method, path, want string }{
		{http.MethodGet, "/status", "GET /status"},
		{http.MethodPost, "/session/abc-123/element", "POST /session/abc-123/element"},
		{http.MethodGet, "/session/abc-123/screenshot", "GET /session/abc-123/screenshot"},
		{http.MethodDelete, "/session/abc-123", "DELETE /session/abc-123"},
		{http.MethodGet, "/session/abc/element/e1/attribute/content-desc", "GET /session/abc/element/e1/attribute/content-desc"},
		{http.MethodGet, "/sessions?full=1", "GET /sessions?full=1"},
		// The bare prefix is the hub's root, so a client whose base URL has a
		// trailing slash is not a client that 404s.
		{http.MethodGet, "", "GET /"},
	} {
		res := request(t, cfg, newState(), tc.method, appiumPrefix+tc.path, "")
		if res.status != http.StatusOK {
			t.Fatalf("%s %s answered %d: %s", tc.method, tc.path, res.status, res.body)
		}
	}

	got := hub.requests()
	want := []string{
		"GET /status",
		"POST /session/abc-123/element",
		"GET /session/abc-123/screenshot",
		"DELETE /session/abc-123",
		"GET /session/abc/element/e1/attribute/content-desc",
		"GET /sessions?full=1",
		"GET /",
	}
	if len(got) != len(want) {
		t.Fatalf("the hub saw %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request %d was %q, want %q", i, got[i], want[i])
		}
	}
}

// The hub is a loopback process this Mac started; it holds no credential and
// has no use for one. An Authorization the caller sent for the TUNNEL is a
// cloud bearer token, and handing it to a local process is a place for it to be
// logged that nobody chose.
func TestTheAppiumProxyDoesNotForwardTheCallersAuthorization(t *testing.T) {
	hub := newFakeHub(t)
	srv := httptest.NewServer(newRunnerServer(config{appiumBaseURL: hub.srv.URL}, newState()).handler())
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+appiumPrefix+"/status", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer a-cloud-token-nobody-here-should-hold")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)

	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, value := range hub.auth {
		if value != "" {
			t.Fatalf("the hub received an Authorization header (%q); a credential minted for the tunnel has no business reaching a local process", value)
		}
	}
}

func TestTheAppiumProxyRefusesWhatWouldLeaveTheHub(t *testing.T) {
	hub := newFakeHub(t)
	cfg := config{appiumBaseURL: hub.srv.URL}

	res := request(t, cfg, newState(), http.MethodGet, appiumPrefix+"/../secrets", "")
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d for a traversal, want 400", res.status)
	}
	if got := hub.requests(); len(got) != 0 {
		t.Fatalf("a traversal still reached the hub: %v", got)
	}
}

// A Mac with no Appium says so, in the one code that means "the answer does not
// exist yet" rather than "you asked wrongly".
func TestTheAppiumProxyOnAMacWithNoHubIsNotReady(t *testing.T) {
	res := request(t, config{}, newState(), http.MethodPost, appiumPrefix+"/session", `{}`)
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.status)
	}
	if res.code() != codeNotReady {
		t.Fatalf("code = %q, want %q", res.code(), codeNotReady)
	}
}

// A hub that is configured and not answering is this Mac's dependency failing,
// which is `upstream` — and the message names the address, because "Appium is
// not running" and "Appium is running somewhere else" are two different causes
// that only the address separates.
func TestAHubThatIsNotAnsweringIsUpstream(t *testing.T) {
	// A port nothing is on. 127.0.0.1:1 is refused rather than dropped, so this
	// fails fast instead of waiting out a connect timeout.
	cfg := config{appiumBaseURL: "http://127.0.0.1:1"}
	res := request(t, cfg, newState(), http.MethodGet, appiumPrefix+"/status", "")
	if res.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.status)
	}
	if !strings.Contains(res.body, "127.0.0.1:1") {
		t.Fatalf("body = %q, want the address in it", res.body)
	}
}

// `mobile.devices` reports the hub as two separate facts because they have two
// separate remedies: no Appium at all is an install, and one that is not
// answering is a restart.
func TestMobileDevicesReportsTheHubAsConfiguredAndReachableSeparately(t *testing.T) {
	hub := newFakeHub(t)
	host := newFakeMobileHost(t)

	none := mobileDevicesOrFail(t, mobileCall(t, host.cfg, `{}`))
	if none.Capabilities.Appium.Configured || none.Capabilities.Appium.Reachable {
		t.Fatalf("appium = %+v on a Mac with none configured", none.Capabilities.Appium)
	}
	if none.Capabilities.Appium.DrivePath != appiumPrefix {
		t.Fatalf("drive_path = %q, want %q — a caller should never have to hold a copy of this runner's routing table",
			none.Capabilities.Appium.DrivePath, appiumPrefix)
	}

	cfg := host.cfg
	cfg.appiumBaseURL = hub.srv.URL
	up := mobileDevicesOrFail(t, mobileCall(t, cfg, `{}`))
	if !up.Capabilities.Appium.Configured || !up.Capabilities.Appium.Reachable {
		t.Fatalf("appium = %+v against a hub that is answering", up.Capabilities.Appium)
	}

	cfg.appiumBaseURL = "http://127.0.0.1:1"
	down := mobileDevicesOrFail(t, mobileCall(t, cfg, `{}`))
	if !down.Capabilities.Appium.Configured || down.Capabilities.Appium.Reachable {
		t.Fatalf("appium = %+v against a configured hub that is not answering", down.Capabilities.Appium)
	}
	if down.Capabilities.Appium.Detail == "" {
		t.Fatal("a hub that is not answering says nothing about why")
	}
}

func mobileDevicesOrFail(t *testing.T, c *call) mobileDevicesResult {
	t.Helper()
	raw, err := mobileDevices(c)
	if err != nil {
		t.Fatalf("mobile.devices: %s", err.Message)
	}
	return raw.(mobileDevicesResult)
}

// --- the routing ------------------------------------------------------------

func TestTheMobileMethodsAreOnTheVerbsTheContractNames(t *testing.T) {
	host := newFakeMobileHost(t)
	for _, tc := range []struct {
		path, wrongMethod string
	}{
		{"/mobile.devices", http.MethodPost},
		{"/mobile.boot", http.MethodGet},
		{"/mobile.shutdown", http.MethodGet},
		{"/toolchain.detect", http.MethodGet},
	} {
		res := request(t, host.cfg, newState(), tc.wrongMethod, tc.path, "")
		if res.code() != codeUnsupportedMethod {
			t.Fatalf("%s %s answered %q, want %q", tc.wrongMethod, tc.path, res.code(), codeUnsupportedMethod)
		}
	}
}

// The two document methods over a real socket, on the verbs the contract names.
// The function-level tests above prove the answers; this proves the caller can
// reach them at all — a GET with no body and a POST with one, through the same
// routing table the control plane hits.
func TestTheNewMethodsAnswerOverHTTP(t *testing.T) {
	host := newFakeMobileHost(t)
	checkout := filepath.Join(host.cfg.workspaceDir, "repos", "acme")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatalf("creating the checkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".nvmrc"), []byte("20.11.0\n"), 0o644); err != nil {
		t.Fatalf("writing the pin file: %v", err)
	}

	devices := request(t, host.cfg, newState(), http.MethodGet, "/mobile.devices", "")
	if devices.status != http.StatusOK {
		t.Fatalf("GET /mobile.devices = %d: %s", devices.status, devices.body)
	}
	var inventory mobileDevicesResult
	if err := json.Unmarshal([]byte(devices.body), &inventory); err != nil {
		t.Fatalf("body %q is not the inventory: %v", devices.body, err)
	}
	if len(inventory.IOS) != 2 {
		t.Fatalf("ios = %+v over HTTP, want the two simulators", inventory.IOS)
	}

	toolchain := request(t, host.cfg, newState(), http.MethodPost, "/toolchain.detect", `{"workspace":"repos/acme"}`)
	if toolchain.status != http.StatusOK {
		t.Fatalf("POST /toolchain.detect = %d: %s", toolchain.status, toolchain.body)
	}
	var pins toolchainResult
	if err := json.Unmarshal([]byte(toolchain.body), &pins); err != nil {
		t.Fatalf("body %q is not the toolchain answer: %v", toolchain.body, err)
	}
	if pins.Env["NODE_VERSION"] != "20.11.0" {
		t.Fatalf("env = %v over HTTP, want the .nvmrc pin ready to pass to claude.run", pins.Env)
	}
}

// The routing table and the proxy have to describe the same set of paths.
//
// The router matches on `r.URL.Path`, which is DECODED, so
// `/mobile%2Eappium/status` reaches the proxy — and a proxy that trimmed a
// prefix only present after decoding would forward the whole undecoded path to
// the hub as if it were a route.
func TestAnEncodedPrefixIsNotARouteIntoTheHub(t *testing.T) {
	hub := newFakeHub(t)
	srv := httptest.NewServer(newRunnerServer(config{appiumBaseURL: hub.srv.URL}, newState()).handler())
	t.Cleanup(srv.Close)

	// Built by hand: a client would helpfully re-encode this away.
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.URL.Opaque = "/mobile%2Eappium/status"
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d for an encoded prefix, want 404", res.StatusCode)
	}
	if got := hub.requests(); len(got) != 0 {
		t.Fatalf("an encoded prefix still reached the hub: %v", got)
	}
}
