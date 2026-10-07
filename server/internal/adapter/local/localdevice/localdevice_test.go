package localdevice

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simctlFixture is two runtimes, one of them holding an unavailable device —
// simctl keeps listing devices whose runtime was uninstalled, and offering one
// of those for registration would offer a device that can never boot.
const simctlFixture = `{
  "devices": {
    "com.apple.CoreSimulator.SimRuntime.iOS-17-4": [
      {"udid":"11111111-2222-3333-4444-555555555555","name":"iPhone 15","state":"Shutdown","isAvailable":true},
      {"udid":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","name":"iPad Air","state":"Booted","isAvailable":true}
    ],
    "com.apple.CoreSimulator.SimRuntime.iOS-15-0": [
      {"udid":"99999999-8888-7777-6666-555555555555","name":"iPhone 12","state":"Shutdown","isAvailable":false}
    ]
  }
}`

// newHost builds a Host pointed at the stand-in scripts, with a fresh fixture
// directory. goos is passed explicitly so the Linux degradation can be
// exercised on the macOS machine this is developed on — the case that otherwise
// has no CI runner to prove it.
func newHost(t *testing.T, goos string) (*Host, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALDEVICE_FAKE_DIR", dir)
	return New(Config{
		GOOS:     goos,
		Xcrun:    wrapFake(t, dir, "fake-xcrun.sh"),
		ADB:      wrapFake(t, dir, "fake-adb.sh"),
		Emulator: wrapFake(t, dir, "fake-emulator.sh"),
		// Short waits, so a poll that will never succeed fails in a second
		// instead of holding the suite for the production four minutes.
		BootTimeout:  3 * time.Second,
		PollInterval: 20 * time.Millisecond,
	}), dir
}

// wrapFake returns a launcher for one of the testdata stand-ins that sets
// LOCALDEVICE_FAKE_DIR for itself, and records the environment it was handed.
//
// The variable cannot be inherited: Host builds its children's environment with
// platform/childenv, which forwards an allowlist and drops everything else — so
// a fixture directory named through the environment would never reach the
// script. Having to work around the scrub here is the same thing as proving it
// happens, and env.txt is what the scrub test reads.
func wrapFake(t *testing.T, dir, name string) string {
	t.Helper()
	target, err := filepath.Abs(filepath.Join("testdata", name))
	require.NoError(t, err)

	launcher := filepath.Join(dir, "run-"+name)
	body := "#!/bin/sh\n" +
		"env | sort > '" + filepath.Join(dir, "child-env.txt") + "'\n" +
		"LOCALDEVICE_FAKE_DIR='" + dir + "'\n" +
		"export LOCALDEVICE_FAKE_DIR\n" +
		"exec '" + target + "' \"$@\"\n"
	require.NoError(t, os.WriteFile(launcher, []byte(body), 0o700))
	return launcher
}

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

// recorded returns every invocation of one fake, one per line.
func recorded(t *testing.T, dir, name string) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimRight(string(body), "\n"), "\n")
}

// The catalog is what the settings page offers, so the shape of the answer is
// the contract: available devices only, and a runtime rendered the way a person
// writes it rather than as an Apple reverse-DNS identifier.
func TestSimulatorsComeFromSimctlWithReadableRuntimes(t *testing.T) {
	host, dir := newHost(t, "darwin")
	writeFixture(t, dir, "simctl-devices.json", simctlFixture)

	sims, err := host.Simulators(context.Background())
	require.NoError(t, err)

	require.Len(t, sims, 2, "the device on an uninstalled runtime must not be offered")
	// Sorted by runtime then name, because map iteration is random and a list
	// that reshuffles on every page load is one nobody can point at.
	assert.Equal(t, "iPad Air", sims[0].Name)
	assert.Equal(t, "iOS 17.4", sims[0].Runtime)
	assert.Equal(t, "Booted", sims[0].State)
	assert.Equal(t, "iPhone 15", sims[1].Name)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", sims[1].UDID)
}

// The platform version Appium wants is the number, not the marketing name.
func TestRuntimeVersionIsTheNumericHalf(t *testing.T) {
	assert.Equal(t, "17.4", RuntimeVersion("iOS 17.4"))
	assert.Equal(t, "", RuntimeVersion(""))
}

// A Linux host must behave exactly as it did before this package existed: no
// simulators, whatever binaries happen to be sitting on its PATH. Gating on
// GOOS as well as on the binary is what makes an `xcrun` shim unable to claim
// simulators the host has no way to run.
func TestLinuxHostReportsNoSimulators(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "simctl-devices.json", simctlFixture)

	ctx := context.Background()
	assert.False(t, host.SupportsIOSSimulators(ctx))

	catalog, err := host.Catalog(ctx)
	require.NoError(t, err)
	assert.Empty(t, catalog.IOS)
	// Empty ARRAYS, not nulls: the settings page has to render "asked and there
	// are none", which a null cannot say.
	assert.NotNil(t, catalog.IOS)
	assert.NotNil(t, catalog.Android)
	assert.Empty(t, recorded(t, dir, "xcrun-argv.txt"), "a linux host must not run simctl at all")
}

// The arguments are a UDID and an AVD name arriving over the settings API, and
// there is no shell anywhere in this package — so the defence has to be the
// allow-list, and it has to run before the exec rather than after it.
func TestShellMetacharactersAreRefusedBeforeAnyExec(t *testing.T) {
	host, dir := newHost(t, "darwin")

	for _, bad := range []string{
		"11111111-2222-3333-4444-555555555555; rm -rf /",
		"$(whoami)",
		"../../etc/passwd",
		"",
		"11111111-2222-3333-4444-55555555555", // one digit short
	} {
		assert.Error(t, ValidateSimulatorUDID(bad), "accepted %q as a simulator UDID", bad)
		assert.Error(t, host.BootSimulator(context.Background(), bad))
		assert.Error(t, host.ShutdownSimulator(context.Background(), bad))
	}
	for _, bad := range []string{"Pixel 7", "avd;reboot", "--help", "", "a/b"} {
		assert.Error(t, ValidateAVDName(bad), "accepted %q as an AVD name", bad)
	}
	for _, bad := range []string{"emulator-", "1234abcd", "emulator-5554; ls", "R58M12345"} {
		assert.Error(t, ValidateEmulatorSerial(bad), "accepted %q as an emulator serial", bad)
	}
	assert.Empty(t, recorded(t, dir, "xcrun-argv.txt"), "a refused argument must never reach a process")
}

// Every argument travels as one argv entry. This is the positive half of the
// validation test: proving the vector form is what carries them means a value
// that somehow got past validation still cannot become a second command.
func TestArgumentsTravelAsAVector(t *testing.T) {
	host, dir := newHost(t, "darwin")
	writeFixture(t, dir, "simctl-devices.json", simctlFixture)

	require.NoError(t, host.BootSimulator(context.Background(), "11111111-2222-3333-4444-555555555555"))

	calls := recorded(t, dir, "xcrun-argv.txt")
	assert.Contains(t, calls, "simctl boot 11111111-2222-3333-4444-555555555555")
	// bootstatus is not optional: simctl boot returns while the device is still
	// coming up, and a session created then fails in ways that read to an agent
	// as a broken app.
	assert.Contains(t, calls, "simctl bootstatus 11111111-2222-3333-4444-555555555555 -b")
}

// A simulator that is already up must not be booted again — the operator's own
// Simulator.app may have started it, and simctl treats a redundant boot as an
// error rather than a no-op.
func TestBootSkipsAnAlreadyBootedSimulator(t *testing.T) {
	host, dir := newHost(t, "darwin")
	writeFixture(t, dir, "simctl-devices.json", simctlFixture)

	require.NoError(t, host.BootSimulator(context.Background(), "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"))

	for _, call := range recorded(t, dir, "xcrun-argv.txt") {
		assert.NotContains(t, call, "simctl boot ", "a booted simulator was booted again")
	}
}

// Which serial an AVD is on is ASKED, never derived from the console port: the
// port is allocated in start order, so a restarted AVD is on a different one
// and a remembered serial would drive whichever emulator is there now.
func TestEmulatorSerialIsResolvedByAskingEachDevice(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "adb-devices.txt", "List of devices attached\nemulator-5554\tdevice\nemulator-5556\tdevice\n")
	writeFixture(t, dir, "avd-emulator-5554.txt", "Pixel_5_API_33\n")
	writeFixture(t, dir, "avd-emulator-5556.txt", "Pixel_7_API_34\n")

	serial, err := host.EmulatorSerial(context.Background(), "Pixel_7_API_34")
	require.NoError(t, err)
	assert.Equal(t, "emulator-5556", serial)

	missing, err := host.EmulatorSerial(context.Background(), "Nexus_One")
	require.NoError(t, err)
	assert.Equal(t, "", missing)
}

// A serial that is not fully attached is not a device a session can be created
// against, so reporting it as running would hand a run a dead phone.
func TestOfflineAndPhysicalSerialsAreNotEmulators(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "adb-devices.txt",
		"List of devices attached\nemulator-5554\toffline\nR58M12345\tdevice\nemulator-5556\tdevice\n")

	serials, err := host.RunningEmulators(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"emulator-5556"}, serials)
}

// The AVD list is filtered by the name grammar rather than by known log
// prefixes: the emulator binary prints diagnostics onto the same stream on some
// SDK versions, and a new one must not become a device nobody can boot.
func TestAVDListIgnoresDiagnosticLines(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "avds.txt", "INFO    | storing crashdata\nPixel_7_API_34\n\nPixel_5_API_33\n")

	avds, err := host.AVDs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"Pixel_7_API_34", "Pixel_5_API_33"}, avds)
}

// Starting an AVD that is already up returns its serial without launching a
// second copy — the common case, since an operator often has one open already.
func TestStartEmulatorReusesARunningAVD(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "adb-devices.txt", "List of devices attached\nemulator-5554\tdevice\n")
	writeFixture(t, dir, "avd-emulator-5554.txt", "Pixel_7_API_34\n")

	serial, err := host.StartEmulator(context.Background(), "Pixel_7_API_34")
	require.NoError(t, err)
	assert.Equal(t, "emulator-5554", serial)
	assert.Empty(t, recorded(t, dir, "emulator-argv.txt"), "a running AVD must not be launched again")

	// adb answers while Android is still on the boot animation, so the wait is
	// for sys.boot_completed and not merely for the device to appear.
	calls := recorded(t, dir, "adb-argv.txt")
	assert.Contains(t, calls, "-s emulator-5554 wait-for-device")
	assert.Contains(t, calls, "-s emulator-5554 shell getprop sys.boot_completed")
}

// The launch is headless and detached. Headless because a window would steal
// focus on the operator's desktop every time a QA task started; detached
// because the emulator has to outlive the request that asked for it.
func TestStartEmulatorLaunchesHeadlessAndWaitsForIt(t *testing.T) {
	host, dir := newHost(t, "linux")
	writeFixture(t, dir, "adb-devices.txt", "List of devices attached\n")
	writeFixture(t, dir, "avd-emulator-5554.txt", "Pixel_7_API_34\n")
	writeFixture(t, dir, "avds.txt", "Pixel_7_API_34\n")
	// The fake appends this to adb-devices.txt when launched, which is how a
	// real emulator becomes visible: some time after the process starts.
	writeFixture(t, dir, "booting.txt", "emulator-5554\tdevice\n")
	// This one succeeds, so a generous wait costs nothing; 3s failed under a
	// loaded `go test ./...` before the detached fake emulator got scheduled.
	host.bootTimeout = 20 * time.Second

	serial, err := host.StartEmulator(context.Background(), "Pixel_7_API_34")
	require.NoError(t, err)
	assert.Equal(t, "emulator-5554", serial)
	assert.Equal(t, []string{"-avd Pixel_7_API_34 -no-window -no-audio"}, recorded(t, dir, "emulator-argv.txt"))
}

// `emu kill` rather than killing a process: this code did not necessarily start
// the emulator, and the console is the only handle that works for an AVD
// somebody launched from Android Studio.
func TestStopEmulatorGoesThroughTheConsole(t *testing.T) {
	host, dir := newHost(t, "linux")

	require.NoError(t, host.StopEmulator(context.Background(), "emulator-5554"))
	assert.Contains(t, recorded(t, dir, "adb-argv.txt"), "-s emulator-5554 emu kill")

	// A physical device's serial is refused, so this can never kill a phone
	// somebody has plugged in.
	assert.Error(t, host.StopEmulator(context.Background(), "R58M12345"))
}

// A host with adb but no emulator binary can still drive an AVD that is already
// running — refusing it would refuse a device that works because a convenience
// is missing.
func TestAndroidSupportSurvivesAMissingEmulatorBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALDEVICE_FAKE_DIR", dir)
	adb := wrapFake(t, dir, "fake-adb.sh")
	writeFixture(t, dir, "adb-devices.txt", "List of devices attached\nemulator-5554\tdevice\n")
	writeFixture(t, dir, "avd-emulator-5554.txt", "Pixel_7_API_34\n")

	host := New(Config{GOOS: "linux", ADB: adb, Emulator: filepath.Join(dir, "nope")})
	ctx := context.Background()

	assert.True(t, host.SupportsAndroidEmulators(ctx))
	exists, err := host.AVDExists(ctx, "Pixel_7_API_34")
	require.NoError(t, err)
	assert.True(t, exists)
}

// Nothing installed at all is not an error: it is what a cluster node is, and
// it has to boot exactly as it did before this package existed.
func TestHostWithNoToolchainReportsNothing(t *testing.T) {
	dir := t.TempDir()
	host := New(Config{GOOS: "linux", ADB: filepath.Join(dir, "nope"), Emulator: filepath.Join(dir, "nope")})
	ctx := context.Background()

	assert.False(t, host.SupportsIOSSimulators(ctx))
	assert.False(t, host.SupportsAndroidEmulators(ctx))
	catalog, err := host.Catalog(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, len(catalog.IOS))
	assert.Equal(t, 0, len(catalog.Android))
}

// adb and the emulator are children of this process like any other, and this
// process holds the database DSN and gateway key until the boot scrub
// runs (platform/runtime/envscrub.go) — plus whatever an operator has exported
// on the laptop this runs on. `adb shell` runs an arbitrary command on a device
// and an emulator outlives the request that started it, so of everything here
// these are the last two children that should inherit any of it.
func TestChildrenDoNotInheritThisProcessesEnvironment(t *testing.T) {
	host, dir := newHost(t, "linux")
	t.Setenv("INTERNAL_AUTH_KEY", "gateway-hmac-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")
	t.Setenv("ANDROID_SDK_ROOT", "/opt/android-sdk")
	writeFixture(t, dir, "adb-devices.txt", "List of devices attached\nemulator-5554\tdevice\n")

	_, err := host.RunningEmulators(context.Background())
	require.NoError(t, err)

	env := readFixture(t, dir, "child-env.txt")
	assert.NotContains(t, env, "INTERNAL_AUTH_KEY", "the gateway key must never reach a device tool")
	assert.NotContains(t, env, "DATABASE_URL")
	assert.NotContains(t, env, "gateway-hmac-secret")
	assert.Contains(t, env, "ANDROID_SDK_ROOT=/opt/android-sdk", "the SDK location is not a credential and adb needs it")
	assert.Contains(t, env, "PATH=", "a child with no PATH finds no binaries at all")
}

func readFixture(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(body)
}

func TestAndroidSDKPathsFollowEachOSInstallLocation(t *testing.T) {
	home := filepath.Join("home", "dev")
	cases := []struct {
		name string
		goos string
		env  map[string]string
		want []string
	}{
		{
			name: "macOS uses Android Studio's Library path",
			goos: "darwin",
			want: []string{filepath.Join(home, "Library", "Android", "sdk", "platform-tools", "adb")},
		},
		{
			name: "Linux uses ~/Android/Sdk",
			goos: "linux",
			want: []string{filepath.Join(home, "Android", "Sdk", "platform-tools", "adb")},
		},
		{
			name: "Windows uses LOCALAPPDATA and the .exe name",
			goos: "windows",
			env:  map[string]string{"LOCALAPPDATA": filepath.Join("C:", "Users", "dev", "AppData", "Local")},
			want: []string{filepath.Join("C:", "Users", "dev", "AppData", "Local", "Android", "Sdk", "platform-tools", "adb.exe")},
		},
		{
			name: "Windows without LOCALAPPDATA falls back to the profile",
			goos: "windows",
			want: []string{filepath.Join(home, "AppData", "Local", "Android", "Sdk", "platform-tools", "adb.exe")},
		},
		{
			name: "SDK roots from the environment come first",
			goos: "linux",
			env:  map[string]string{"ANDROID_HOME": "/opt/sdk-a", "ANDROID_SDK_ROOT": "/opt/sdk-b"},
			want: []string{
				filepath.Join("/opt/sdk-a", "platform-tools", "adb"),
				filepath.Join("/opt/sdk-b", "platform-tools", "adb"),
				filepath.Join(home, "Android", "Sdk", "platform-tools", "adb"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			assert.Equal(t, tc.want, androidSDKPaths(tc.goos, getenv, home, "platform-tools", "adb"))
		})
	}
}

func TestAndroidSDKPathsWithoutAHomeOnlyUseTheEnvironment(t *testing.T) {
	getenv := func(key string) string {
		if key == "ANDROID_HOME" {
			return "/opt/sdk"
		}
		return ""
	}
	assert.Equal(t, []string{filepath.Join("/opt/sdk", "emulator", "emulator")}, androidSDKPaths("linux", getenv, "", "emulator", "emulator"))
}

func TestIsExecutablePerOS(t *testing.T) {
	cases := []struct {
		name string
		goos string
		path string
		mode fs.FileMode
		want bool
	}{
		{"unix executable bit", "linux", "/sdk/adb", 0o755, true},
		{"unix without the bit", "darwin", "/sdk/adb", 0o644, false},
		{"unix directory", "linux", "/sdk/adb", fs.ModeDir | 0o755, false},
		{"windows exe without any execute bit", "windows", `C:\sdk\adb.exe`, 0o666, true},
		{"windows extension is case-insensitive", "windows", `C:\sdk\ADB.EXE`, 0o666, true},
		{"windows batch file", "windows", `C:\sdk\emulator.bat`, 0o666, true},
		{"windows cmd script", "windows", `C:\sdk\emulator.cmd`, 0o666, true},
		{"windows extension-less file", "windows", `C:\sdk\adb`, 0o777, false},
		{"windows directory named like an exe", "windows", `C:\sdk\adb.exe`, fs.ModeDir | 0o777, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isExecutable(tc.goos, tc.path, tc.mode))
		})
	}
}
