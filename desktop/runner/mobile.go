package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// mobile.* — the simulators and emulators attached to THIS Mac.
//
// A Linux pod cannot run an iOS simulator, and a Mac being a machine with
// devices on it is the whole reason mobile QA works at all. This half of the
// product used to live in agent-server, which ran beside this program on the
// same machine and could exec `xcrun` itself; the backend is one shared cloud
// deployment now, so the capability comes back as RPC methods reached down the
// tunnel rather than as a local hub somebody else discovers.
//
//	GET  /mobile.devices    what this Mac has, and what it cannot do and why
//	POST /mobile.boot       bring one up and say what Appium should be handed
//	POST /mobile.shutdown   put one back down
//	ANY  /mobile.appium/…   the local Appium hub, verbatim
//
// # The lease is Appium's, and nothing here may become a second one
//
// A second Appium session against a device that already has one FAILS, and that
// failure is what makes the lease mutual across processes — the cloud runs
// several replicas, and a mutex in any one of them covers only itself. So this
// file holds no lock, no queue and no registry of which device is busy: the
// proxy below forwards concurrent requests concurrently and passes Appium's
// refusal back with its own status and body, which is what the caller already
// classifies as "device busy, park the task". Serialising here would replace a
// cross-process lock with a per-Mac one and quietly break the parking path.
//
// # The physical-phone bridge is somebody else's path
//
// A phone plugged into a wall, reached through the cluster's adb bridge, is
// `remote_adb` and has nothing to do with this file. It is refused BY NAME
// below rather than falling through as an unknown kind, so the message says
// where that kind is handled instead of reading as a typo. The emulator serial
// grammar is the other half of the same rule: `adb -s <serial> emu kill` is only
// ever aimed at an `emulator-NNNN`, so this program can never kill a phone
// somebody has plugged into their laptop.

const (
	// mobileListTimeout bounds an inventory command. simctl and adb answer
	// these from local state, so a slow one means the toolchain is wedged
	// rather than busy, and a settings page must not hang on it.
	mobileListTimeout = 30 * time.Second

	// mobileActionTimeout bounds a command that changes something (boot,
	// shutdown, emu kill). Longer than a list because simctl's boot itself can
	// sit behind CoreSimulator starting up.
	mobileActionTimeout = 90 * time.Second

	// mobileDefaultBootTimeout is the ceiling on waiting for a device to become
	// usable when the caller names none. A cold Android emulator on a laptop
	// genuinely takes minutes; past this it is not slow, it is stuck.
	mobileDefaultBootTimeout = 4 * time.Minute

	// mobileMaxBootTimeout bounds what a caller may ask for. A ceiling the
	// caller chooses is not a ceiling.
	mobileMaxBootTimeout = 15 * time.Minute

	// mobileBootPoll is how often a boot wait re-asks. Frequent enough that a
	// fast simulator does not sit idle, cheap enough that fifteen minutes of
	// polling is not a load.
	mobileBootPoll = 2 * time.Second

	// appiumStatusTimeout bounds the reachability probe in `mobile.devices`.
	// The hub is a loopback process: it answers in milliseconds or it is not
	// there, and a slow answer must not hold the inventory.
	appiumStatusTimeout = 3 * time.Second

	// appiumProxyTimeout is the ceiling on one proxied call. Generous because
	// `POST /session` installs and launches an app and starts a driver on a
	// cold device — the caller's own bound for that is three minutes — and
	// bounded because a wedged hub must not hold a tunnel stream forever.
	appiumProxyTimeout = 6 * time.Minute

	// appiumResponseLimit bounds the body copied back. A full-resolution
	// screenshot arrives base64-encoded inside a JSON document, so this is
	// megabytes rather than kilobytes; it is the same 24 MiB the caller reads,
	// so nothing is cut here that would have survived there anyway.
	appiumResponseLimit = 24 * 1024 * 1024
)

// appiumPrefix is the path everything under it is proxied from. It is part of
// the contract: the caller sets Appium's base URL to <tunnel base>/mobile.appium
// and its ordinary Appium client works unchanged, because every path it builds
// lands underneath.
const appiumPrefix = "/mobile.appium"

// The device kinds this Mac can drive, spelled exactly as agent-server's
// domain does. Two of the three, deliberately: `remote_adb` is the bridge's.
const (
	kindIOSSimulator    = "ios_simulator"
	kindAndroidEmulator = "android_emulator"
	kindRemoteADB       = "remote_adb"
)

// simulatorUDID is simctl's identity: a UUID. Anchored and exact — a permissive
// "no spaces" rule would still accept `-rf` or a path, and these become argv.
var simulatorUDID = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// avdName is what `avdmanager create avd` permits. Notably no spaces: the SDK
// itself replaces them with underscores, so a name with one in it never came
// from the SDK.
//
// The first character may not be a dash or a dot, and that is the security rule
// rather than the naming one: `emulator -avd --help` and `-avd -ports:5554` are
// argument injection with no shell involved at all, because the emulator binary
// parses its own argv. A leading dot would likewise let a name reach for a path.
var avdName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)

// emulatorSerial is how adb names a running emulator. The console port is what
// makes it unique on a host, and it is always numeric. A physical device's
// serial does not match, which is the point: nothing here may `emu kill` a
// phone somebody has plugged in.
var emulatorSerial = regexp.MustCompile(`^emulator-[0-9]{1,6}$`)

// --- what this Mac has ------------------------------------------------------

// localSimulator is one iOS simulator. `platform_version` is carried beside the
// runtime because it is what Appium's `appium:platformVersion` capability
// wants, and simctl knows it exactly — making the caller parse "iOS 17.4" would
// only create a way to be wrong about a fact this side already has.
type localSimulator struct {
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	// PlatformVersion is the numeric half of Runtime: "iOS 17.4" → "17.4".
	PlatformVersion string `json:"platform_version,omitempty"`
	// State is simctl's own word — "Booted", "Shutdown". Reported rather than
	// reduced to a boolean so a UI can say what is happening between the two.
	State string `json:"state"`
}

// runningEmulator ties an AVD to the adb serial it came up on. The serial is
// allocated at boot and changes when an emulator is restarted, which is why it
// is reported rather than remembered.
type runningEmulator struct {
	AVD    string `json:"avd"`
	Serial string `json:"serial"`
}

// mobileCapability is "can this Mac do this half, and if not, why not".
//
// The `detail` is not decoration. A cloud that is told `false` and nothing else
// can only say "unavailable", which sends the user looking in the wrong place;
// told "this machine has no Android SDK platform-tools (adb)" it can say the thing
// that fixes it.
type mobileCapability struct {
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// appiumCapability separates two facts that fail differently: whether this Mac
// has a hub configured at all, and whether a call would get to it right now.
type appiumCapability struct {
	Configured bool `json:"configured"`
	// Reachable is whether a call through DrivePath gets to a hub: one answers,
	// or this runner starts one on demand and its last start has not just
	// failed.
	Reachable bool `json:"reachable"`
	// OnDemand says this runner starts the hub itself when a call needs one
	// and stops it once idle, so nothing answering right now is the ordinary
	// state rather than a fault.
	OnDemand bool   `json:"on_demand,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	// DrivePath is where to point an Appium client, relative to this runner's
	// own base. Reported rather than assumed so the caller never has to hold a
	// copy of this program's routing table.
	DrivePath string `json:"drive_path"`
	Detail    string `json:"detail,omitempty"`
}

type mobileCapabilities struct {
	IOSSimulators    mobileCapability `json:"ios_simulators"`
	AndroidEmulators mobileCapability `json:"android_emulators"`
	Appium           appiumCapability `json:"appium"`
}

// mobileDevicesResult is the inventory. Every slice is non-nil: an empty array
// says "this Mac was asked and has none", which is a different statement from a
// null, and it is the one a settings page has to render.
type mobileDevicesResult struct {
	V              int                `json:"v"`
	IOS            []localSimulator   `json:"ios"`
	Android        []string           `json:"android"`
	AndroidRunning []runningEmulator  `json:"android_running"`
	Capabilities   mobileCapabilities `json:"capabilities"`
}

// mobileDevices answers what this Mac has.
//
// A failure on one side never fails the other: a broken Android SDK must not
// make the simulators disappear. What a failure does do is become the `detail`
// on that half's capability, because "asking failed" and "there are none" are
// the same to a form offering a choice and very different to whoever has to fix
// it.
func mobileDevices(c *call) (any, *rpcError) {
	out := mobileDevicesResult{
		V:              protocolVersion,
		IOS:            []localSimulator{},
		Android:        []string{},
		AndroidRunning: []runningEmulator{},
	}

	switch {
	case c.cfg.xcrunBin == "":
		out.Capabilities.IOSSimulators = mobileCapability{
			Detail: "this machine has no Xcode command line tools (xcrun), so it has no iOS simulators",
		}
	default:
		sims, err := listSimulators(c)
		if err != nil {
			out.Capabilities.IOSSimulators = mobileCapability{Detail: err.Message}
		} else {
			out.IOS = sims
			out.Capabilities.IOSSimulators = mobileCapability{Available: true}
		}
	}

	// adb is what an emulator is TALKED TO through, so it is mandatory; the
	// `emulator` binary is not. A Mac where somebody already started an AVD
	// from Android Studio can drive it perfectly well without ever launching
	// one, and refusing that Mac would be refusing a working device over a
	// missing convenience.
	switch {
	case c.cfg.adbBin == "":
		out.Capabilities.AndroidEmulators = mobileCapability{
			Detail: "this machine has no Android SDK platform-tools (adb), so it cannot reach an emulator",
		}
	default:
		running, err := listRunningEmulators(c)
		if err != nil {
			out.Capabilities.AndroidEmulators = mobileCapability{Detail: err.Message}
			break
		}
		out.AndroidRunning = running
		if c.cfg.emulatorBin == "" {
			out.Capabilities.AndroidEmulators = mobileCapability{
				Available: len(running) > 0,
				Detail: "this machine has adb but no `emulator` binary, so it can drive an AVD that is already running " +
					"and cannot start one",
			}
			break
		}
		avds, err := listAVDs(c)
		if err != nil {
			out.Capabilities.AndroidEmulators = mobileCapability{Detail: err.Message}
			break
		}
		out.Android = avds
		out.Capabilities.AndroidEmulators = mobileCapability{Available: true}
	}

	out.Capabilities.Appium = appiumStatus(c)
	return out, nil
}

// appiumStatus asks the hub whether it is there.
//
// Two booleans rather than one, because they have two different remedies:
// `configured:false` means this Mac has no Appium installed (the desktop app
// reports it in the preflight either way), and `reachable:false` means one is
// configured and a call would not get to it.
//
// It never starts the hub. This is polled by settings pages and sweepers; a
// probe that started one would keep it running forever. A hub this runner
// starts on demand and that is simply not running is therefore reachable:
// read as down, a task parked on a free device would wait for a hub that only
// its own run starts.
func appiumStatus(c *call) appiumCapability {
	status := appiumCapability{DrivePath: appiumPrefix}
	if c.cfg.appiumBaseURL == "" {
		status.Detail = "this machine has no Appium; a session cannot be driven against a device here until it is installed"
		return status
	}
	status.Configured = true
	status.BaseURL = c.cfg.appiumBaseURL
	hub := c.state.appiumHub()
	status.OnDemand = hub != nil

	why := probeAppium(c.ctx, c.cfg.appiumBaseURL)
	if why == "" {
		status.Reachable = true
		return status
	}
	if hub == nil {
		status.Detail = why
		return status
	}
	if failed := hub.recentFailure(); failed != nil {
		status.Detail = failed.Message
		return status
	}
	status.Reachable = true
	status.Detail = fmt.Sprintf("nothing is running on %s right now; this machine starts Appium there when a session is driven, "+
		"and stops it after %s unused", c.cfg.appiumBaseURL, hub.t.idleAfter)
	return status
}

// probeAppium is empty when the hub at base answers, and otherwise the
// sentence saying why not.
func probeAppium(ctx context.Context, base string) string {
	ctx, cancel := context.WithTimeout(ctx, appiumStatusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status", nil)
	if err != nil {
		return fmt.Sprintf("building the status request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Sprintf("nothing answered at %s (%v)", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("%s/status answered %d", base, resp.StatusCode)
	}
	return ""
}

// simctlList is the shape of `simctl list devices --json`. The map key is the
// runtime identifier ("com.apple.CoreSimulator.SimRuntime.iOS-17-4"), which is
// the only place the version appears.
type simctlList struct {
	Devices map[string][]struct {
		UDID        string `json:"udid"`
		Name        string `json:"name"`
		State       string `json:"state"`
		IsAvailable bool   `json:"isAvailable"`
	} `json:"devices"`
}

// listSimulators lists every usable simulator, booted or shut down.
//
// Both states, because a shut-down simulator is the normal thing to register:
// the whole point of `mobile.boot` is that this Mac brings it up. Unavailable
// ones are dropped — they are devices whose runtime was uninstalled, and simctl
// keeps listing them so a repair prompt can be shown, which this is not.
func listSimulators(c *call) ([]localSimulator, *rpcError) {
	raw, callErr := mobileRun(c, c.cfg.xcrunBin, "xcrun", mobileListTimeout, "simctl", "list", "devices", "--json")
	if callErr != nil {
		return nil, callErr
	}
	var parsed simctlList
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, failure(codeUpstream, "simctl's device list is not the JSON this runner expects: %v", err)
	}
	out := []localSimulator{}
	for runtimeID, devices := range parsed.Devices {
		name := runtimeName(runtimeID)
		for _, d := range devices {
			if !d.IsAvailable || d.UDID == "" {
				continue
			}
			out = append(out, localSimulator{
				UDID:            d.UDID,
				Name:            d.Name,
				Runtime:         name,
				PlatformVersion: runtimeVersion(name),
				State:           d.State,
			})
		}
	}
	// Map iteration order is random, and a list of simulators that reshuffles
	// on every load is one nobody can point at. Runtime then name, so the
	// newest iOS does not sort between two older ones by device name.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runtime != out[j].Runtime {
			return out[i].Runtime < out[j].Runtime
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// runtimeName turns simctl's runtime identifier into what a person calls it:
// "com.apple.CoreSimulator.SimRuntime.iOS-17-4" → "iOS 17.4". An identifier
// that does not follow the shape is returned unchanged rather than mangled — it
// is still more useful than an empty column.
func runtimeName(id string) string {
	const prefix = "com.apple.CoreSimulator.SimRuntime."
	trimmed := strings.TrimPrefix(id, prefix)
	if trimmed == id {
		return id
	}
	parts := strings.SplitN(trimmed, "-", 2)
	if len(parts) != 2 {
		return trimmed
	}
	return parts[0] + " " + strings.ReplaceAll(parts[1], "-", ".")
}

// runtimeVersion is the numeric half of a runtime name — "iOS 17.4" → "17.4" —
// which is what Appium's platformVersion capability wants.
func runtimeVersion(runtime string) string {
	fields := strings.Fields(runtime)
	if len(fields) < 2 {
		return ""
	}
	return fields[len(fields)-1]
}

// listAVDs lists the Android virtual devices this Mac has defined.
func listAVDs(c *call) ([]string, *rpcError) {
	raw, callErr := mobileRun(c, c.cfg.emulatorBin, "emulator", mobileListTimeout, "-list-avds")
	if callErr != nil {
		return nil, callErr
	}
	out := []string{}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// The emulator binary prints diagnostics ("INFO | storing crashdata…")
		// onto the same stream as the names on some SDK versions. Filtering by
		// the name grammar rather than by known prefixes means a new diagnostic
		// does not become a device nobody can boot.
		if line == "" || avdName.FindString(line) != line {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// listRunningEmulators pairs every running emulator with its AVD name.
//
// Asked of each serial rather than derived from the console port, because the
// port is allocated in start order: an AVD that was restarted is on a different
// one, and remembering 5554 would drive whichever emulator happens to be there
// now. A serial whose name cannot be read is dropped rather than reported under
// a guess.
func listRunningEmulators(c *call) ([]runningEmulator, *rpcError) {
	serials, callErr := adbEmulatorSerials(c)
	if callErr != nil {
		return nil, callErr
	}
	out := []runningEmulator{}
	for _, serial := range serials {
		name, err := emulatorAVDName(c, serial)
		if err != nil {
			log.Debug().Str("call", c.id).Str("serial", serial).Str("why", err.Message).
				Msg("asking an emulator its AVD name failed")
			continue
		}
		if name == "" {
			continue
		}
		out = append(out, runningEmulator{AVD: name, Serial: serial})
	}
	return out, nil
}

// adbEmulatorSerials is every emulator adb currently has fully attached.
//
// Only the fully-attached ones: a serial in "offline" or "unauthorized" is a
// device Appium cannot create a session against, and reporting it as running
// would hand a run a dead device. Physical serials are filtered by the grammar
// rather than by a device-type lookup — they belong to the bridge's path.
func adbEmulatorSerials(c *call) ([]string, *rpcError) {
	raw, callErr := mobileRun(c, c.cfg.adbBin, "adb", mobileListTimeout, "devices")
	if callErr != nil {
		return nil, callErr
	}
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[1] != "device" {
			continue
		}
		if emulatorSerial.MatchString(fields[0]) {
			out = append(out, fields[0])
		}
	}
	return out, nil
}

// emulatorAVDName asks one emulator what it is. `adb emu avd name` answers with
// the name and then a line saying OK, which is the console protocol's
// acknowledgement rather than part of the answer.
func emulatorAVDName(c *call, serial string) (string, *rpcError) {
	if !emulatorSerial.MatchString(serial) {
		return "", failure(codeInternal, "%q is not an emulator serial", serial)
	}
	raw, callErr := mobileRun(c, c.cfg.adbBin, "adb", mobileListTimeout, "-s", serial, "emu", "avd", "name")
	if callErr != nil {
		return "", callErr
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" {
			continue
		}
		return line, nil
	}
	return "", nil
}

// serialForAVD finds which serial, if any, the named AVD is running on.
func serialForAVD(c *call, avd string) (string, *rpcError) {
	running, callErr := listRunningEmulators(c)
	if callErr != nil {
		return "", callErr
	}
	for _, e := range running {
		if e.AVD == avd {
			return e.Serial, nil
		}
	}
	return "", nil
}

// --- booting and shutting down ----------------------------------------------

type mobileDeviceParams struct {
	// Kind is `ios_simulator` or `android_emulator`. `remote_adb` is refused by
	// name: a physical phone is the bridge's path and never this one.
	Kind string `json:"kind"`
	// ID is the STABLE identity of the device, in whatever terms its kind makes
	// that question answerable — the simctl UDID for a simulator, the AVD name
	// for an emulator. It is never the adb serial, which is allocated at boot.
	ID string `json:"id"`
	// TimeoutMS bounds the wait for the device to become usable. Zero means the
	// default; boot only.
	TimeoutMS int64 `json:"timeout_ms,omitempty"`
}

// mobileBootResult is everything the caller needs to create an Appium session.
//
// `udid` is the field that matters and the reason this call exists: what Appium
// is finally handed is NOT what the caller asked for. A simulator's identity and
// its Appium udid are the same string, and an emulator's are not — an AVD is a
// name, and the serial it comes up on is allocated by the emulator at boot. A
// caller that guessed `emulator-5554` would drive whichever emulator happens to
// be on that console port.
type mobileBootResult struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// UDID is `appium:udid`, verbatim.
	UDID string `json:"udid"`
	// PlatformVersion is `appium:platformVersion`, when this Mac knows it.
	PlatformVersion string `json:"platform_version,omitempty"`
	// Name is what a person calls the device, when there is one to report.
	Name string `json:"name,omitempty"`
	// AlreadyBooted says nothing was started — the device was up when the call
	// arrived. Reported because "booted in 4ms" is otherwise indistinguishable
	// from a boot that did not happen.
	AlreadyBooted bool `json:"already_booted"`
	// DurationMS is how long the wait took, so a caller can tell a warm device
	// from a cold one without timing the request itself.
	DurationMS int64 `json:"duration_ms"`
}

type mobileShutdownResult struct {
	V          int    `json:"v"`
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	WasRunning bool   `json:"was_running"`
}

// checkDeviceParams refuses everything before anything is spawned.
func checkDeviceParams(c *call, method string) (mobileDeviceParams, *rpcError) {
	var p mobileDeviceParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return p, failure(codeBadRequest, "%s params are not the expected object: %v", method, err)
	}
	p.Kind = strings.TrimSpace(p.Kind)
	p.ID = strings.TrimSpace(p.ID)

	switch p.Kind {
	case kindIOSSimulator:
		if c.cfg.xcrunBin == "" {
			return p, failure(codeNotReady, "this machine has no Xcode command line tools (xcrun), so it has no iOS simulators")
		}
		if !simulatorUDID.MatchString(p.ID) {
			return p, failure(codeBadRequest, "id %q is not a simulator UDID — take it from mobile.devices", p.ID)
		}
	case kindAndroidEmulator:
		if c.cfg.adbBin == "" {
			return p, failure(codeNotReady, "this machine has no Android SDK platform-tools (adb), so it cannot reach an emulator")
		}
		if !avdName.MatchString(p.ID) {
			return p, failure(codeBadRequest, "id %q is not an AVD name — letters, digits, dot, dash and underscore only, and it may not begin with a dash", p.ID)
		}
	case kindRemoteADB:
		// Refused by NAME rather than as an unknown value, because it is a real
		// kind that is handled somewhere else. A message reading "unknown kind"
		// would send somebody hunting for a typo.
		return p, failure(codeBadRequest,
			"%s is a physical phone reached through the cluster's adb bridge; it is not attached to this machine and is not this runner's to boot", kindRemoteADB)
	case "":
		return p, failure(codeBadRequest, "kind is required: %s or %s", kindIOSSimulator, kindAndroidEmulator)
	default:
		return p, failure(codeBadRequest, "kind %q is not one this machine drives (%s, %s)", p.Kind, kindIOSSimulator, kindAndroidEmulator)
	}
	return p, nil
}

func bootTimeout(p mobileDeviceParams) (time.Duration, *rpcError) {
	if p.TimeoutMS == 0 {
		return mobileDefaultBootTimeout, nil
	}
	d := time.Duration(p.TimeoutMS) * time.Millisecond
	if d < mobileBootPoll || d > mobileMaxBootTimeout {
		return 0, failure(codeBadRequest, "timeout_ms %d is outside %d..%d", p.TimeoutMS,
			mobileBootPoll.Milliseconds(), mobileMaxBootTimeout.Milliseconds())
	}
	return d, nil
}

// mobileBoot brings a device up and reports what Appium should be handed.
//
// Idempotent, and that is what makes it safe to call at the start of every run:
// a device that is already up is a success with `already_booted`, not a
// conflict. The wait is not optional — `simctl boot` returns as soon as the boot
// has been STARTED, and a session created against a half-booted simulator fails
// in ways that read to an agent as a broken app.
func mobileBoot(c *call) (any, *rpcError) {
	p, callErr := checkDeviceParams(c, "mobile.boot")
	if callErr != nil {
		return nil, callErr
	}
	deadline, callErr := bootTimeout(p)
	if callErr != nil {
		return nil, callErr
	}

	// A boot is what precedes a session, so the hub comes up beside the
	// device: a cold simulator and a cold XCUITest driver each take long
	// enough that paying for them one after the other is noticeable.
	hub := c.state.appiumHub()
	release := hub.hold()
	defer release()
	hubReady := hub.ensureAsync(c.ctx)

	started := time.Now()
	var result mobileBootResult
	if p.Kind == kindIOSSimulator {
		result, callErr = bootSimulator(c, p.ID, deadline)
	} else {
		result, callErr = bootEmulator(c, p.ID, deadline)
	}
	if callErr != nil {
		return nil, callErr
	}
	// Idempotent, so the caller's retry after this is cheap: the device answers
	// already_booted and only the hub is asked again.
	if hubErr := <-hubReady; hubErr != nil {
		return nil, failure(hubErr.Code, "%s %s is up, but %s", p.Kind, p.ID, hubErr.Message)
	}
	result.V = protocolVersion
	result.Kind = p.Kind
	result.ID = p.ID
	result.DurationMS = time.Since(started).Milliseconds()
	log.Info().Str("call", c.id).Str("kind", p.Kind).Str("id", p.ID).Str("udid", result.UDID).
		Bool("already", result.AlreadyBooted).Dur("took", time.Since(started)).Msg("mobile device booted")
	return result, nil
}

func bootSimulator(c *call, udid string, deadline time.Duration) (mobileBootResult, *rpcError) {
	sim, found, callErr := findSimulator(c, udid)
	if callErr != nil {
		return mobileBootResult{}, callErr
	}
	if !found {
		return mobileBootResult{}, failure(codeBadRequest, "this machine has no simulator with UDID %s", udid)
	}

	out := mobileBootResult{
		UDID:            sim.UDID,
		Name:            sim.Name,
		PlatformVersion: sim.PlatformVersion,
		AlreadyBooted:   strings.EqualFold(sim.State, "Booted"),
	}
	if !out.AlreadyBooted {
		if _, callErr := mobileRun(c, c.cfg.xcrunBin, "xcrun", mobileActionTimeout, "simctl", "boot", udid); callErr != nil {
			// A device that came up between the check and the call is a race
			// with the user's own Simulator.app, not a failure.
			if !strings.Contains(strings.ToLower(callErr.Message), "current state: booted") {
				return mobileBootResult{}, callErr
			}
			out.AlreadyBooted = true
		}
	}

	// `bootstatus -b` is the wait, and it is idempotent — which is why an
	// already-booted device goes through it too rather than being trusted on
	// the strength of a word simctl printed a moment ago.
	//
	// Simulator.app is deliberately NOT opened. The boot was asked for by the
	// cloud on behalf of a task, and stealing focus on somebody's desktop every
	// time a QA task starts is the same problem `-no-window` avoids for the
	// emulator.
	if _, callErr := mobileRun(c, c.cfg.xcrunBin, "xcrun", deadline, "simctl", "bootstatus", udid, "-b"); callErr != nil {
		return mobileBootResult{}, callErr
	}
	return out, nil
}

func findSimulator(c *call, udid string) (localSimulator, bool, *rpcError) {
	sims, callErr := listSimulators(c)
	if callErr != nil {
		return localSimulator{}, false, callErr
	}
	for _, s := range sims {
		if strings.EqualFold(s.UDID, udid) {
			return s, true, nil
		}
	}
	return localSimulator{}, false, nil
}

func bootEmulator(c *call, avd string, deadline time.Duration) (mobileBootResult, *rpcError) {
	serial, callErr := serialForAVD(c, avd)
	if callErr != nil {
		return mobileBootResult{}, callErr
	}
	out := mobileBootResult{Name: avd, AlreadyBooted: serial != ""}

	if serial == "" {
		if c.cfg.emulatorBin == "" {
			return mobileBootResult{}, failure(codeNotReady,
				"%q is not running and this machine has no `emulator` binary to start it with", avd)
		}
		known, callErr := listAVDs(c)
		if callErr != nil {
			return mobileBootResult{}, callErr
		}
		if !slices.Contains(known, avd) {
			return mobileBootResult{}, failure(codeBadRequest, "this machine has no AVD called %q", avd)
		}
		if err := startEmulator(c, avd); err != nil {
			return mobileBootResult{}, err
		}
		if serial, callErr = waitForSerial(c, avd, deadline); callErr != nil {
			return mobileBootResult{}, callErr
		}
	}

	if callErr := waitForAndroidBoot(c, serial, deadline); callErr != nil {
		return mobileBootResult{}, callErr
	}
	out.UDID = serial
	// Best effort: the version makes the capability set explicit and its absence
	// costs nothing, so a getprop that fails is not a boot that failed.
	if release, callErr := mobileRun(c, c.cfg.adbBin, "adb", mobileListTimeout,
		"-s", serial, "shell", "getprop", "ro.build.version.release"); callErr == nil {
		out.PlatformVersion = strings.TrimSpace(release)
	}
	return out, nil
}

// startEmulator is the ONE child this program spawns and does not reap with the
// call, and the exception is deliberate.
//
// `emulator -avd X` is not a command that returns once the device is up: it is
// the process that IS the emulator, and it has to outlive the request that
// asked for it — that is the whole point of booting one. Tying it to the call's
// context would kill the device the moment the caller had its answer. Tying it
// to this process would be worse in a subtler way: a thirty-second network drop
// restarts the runner, and a device a parked task is waiting on would be
// destroyed by a reconnect. An emulator's lifetime belongs to the device, not to
// the tunnel.
//
// What it still gets is its own process group — not so this program can kill it,
// but so a signal aimed at the runner's group (the supervisor's SIGTERM on quit)
// does not take a booting emulator down with it; on Windows it also breaks away
// from any job this process is in (see startDetached). It is released by
// `mobile.shutdown`, by the user, or by a reboot, and by nothing else here.
//
// Headless, for the same reason a simulator's window is not opened: the device
// is a fixture an agent drives, and a window that steals focus every time a QA
// task starts is somebody's afternoon.
func startEmulator(c *call, avd string) *rpcError {
	cmd, err := startDetached(func() *exec.Cmd {
		cmd := exec.Command(c.cfg.emulatorBin, "-avd", avd, "-no-window", "-no-audio")
		// This process's own environment, which carries no credential by
		// construction: the runner is configured on stdin precisely because a
		// same-user process can read another's environment on macOS. What the
		// emulator needs from it is PATH, HOME and the ANDROID_* roots the
		// desktop app set from the adb it actually found.
		cmd.Env = os.Environ()
		// Discarded rather than inherited: the emulator is chatty, and
		// interleaving its log into this process's structured output makes both
		// unreadable. Nothing here opens a pipe it would then have to keep
		// reading.
		cmd.Stdout, cmd.Stderr = nil, nil
		return cmd
	})
	if err != nil {
		return failure(codeInternal, "could not start the emulator: %v", err)
	}
	pid := cmd.Process.Pid
	// Reaped so the process table does not fill with zombies on a Mac that
	// starts and stops emulators all day. If this program exits first the
	// emulator is reparented, which is the intended ending.
	go func() { _ = cmd.Wait() }()
	log.Info().Str("call", c.id).Str("avd", avd).Int("pid", pid).Msg("emulator starting")
	return nil
}

// waitForSerial polls until adb sees the AVD.
//
// Polling rather than `adb wait-for-device`, because that waits for ANY device:
// on a Mac with a phone plugged in or a second emulator already up it returns
// immediately and the caller carries on with the wrong serial.
func waitForSerial(c *call, avd string, deadline time.Duration) (string, *rpcError) {
	until := time.Now().Add(deadline)
	for {
		serial, callErr := serialForAVD(c, avd)
		if callErr != nil {
			return "", callErr
		}
		if serial != "" {
			return serial, nil
		}
		if time.Now().After(until) {
			return "", failure(codeUpstream, "emulator %q did not appear in adb within %s", avd, deadline)
		}
		if callErr := mobileSleep(c, mobileBootPoll); callErr != nil {
			return "", callErr
		}
	}
}

// waitForAndroidBoot waits for the system to finish coming up, not merely for
// adb to answer. adb answers while Android is still on the boot animation, and
// a session created then fails against an app that is not installed yet.
func waitForAndroidBoot(c *call, serial string, deadline time.Duration) *rpcError {
	if !emulatorSerial.MatchString(serial) {
		return failure(codeInternal, "%q is not an emulator serial", serial)
	}
	until := time.Now().Add(deadline)
	for {
		out, callErr := mobileRun(c, c.cfg.adbBin, "adb", mobileListTimeout,
			"-s", serial, "shell", "getprop", "sys.boot_completed")
		if callErr == nil && strings.TrimSpace(out) == "1" {
			return nil
		}
		if time.Now().After(until) {
			return failure(codeUpstream, "emulator %s did not finish booting within %s", serial, deadline)
		}
		if callErr := mobileSleep(c, mobileBootPoll); callErr != nil {
			return callErr
		}
	}
}

// mobileShutdown puts a device back down.
//
// Idempotent in the other direction: a device that is already down is a success
// with `was_running:false`. The caller's intent is a state, not a transition,
// and a 4xx for "it is already how you wanted it" would make callers treat a
// success as a fault.
func mobileShutdown(c *call) (any, *rpcError) {
	p, callErr := checkDeviceParams(c, "mobile.shutdown")
	if callErr != nil {
		return nil, callErr
	}
	out := mobileShutdownResult{V: protocolVersion, Kind: p.Kind, ID: p.ID}

	if p.Kind == kindIOSSimulator {
		sim, found, callErr := findSimulator(c, p.ID)
		if callErr != nil {
			return nil, callErr
		}
		if !found {
			return nil, failure(codeBadRequest, "this machine has no simulator with UDID %s", p.ID)
		}
		out.WasRunning = !strings.EqualFold(sim.State, "Shutdown")
		if !out.WasRunning {
			return out, nil
		}
		if _, callErr := mobileRun(c, c.cfg.xcrunBin, "xcrun", mobileActionTimeout, "simctl", "shutdown", p.ID); callErr != nil {
			if !strings.Contains(strings.ToLower(callErr.Message), "current state: shutdown") {
				return nil, callErr
			}
		}
		log.Info().Str("call", c.id).Str("kind", p.Kind).Str("id", p.ID).Msg("mobile device shut down")
		return out, nil
	}

	serial, callErr := serialForAVD(c, p.ID)
	if callErr != nil {
		return nil, callErr
	}
	if serial == "" {
		return out, nil
	}
	out.WasRunning = true
	// The serial came from `adb devices` and matched the emulator grammar on
	// the way in; it is checked again here because this is the one command in
	// the file that ends a process, and the thing it must never end is a phone
	// somebody plugged into their laptop.
	if !emulatorSerial.MatchString(serial) {
		return nil, failure(codeInternal, "%q is not an emulator serial", serial)
	}
	if _, callErr := mobileRun(c, c.cfg.adbBin, "adb", mobileActionTimeout, "-s", serial, "emu", "kill"); callErr != nil {
		return nil, callErr
	}
	log.Info().Str("call", c.id).Str("kind", p.Kind).Str("id", p.ID).Str("serial", serial).Msg("mobile device shut down")
	return out, nil
}

// --- the Appium proxy -------------------------------------------------------

// proxyAppium forwards one call to the local Appium hub and passes its answer
// back untouched.
//
// It is a proxy in the same sense `embeddings.create` is: the status, the
// content type and the body are the hub's own. That is not laziness, it is the
// contract — Appium's error bodies carry W3C error codes the caller classifies,
// and the one that matters most is the refusal of a second session against a
// device that already has one. Rewriting a 4xx from the hub into a failure of
// this Mac's would turn "the device is busy, park the task" into "something
// broke", which is the difference between a task that resumes and one that
// fails.
//
// Concurrency is deliberately unmanaged. Two calls for two devices are two
// calls; two calls for the SAME device are two calls as well, and the second
// one gets the hub's refusal — which is exactly what makes the lease hold
// across the cloud's replicas. A semaphore here would replace a cross-process
// lock with a per-Mac one.
func (s *runnerServer) proxyAppium(w http.ResponseWriter, r *http.Request) {
	id := newCallID()
	if s.cfg.appiumBaseURL == "" {
		writeError(w, failure(codeNotReady,
			"this machine has no Appium; install it and reconnect (this runner starts a hub when it is installed)"))
		return
	}

	// The path after the prefix, verbatim — session ids, element ids and
	// attribute names are all in there and none of them is this program's to
	// normalise. What is refused is a traversal segment: an Appium path never
	// contains one, and a base URL a caller can walk out of is a base URL that
	// is not a base.
	//
	// The ESCAPED path, and it is required to carry the prefix in that form
	// too. The router matched on `r.URL.Path`, which is decoded, so
	// `/mobile%2Eappium/status` reaches here — and trimming a prefix that is
	// only present after decoding would forward the whole undecoded path to the
	// hub as if it were a route. Requiring both forms to agree is what makes
	// the routing table and the proxy describe the same set of paths.
	escaped := r.URL.EscapedPath()
	if !strings.HasPrefix(escaped, appiumPrefix) {
		writeError(w, failure(codeUnsupportedMethod, "this runner has no %q", r.URL.Path))
		return
	}
	rest := strings.TrimPrefix(escaped, appiumPrefix)
	if rest == "" {
		rest = "/"
	}
	if !strings.HasPrefix(rest, "/") {
		writeError(w, failure(codeUnsupportedMethod, "this runner has no %q", r.URL.Path))
		return
	}
	for _, segment := range strings.Split(rest, "/") {
		if segment == ".." {
			writeError(w, failure(codeBadRequest, "the Appium path may not contain a %q segment", ".."))
			return
		}
	}

	target := s.cfg.appiumBaseURL + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	parsed, err := url.Parse(target)
	if err != nil {
		writeError(w, failure(codeBadRequest, "the Appium path does not make a URL: %v", err))
		return
	}
	// Belt to the traversal check's braces. The base is fixed and loopback, and
	// this asserts that nothing in the caller's path moved it — a property worth
	// checking rather than reasoning about, because it is the one that keeps
	// this from being a request forwarder.
	base, err := url.Parse(s.cfg.appiumBaseURL)
	if err != nil || parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		writeError(w, failure(codeBadRequest, "the Appium path may not name another host"))
		return
	}

	body, readErr := readBody(r)
	if readErr != nil {
		writeError(w, readErr)
		return
	}

	// After every refusal above, so a malformed call starts nothing. Held for
	// the whole call: a long `POST /session` is use of the hub until it ends.
	hub := s.state.appiumHub()
	release := hub.hold()
	defer release()
	if callErr := hub.ensure(r.Context()); callErr != nil {
		writeError(w, callErr)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), appiumProxyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, parsed.String(), strings.NewReader(string(body)))
	if err != nil {
		writeError(w, failure(codeInternal, "building the Appium request: %v", err))
		return
	}
	// Two headers, and no more. The hub is a loopback process this Mac started;
	// it holds no credential and has no use for one, so an Authorization the
	// caller sent for the tunnel is deliberately NOT forwarded — handing a cloud
	// bearer token to a local process is a place for it to be logged that
	// nobody chose.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			writeError(w, failure(codeCancelled, "the call was cancelled"))
			return
		}
		writeError(w, failure(codeUpstream, "could not reach Appium at %s — is the hub running? (%v)", s.cfg.appiumBaseURL, err))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, appiumResponseLimit)); err != nil {
		// The status is long gone, so this can only be logged. It means either
		// the caller stopped reading or the hub stopped writing, and both are
		// visible to the caller as a truncated body.
		log.Debug().Str("call", id).Err(err).Msg("the Appium answer was cut short")
	}
}

// --- plumbing ---------------------------------------------------------------

// mobileRun runs one toolchain command and turns its failure into an answer.
//
// `label` is the tool's own name rather than its path, because the path is this
// Mac's business and the name is what the message has to say. Everything a
// caller supplied has already been through a grammar by the time it reaches
// here; the argv array and the absence of a shell are the second lock.
func mobileRun(c *call, bin, label string, timeout time.Duration, args ...string) (string, *rpcError) {
	if bin == "" {
		return "", failure(codeNotReady, "this machine has no %s", label)
	}
	out, err := runTool(c.ctx, bin, "", nil, timeout, args...)
	if err == nil {
		return out.stdout, nil
	}
	if errors.Is(err, errStartFailed) {
		return "", failure(codeInternal, "could not start %s: %v", label, err)
	}
	if out.timedOut {
		return "", failure(codeUpstream, "%s %s took longer than %s", label, args[0], timeout)
	}
	if c.ctx.Err() != nil {
		return "", failure(codeCancelled, "the call was cancelled")
	}
	// The tool's own words: they name the actual problem — a device in the
	// wrong state, an SDK that is not there, an emulator that will not start —
	// and a sentence of ours would lose that. simctl writes them to stderr;
	// adb sometimes writes them to stdout, so both are considered.
	message := strings.TrimSpace(out.stderr)
	if message == "" {
		message = strings.TrimSpace(out.stdout)
	}
	if message == "" {
		message = err.Error()
	}
	return "", failure(codeUpstream, "%s %s failed: %s", label, args[0], firstNonEmptyLine(message))
}

// mobileSleep waits, or reports that the caller went away. A poll loop that
// ignored the context would keep asking adb about a device nobody is waiting
// for, for as long as the deadline allows.
func mobileSleep(c *call, d time.Duration) *rpcError {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return failure(codeCancelled, "the call was cancelled")
	case <-timer.C:
		return nil
	}
}
