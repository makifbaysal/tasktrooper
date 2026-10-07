// Package simrun is the local toolchain half of "run this task on a
// simulator": a debug build for the simulator SDK, then install and launch.
package simrun

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/local/localdevice"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/proctree"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Android device ids carry what they name, because an AVD that is not
// running yet and a USB phone are both runnable and nothing in a bare name
// says which one it is.
const (
	avdPrefix    = "avd:"
	serialPrefix = "adb:"
)

type Host struct {
	devices *localdevice.Host
}

var _ port.SimulatorHost = (*Host)(nil)

func New(devices *localdevice.Host) *Host {
	return &Host{devices: devices}
}

func (h *Host) Devices(ctx context.Context) ([]domain.SimulatorDevice, error) {
	var out []domain.SimulatorDevice
	var errs []error
	if h.devices.SupportsIOSSimulators(ctx) {
		sims, err := h.devices.Simulators(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		for _, sim := range sims {
			out = append(out, domain.SimulatorDevice{
				ID: sim.UDID, Name: sim.Name, Platform: domain.MobileStorePlatformIOS, Runtime: sim.Runtime, State: strings.ToLower(sim.State),
			})
		}
	}
	if h.devices.SupportsAndroidEmulators(ctx) {
		avds, err := h.devices.AVDs(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		for _, avd := range avds {
			state := "stopped"
			if serial, err := h.devices.EmulatorSerial(ctx, avd); err == nil && serial != "" {
				state = "running"
			}
			out = append(out, domain.SimulatorDevice{ID: avdPrefix + avd, Name: avd, Platform: domain.MobileStorePlatformAndroid, Runtime: "emulator", State: state})
		}
		for _, serial := range h.physicalDevices(ctx) {
			out = append(out, domain.SimulatorDevice{ID: serialPrefix + serial, Name: serial, Platform: domain.MobileStorePlatformAndroid, Runtime: "device", State: "running"})
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// physicalDevices are adb devices that are not emulators: a phone on USB is a
// better place to try a build than any emulator.
func (h *Host) physicalDevices(ctx context.Context) []string {
	adb := h.devices.ADBPath()
	if adb == "" {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, adb, "devices")
	cmd.Env = childenv.For(os.Environ(), nil)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var serials []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != "device" || strings.HasPrefix(fields[0], "emulator-") {
			continue
		}
		serials = append(serials, fields[0])
	}
	return serials
}

func (h *Host) BuildAndLaunch(ctx context.Context, req port.SimulatorLaunch) error {
	status := req.Status
	if status == nil {
		status = func(string) {}
	}
	logf := req.Log
	if logf == nil {
		logf = func(string) {}
	}
	switch req.Platform {
	case domain.MobileStorePlatformIOS:
		return h.runIOS(ctx, req, status, logf)
	case domain.MobileStorePlatformAndroid:
		return h.runAndroid(ctx, req, status, logf)
	}
	return fmt.Errorf("unsupported platform %q", req.Platform)
}

func (h *Host) runIOS(ctx context.Context, req port.SimulatorLaunch, status func(string), logf func(string)) error {
	xcrun := h.devices.XcrunPath()
	if xcrun == "" {
		return errors.New("this machine has no Xcode command line tools (xcrun)")
	}
	if err := localdevice.ValidateSimulatorUDID(req.DeviceID); err != nil {
		return err
	}
	xcodebuild, err := exec.LookPath("xcodebuild")
	if err != nil {
		return errors.New("xcodebuild not found; install Xcode")
	}

	status(domain.SimulatorRunBuilding)
	projectFlag, project, err := findXcodeProject(req.ProjectDir)
	if err != nil {
		return err
	}
	if err := h.podInstall(ctx, filepath.Dir(project), logf); err != nil {
		return err
	}
	derived := filepath.Join(req.CacheDir, "DerivedData")
	if req.CacheDir == "" {
		derived = filepath.Join(os.TempDir(), "tasktrooper-simrun-derived")
	}
	if err := run(ctx, req.ProjectDir, nil, logf, xcodebuild,
		projectFlag, project, "-scheme", req.Scheme, "-configuration", "Debug",
		"-sdk", "iphonesimulator", "-destination", "id="+req.DeviceID,
		"-derivedDataPath", derived, "build", "CODE_SIGNING_ALLOWED=NO"); err != nil {
		return fmt.Errorf("the simulator build failed: %w", err)
	}
	app, err := newestMatch(filepath.Join(derived, "Build", "Products", "Debug-iphonesimulator"), ".app", req.Scheme+".app")
	if err != nil {
		return err
	}
	bundleID := plistValue(ctx, filepath.Join(app, "Info.plist"), "CFBundleIdentifier")
	if bundleID == "" {
		bundleID = req.Identifier
	}
	if bundleID == "" {
		return fmt.Errorf("could not read the bundle id of %s", filepath.Base(app))
	}

	status(domain.SimulatorRunInstalling)
	if err := h.devices.BootSimulator(ctx, req.DeviceID); err != nil {
		return fmt.Errorf("booting the simulator: %w", err)
	}
	// The simulator boots headless; Simulator.app is what puts it on screen.
	if open, err := exec.LookPath("open"); err == nil {
		_ = run(ctx, req.ProjectDir, nil, logf, open, "-a", "Simulator", "--args", "-CurrentDeviceUDID", req.DeviceID)
	}
	if err := run(ctx, req.ProjectDir, nil, logf, xcrun, "simctl", "install", req.DeviceID, app); err != nil {
		return fmt.Errorf("installing on the simulator: %w", err)
	}
	status(domain.SimulatorRunLaunching)
	_ = run(ctx, req.ProjectDir, nil, func(string) {}, xcrun, "simctl", "terminate", req.DeviceID, bundleID)
	if err := run(ctx, req.ProjectDir, nil, logf, xcrun, "simctl", "launch", req.DeviceID, bundleID); err != nil {
		return fmt.Errorf("launching %s: %w", bundleID, err)
	}
	return nil
}

// findXcodeProject prefers a workspace: with CocoaPods or a local package the
// .xcodeproj alone does not build.
func findXcodeProject(dir string) (flag, path string, err error) {
	var workspaces, projects []string
	walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		depth := len(strings.Split(rel, string(filepath.Separator)))
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".xcworkspace"):
			if !strings.HasSuffix(filepath.Dir(p), ".xcodeproj") {
				workspaces = append(workspaces, p)
			}
			return filepath.SkipDir
		case strings.HasSuffix(name, ".xcodeproj"):
			projects = append(projects, p)
			return filepath.SkipDir
		case name == "Pods" || name == "node_modules" || name == "build" || strings.HasPrefix(name, ".") && p != dir:
			return filepath.SkipDir
		case depth > 2:
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return "", "", walkErr
	}
	sort.Strings(workspaces)
	sort.Strings(projects)
	if len(workspaces) > 0 {
		return "-workspace", workspaces[0], nil
	}
	if len(projects) > 0 {
		return "-project", projects[0], nil
	}
	return "", "", fmt.Errorf("no .xcworkspace or .xcodeproj under %s", dir)
}

func (h *Host) podInstall(ctx context.Context, dir string, logf func(string)) error {
	if _, err := os.Stat(filepath.Join(dir, "Podfile")); err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "Pods")); err == nil {
		return nil
	}
	pod, err := exec.LookPath("pod")
	if err != nil {
		return errors.New("the project uses CocoaPods and `pod` is not installed (gem install cocoapods)")
	}
	if err := run(ctx, dir, nil, logf, pod, "install"); err != nil {
		return fmt.Errorf("pod install failed: %w", err)
	}
	return nil
}

func plistValue(ctx context.Context, plist, key string) string {
	cmd := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (h *Host) runAndroid(ctx context.Context, req port.SimulatorLaunch, status func(string), logf func(string)) error {
	adb := h.devices.ADBPath()
	if adb == "" {
		return errors.New("this machine has no Android SDK platform-tools (adb)")
	}
	gradlew := filepath.Join(req.ProjectDir, "gradlew")
	if info, err := os.Stat(gradlew); err != nil || info.Mode()&0o111 == 0 {
		return fmt.Errorf("no executable ./gradlew in %s", req.ProjectDir)
	}
	sdk := filepath.Dir(filepath.Dir(adb))
	env := []string{"ANDROID_HOME=" + sdk, "ANDROID_SDK_ROOT=" + sdk}

	status(domain.SimulatorRunBuilding)
	if err := run(ctx, req.ProjectDir, env, logf, gradlew, ":"+req.Module+":assembleDebug"); err != nil {
		return fmt.Errorf("the debug build failed: %w", err)
	}
	moduleDir := filepath.Join(req.ProjectDir, filepath.FromSlash(strings.ReplaceAll(req.Module, ":", "/")))
	apk, err := newestMatch(filepath.Join(moduleDir, "build", "outputs", "apk", "debug"), ".apk", "")
	if err != nil {
		return err
	}

	status(domain.SimulatorRunInstalling)
	serial, err := h.androidSerial(ctx, req.DeviceID)
	if err != nil {
		return err
	}
	if err := run(ctx, req.ProjectDir, nil, logf, adb, "-s", serial, "install", "-r", apk); err != nil {
		return fmt.Errorf("installing on %s: %w", serial, err)
	}
	pkg := apkPackage(ctx, sdk, apk)
	if pkg == "" {
		pkg = req.Identifier
	}
	if pkg == "" {
		return fmt.Errorf("could not read the package name of %s", filepath.Base(apk))
	}

	status(domain.SimulatorRunLaunching)
	if err := run(ctx, req.ProjectDir, nil, logf, adb, "-s", serial, "shell", "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		return fmt.Errorf("launching %s: %w", pkg, err)
	}
	return nil
}

func (h *Host) androidSerial(ctx context.Context, deviceID string) (string, error) {
	switch {
	case strings.HasPrefix(deviceID, serialPrefix):
		serial := strings.TrimPrefix(deviceID, serialPrefix)
		if err := localdevice.ValidateEmulatorSerial(serial); err != nil && !validDeviceSerial(serial) {
			return "", fmt.Errorf("%q is not an adb serial", serial)
		}
		return serial, nil
	case strings.HasPrefix(deviceID, avdPrefix):
		serial, err := h.devices.LaunchEmulator(ctx, strings.TrimPrefix(deviceID, avdPrefix), true)
		if err != nil {
			return "", fmt.Errorf("starting the emulator: %w", err)
		}
		return serial, nil
	}
	return "", fmt.Errorf("unknown Android device %q", deviceID)
}

func validDeviceSerial(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}

// apkPackage reads the package out of the APK itself: a debug build commonly
// carries an applicationIdSuffix, so the release identifier is not what got
// installed.
func apkPackage(ctx context.Context, sdk, apk string) string {
	tools, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*", "aapt2"))
	if len(tools) == 0 {
		return ""
	}
	sort.Strings(tools)
	cmd := exec.CommandContext(ctx, tools[len(tools)-1], "dump", "packagename", apk)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newestMatch(dir, ext, prefer string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("the build left nothing in %s: %w", dir, err)
	}
	best, bestAt := "", time.Time{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if prefer != "" && e.Name() == prefer {
			return filepath.Join(dir, e.Name()), nil
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestAt) {
			best, bestAt = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("the build produced no %s in %s", ext, dir)
	}
	return best, nil
}

// run starts name as a killable tree (xcodebuild and gradle both leave
// daemons and workers behind) and streams its output to logf.
func run(ctx context.Context, dir string, env []string, logf func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = childenv.For(os.Environ(), env)
	cmd.Stdin = nil
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw

	tree, err := proctree.Start(cmd)
	if err != nil {
		_ = pw.Close()
		return fmt.Errorf("starting %s: %w", filepath.Base(name), err)
	}
	defer tree.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			logf(scanner.Text())
		}
		_, _ = io.Copy(io.Discard, pr)
	}()
	waitErr := cmd.Wait()
	_ = pw.Close()
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("%s exited: %w", filepath.Base(name), waitErr)
	}
	return nil
}
