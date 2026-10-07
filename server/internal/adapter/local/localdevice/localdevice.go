package localdevice

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

const (
	listTimeout         = 30 * time.Second
	actionTimeout       = 90 * time.Second
	defaultBootTimeout  = 4 * time.Minute
	defaultPollInterval = 2 * time.Second
)

var simulatorUDIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

var avdNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)

var emulatorSerialPattern = regexp.MustCompile(`^emulator-[0-9]{1,6}$`)

func ValidateSimulatorUDID(udid string) error {
	if !simulatorUDIDPattern.MatchString(udid) {
		return fmt.Errorf("%q is not a simulator UDID — copy it from the local device catalog", udid)
	}
	return nil
}

func ValidateAVDName(name string) error {
	if !avdNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not an AVD name — letters, digits, dot, dash and underscore only", name)
	}
	return nil
}

func ValidateEmulatorSerial(serial string) error {
	if !emulatorSerialPattern.MatchString(serial) {
		return fmt.Errorf("%q is not an emulator serial", serial)
	}
	return nil
}

type Config struct {
	GOOS         string
	Xcrun        string
	ADB          string
	Emulator     string
	BootTimeout  time.Duration
	PollInterval time.Duration
}

type Host struct {
	goos     string
	xcrun    string
	adb      string
	emulator string

	bootTimeout  time.Duration
	pollInterval time.Duration
}

func New(cfg Config) *Host {
	goos := cfg.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	h := &Host{goos: goos, bootTimeout: cfg.BootTimeout, pollInterval: cfg.PollInterval}
	if h.bootTimeout <= 0 {
		h.bootTimeout = defaultBootTimeout
	}
	if h.pollInterval <= 0 {
		h.pollInterval = defaultPollInterval
	}

	if goos == "darwin" {
		h.xcrun = resolve(cfg.Xcrun, "xcrun")
	}
	home, _ := os.UserHomeDir()
	h.adb = resolve(cfg.ADB, "adb", androidSDKPaths(goos, os.Getenv, home, "platform-tools", "adb")...)
	h.emulator = resolve(cfg.Emulator, "emulator", androidSDKPaths(goos, os.Getenv, home, "emulator", "emulator")...)

	log.Debug().
		Str("goos", goos).
		Bool("xcrun", h.xcrun != "").
		Bool("adb", h.adb != "").
		Bool("emulator", h.emulator != "").
		Msg("localdevice: host toolchain resolved")
	return h
}

func resolve(explicit, name string, fallbacks ...string) string {
	if explicit != "" {
		if p, err := exec.LookPath(explicit); err == nil {
			return p
		}

		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, candidate := range fallbacks {
		if info, err := os.Stat(candidate); err == nil && isExecutable(runtime.GOOS, candidate, info.Mode()) {
			return candidate
		}
	}
	return ""
}

// isExecutable takes the OS whose filesystem the file is on: Windows has no
// execute bit, so there the extension is what makes a file runnable.
func isExecutable(goos, path string, mode fs.FileMode) bool {
	if !mode.IsRegular() {
		return false
	}
	if goos == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd":
			return true
		}
		return false
	}
	return mode&0o111 != 0
}

// androidSDKPaths lists where a tool sits in the SDKs this host may have: the
// roots the environment names, then the one Android Studio installs to.
func androidSDKPaths(goos string, getenv func(string) string, home, dir, name string) []string {
	var roots []string
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := strings.TrimSpace(getenv(env)); v != "" {
			roots = append(roots, v)
		}
	}
	switch goos {
	case "darwin":
		if home != "" {
			roots = append(roots, filepath.Join(home, "Library", "Android", "sdk"))
		}
	case "windows":
		name += ".exe"
		if local := strings.TrimSpace(getenv("LOCALAPPDATA")); local != "" {
			roots = append(roots, filepath.Join(local, "Android", "Sdk"))
		} else if home != "" {
			roots = append(roots, filepath.Join(home, "AppData", "Local", "Android", "Sdk"))
		}
	default:
		if home != "" {
			roots = append(roots, filepath.Join(home, "Android", "Sdk"))
		}
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, dir, name))
	}
	return out
}

func (h *Host) SupportsIOSSimulators(context.Context) bool {
	return h != nil && h.goos == "darwin" && h.xcrun != ""
}

func (h *Host) SupportsAndroidEmulators(ctx context.Context) bool {
	if h == nil || h.adb == "" {
		return false
	}
	if h.emulator != "" {
		return true
	}
	serials, err := h.RunningEmulators(ctx)
	return err == nil && len(serials) > 0
}

func (h *Host) Catalog(ctx context.Context) (domain.LocalDeviceCatalog, error) {
	out := domain.EmptyLocalDeviceCatalog()
	if h == nil {
		return out, nil
	}
	if h.SupportsIOSSimulators(ctx) {
		sims, err := h.Simulators(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("localdevice: listing simulators failed")
		} else {
			out.IOS = sims
		}
	}
	if h.adb != "" || h.emulator != "" {
		avds, err := h.AVDs(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("localdevice: listing AVDs failed")
		} else {
			out.Android = avds
		}
	}
	return out, nil
}

type simctlList struct {
	Devices map[string][]struct {
		UDID        string `json:"udid"`
		Name        string `json:"name"`
		State       string `json:"state"`
		IsAvailable bool   `json:"isAvailable"`
	} `json:"devices"`
}

func (h *Host) Simulators(ctx context.Context) ([]domain.LocalSimulator, error) {
	if h == nil || h.xcrun == "" {
		return []domain.LocalSimulator{}, nil
	}
	raw, err := h.output(ctx, listTimeout, h.xcrun, "simctl", "list", "devices", "--json")
	if err != nil {
		return nil, err
	}
	var parsed simctlList
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode simctl device list: %w", err)
	}
	out := []domain.LocalSimulator{}
	for runtimeID, devices := range parsed.Devices {
		name := runtimeName(runtimeID)
		for _, d := range devices {
			if !d.IsAvailable || d.UDID == "" {
				continue
			}
			out = append(out, domain.LocalSimulator{
				UDID:    d.UDID,
				Name:    d.Name,
				Runtime: name,
				State:   d.State,
			})
		}
	}

	sortSimulators(out)
	return out, nil
}

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

func RuntimeVersion(runtime string) string {
	fields := strings.Fields(runtime)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func sortSimulators(sims []domain.LocalSimulator) {

	for i := 1; i < len(sims); i++ {
		for j := i; j > 0 && lessSimulator(sims[j], sims[j-1]); j-- {
			sims[j], sims[j-1] = sims[j-1], sims[j]
		}
	}
}

func lessSimulator(a, b domain.LocalSimulator) bool {
	if a.Runtime != b.Runtime {
		return a.Runtime < b.Runtime
	}
	return a.Name < b.Name
}

// Simulator finds one by UDID.
func (h *Host) Simulator(ctx context.Context, udid string) (domain.LocalSimulator, bool, error) {
	if err := ValidateSimulatorUDID(udid); err != nil {
		return domain.LocalSimulator{}, false, err
	}
	sims, err := h.Simulators(ctx)
	if err != nil {
		return domain.LocalSimulator{}, false, err
	}
	for _, s := range sims {
		if strings.EqualFold(s.UDID, udid) {
			return s, true, nil
		}
	}
	return domain.LocalSimulator{}, false, nil
}

func (h *Host) SimulatorBooted(ctx context.Context, udid string) (bool, error) {
	sim, ok, err := h.Simulator(ctx, udid)
	if err != nil || !ok {
		return false, err
	}
	return strings.EqualFold(sim.State, "Booted"), nil
}

func (h *Host) BootSimulator(ctx context.Context, udid string) error {
	if h == nil || h.xcrun == "" {
		return fmt.Errorf("this host has no iOS simulators")
	}
	if err := ValidateSimulatorUDID(udid); err != nil {
		return err
	}
	booted, err := h.SimulatorBooted(ctx, udid)
	if err != nil {
		return err
	}
	if !booted {
		if _, err := h.output(ctx, actionTimeout, h.xcrun, "simctl", "boot", udid); err != nil {

			if !strings.Contains(strings.ToLower(err.Error()), "current state: booted") {
				return fmt.Errorf("boot simulator: %w", err)
			}
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, h.bootTimeout)
	defer cancel()
	if _, err := h.output(waitCtx, h.bootTimeout, h.xcrun, "simctl", "bootstatus", udid, "-b"); err != nil {
		return fmt.Errorf("wait for simulator boot: %w", err)
	}
	return nil
}

func (h *Host) ShutdownSimulator(ctx context.Context, udid string) error {
	if h == nil || h.xcrun == "" {
		return fmt.Errorf("this host has no iOS simulators")
	}
	if err := ValidateSimulatorUDID(udid); err != nil {
		return err
	}
	if _, err := h.output(ctx, actionTimeout, h.xcrun, "simctl", "shutdown", udid); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "current state: shutdown") {
			return nil
		}
		return fmt.Errorf("shutdown simulator: %w", err)
	}
	return nil
}

func (h *Host) AVDs(ctx context.Context) ([]string, error) {
	if h == nil || h.emulator == "" {
		return []string{}, nil
	}
	raw, err := h.output(ctx, listTimeout, h.emulator, "-list-avds")
	if err != nil {
		return nil, err
	}
	out := []string{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || avdNamePattern.FindString(line) != line {
			continue
		}
		out = append(out, line)
	}
	return out, scanner.Err()
}

func (h *Host) AVDExists(ctx context.Context, name string) (bool, error) {
	if err := ValidateAVDName(name); err != nil {
		return false, err
	}
	avds, err := h.AVDs(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range avds {
		if a == name {
			return true, nil
		}
	}

	if h.emulator == "" {
		serial, serr := h.EmulatorSerial(ctx, name)
		return serial != "", serr
	}
	return false, nil
}

func (h *Host) RunningEmulators(ctx context.Context) ([]string, error) {
	if h == nil || h.adb == "" {
		return nil, nil
	}
	raw, err := h.output(ctx, listTimeout, h.adb, "devices")
	if err != nil {
		return nil, err
	}
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		if len(fields) < 2 || fields[1] != "device" {
			continue
		}
		if emulatorSerialPattern.MatchString(fields[0]) {
			out = append(out, fields[0])
		}
	}
	return out, scanner.Err()
}

func (h *Host) EmulatorSerial(ctx context.Context, avd string) (string, error) {
	if err := ValidateAVDName(avd); err != nil {
		return "", err
	}
	serials, err := h.RunningEmulators(ctx)
	if err != nil {
		return "", err
	}
	for _, serial := range serials {
		name, err := h.emulatorAVDName(ctx, serial)
		if err != nil {
			log.Debug().Err(err).Str("serial", serial).Msg("localdevice: asking an emulator its AVD name failed")
			continue
		}
		if name == avd {
			return serial, nil
		}
	}
	return "", nil
}

func (h *Host) emulatorAVDName(ctx context.Context, serial string) (string, error) {
	if err := ValidateEmulatorSerial(serial); err != nil {
		return "", err
	}
	raw, err := h.output(ctx, listTimeout, h.adb, "-s", serial, "emu", "avd", "name")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" {
			continue
		}
		return line, nil
	}
	return "", nil
}

func (h *Host) StartEmulator(ctx context.Context, avd string) (string, error) {
	return h.LaunchEmulator(ctx, avd, false)
}

// LaunchEmulator is StartEmulator with a choice of window: Appium drives a
// headless one, a person trying a build needs to see it. An AVD that is already
// running is used as it is.
func (h *Host) LaunchEmulator(ctx context.Context, avd string, windowed bool) (string, error) {
	if h == nil || h.adb == "" {
		return "", fmt.Errorf("this host has no Android SDK platform-tools (adb)")
	}
	if err := ValidateAVDName(avd); err != nil {
		return "", err
	}
	serial, err := h.EmulatorSerial(ctx, avd)
	if err != nil {
		return "", err
	}
	if serial == "" {
		if h.emulator == "" {
			return "", fmt.Errorf("%q is not running and this host has no emulator binary to start it with", avd)
		}
		if err := h.spawnEmulator(avd, windowed); err != nil {
			return "", err
		}
		if serial, err = h.waitForSerial(ctx, avd); err != nil {
			return "", err
		}
	}
	if err := h.waitForBoot(ctx, serial); err != nil {
		return "", err
	}
	return serial, nil
}

func (h *Host) spawnEmulator(avd string, windowed bool) error {
	args := []string{"-avd", avd, "-no-window", "-no-audio"}
	if windowed {
		args = []string{"-avd", avd, "-no-audio"}
	}
	cmd := exec.Command(h.emulator, args...)
	cmd.Env = childenv.For(os.Environ(), nil)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start emulator %q: %w", avd, err)
	}
	go func() { _ = cmd.Wait() }()
	log.Info().Str("avd", avd).Int("pid", cmd.Process.Pid).Msg("localdevice: emulator starting")
	return nil
}

func (h *Host) waitForSerial(ctx context.Context, avd string) (string, error) {
	deadline := time.Now().Add(h.bootTimeout)
	for {
		serial, err := h.EmulatorSerial(ctx, avd)
		if err != nil {
			return "", err
		}
		if serial != "" {
			return serial, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("emulator %q did not appear in adb within %s", avd, h.bootTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(h.pollInterval):
		}
	}
}

func (h *Host) waitForBoot(ctx context.Context, serial string) error {
	if err := ValidateEmulatorSerial(serial); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, h.bootTimeout)
	defer cancel()
	if _, err := h.output(waitCtx, h.bootTimeout, h.adb, "-s", serial, "wait-for-device"); err != nil {
		return fmt.Errorf("wait for %s: %w", serial, err)
	}
	deadline := time.Now().Add(h.bootTimeout)
	for {
		raw, err := h.output(waitCtx, listTimeout, h.adb, "-s", serial, "shell", "getprop", "sys.boot_completed")
		if err == nil && strings.TrimSpace(string(raw)) == "1" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("emulator %s did not finish booting within %s", serial, h.bootTimeout)
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(h.pollInterval):
		}
	}
}

func (h *Host) StopEmulator(ctx context.Context, serial string) error {
	if h == nil || h.adb == "" {
		return fmt.Errorf("this host has no Android SDK platform-tools (adb)")
	}
	if err := ValidateEmulatorSerial(serial); err != nil {
		return err
	}
	if _, err := h.output(ctx, actionTimeout, h.adb, "-s", serial, "emu", "kill"); err != nil {
		return fmt.Errorf("stop emulator %s: %w", serial, err)
	}
	return nil
}

func (h *Host) output(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("no binary for this operation on this host")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)

	cmd.Env = childenv.For(os.Environ(), nil)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), truncate(detail, 400))
	}
	return out, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// XcrunPath, ADBPath and EmulatorPath are the resolved toolchain binaries, ""
// when this host has none.
func (h *Host) XcrunPath() string    { return h.xcrun }
func (h *Host) ADBPath() string      { return h.adb }
func (h *Host) EmulatorPath() string { return h.emulator }
