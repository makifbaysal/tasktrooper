// Package mobile drives a real device through an Appium server so QA /
// mobile-developer / PM agents can exercise the app the way a user would:
// launch it, tap, type, swipe, read the view hierarchy and take screenshots the
// model can actually see (via domain.ToolResult.Images).
//
// "A real device" is a physical Android phone, an iOS simulator or an Android
// emulator — see domain.DeviceKind*. The kind changes exactly one thing in this
// package, the capability set in capabilitiesFor; every tool, the lease, the
// pool and the parked-task path are identical for all three, and that identity
// is the feature. An agent cannot tell which it is driving, so the same QA
// grounding gates and the same role prompts apply unchanged.
//
// It is the twin of the browser package, with one structural difference that
// shapes everything here: the browser is a process this pod owns and can start
// as many of as it likes, whereas a device is a shared object — a phone every
// pod in the installation reaches, or a simulator with exactly one screen. So a
// session is not just a lazily started resource, it is a lease on one device:
//
//   - Appium itself is the lock. A second CreateSession against a device that
//     already has one fails, and that failure is what makes the lease mutual
//     across processes — a Go mutex only covers this one pod. Per UDID, not per
//     hub: one Appium with one adb server drives several phones at once.
//   - A lease nobody releases is a device nobody else can ever use, so an idle
//     session is closed on a timer as well as by mobile_release_device.
//   - Losing the race is not a tool error. It is domain.ResourceBlock, which
//     parks the task in the blocked column and lets the device sweeper resume
//     it when a phone frees up (see application/board/device_sweeper.go).
//
// Which phone a run gets is Pool's business (pool.go), not a tool argument:
// several sessions, one per registered device, and a run is handed a free one
// on its first call.
package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// notConfiguredMsg is operator-facing: the tools exist in every build, but the
// hub only exists where an operator has actually attached a device.
var notConfiguredMsg = prompt.MobileNotConfiguredText()

var errNotConfigured = errors.New(notConfiguredMsg)

// errDeviceBusy means another run holds the phone. It is deliberately distinct
// from every other failure: it is the one condition that must not surface as a
// tool error, because the correct response is to wait, not to retry or give up.
var errDeviceBusy = errors.New("mobile device is in use by another run")

var errHubUnreachable = errors.New("appium unreachable")

// hubUnavailableError is a hub this process starts on demand that would not
// start. Its own type because the tools word it from the catalog, with the
// reason as data.
type hubUnavailableError struct{ err error }

func (e *hubUnavailableError) Error() string { return "appium hub unavailable: " + e.err.Error() }
func (e *hubUnavailableError) Unwrap() error { return e.err }

// Hub keeps the Appium server running when this process starts it on demand
// (adapter/local/appiumhub). Without one, the hub is whoever's runs it and the
// calls go straight to the address.
type Hub interface {
	// Ensure returns once the hub at hubURL answers. Every call counts as use.
	Ensure(ctx context.Context, hubURL string) error
	// Manages reports whether the hub at hubURL is started on demand, so that
	// nothing answering there means nothing is leased on it.
	Manages(hubURL string) bool
}

const (
	// executeTimeout bounds one Appium command. Device commands are slower than
	// CDP round-trips — a tap waits for the UiAutomator2 server to settle — so
	// this is longer than the browser's.
	executeTimeout = 60 * time.Second

	// createTimeout bounds session creation, which installs/launches the app and
	// on a cold device starts the UiAutomator2 server from scratch.
	createTimeout = 180 * time.Second

	// deleteTimeout bounds the release. Short on purpose: a hung DELETE must not
	// keep this pod from admitting it no longer wants the phone.
	deleteTimeout = 20 * time.Second

	// idleRelease is how long the lease survives with no tool call. The device
	// is shared, and a run that wandered off — model gave up, pod died between
	// calls, agent forgot mobile_release_device — must not hold it forever.
	// Well above the gap between two tool calls in one run, well below the
	// sweeper's 10-minute recheck, so a stranded lease is always gone before
	// the next waiting task looks.
	idleRelease = 5 * time.Minute
)

// Config is what an operator sets to attach a device. An empty HubURL or UDID
// disables the tools entirely rather than half-registering them.
type Config struct {
	// HubURL is the Appium server base, e.g. http://appium.tasktrooper:4723.
	// It may equally be http://127.0.0.1:4723 — a hub on the operator's own
	// machine is the same hub as far as this client is concerned.
	HubURL string
	// Kind is domain.DeviceKind*: it selects the Appium driver and the shape of
	// the capability set. Empty means remote_adb, the physical Android phone
	// this package was written for, so a Config built by older code keeps
	// producing exactly the session it always did.
	Kind string
	// DeviceUDID pins the device. Appium would happily pick "whatever adb lists
	// first", which on a host with an emulator attached is a different device
	// than the one QA was told it is testing. What it holds depends on the
	// kind: the bridge's loopback address for a phone, the adb serial
	// (emulator-5554) for an emulator, the simctl UDID for a simulator.
	DeviceUDID string
	// PlatformVersion is optional and only used to make the capability set
	// explicit in logs and errors.
	PlatformVersion string
	// DevicePIN unlocks the lock screen. Empty means the device has no PIN.
	// Never logged, never returned in a tool result.
	DevicePIN string
	// AuthToken, when set, is sent as a bearer token — the tunnel in front of a
	// home-hosted Appium (Tailscale ACL, Cloudflare Access) is the real access
	// control, this is defence in depth for the hop inside it.
	AuthToken string
}

// Configured reports whether an operator actually attached a device.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.HubURL) != "" && strings.TrimSpace(c.DeviceUDID) != ""
}

// Session is the single shared device lease behind all mobile_* tools. One
// Appium session is reused across calls for the same reason the browser reuses
// one tab: launch then tap then screenshot must all see the same app state.
type Session struct {
	cfg    Config
	client *http.Client

	mu        sync.Mutex
	hub       Hub
	sessionID string
	// owner is the run holding this phone, uuid.Nil when it is free. A field
	// here rather than relying on Appium's own refusal, because that refusal is
	// cross-PROCESS: two runs inside this pod would otherwise both find a live
	// session and both drive the same phone, each undoing the other's taps.
	owner uuid.UUID
	// lastUsed drives the idle release. Read and written under mu only.
	lastUsed time.Time
	// appID is the package the current session launched, kept so a screenshot
	// can say what it is a screenshot of.
	appID string

	// stopIdle tears down the idle watcher when the session closes.
	stopIdle chan struct{}

	// now is time.Now in production; tests replace it to drive the idle timer
	// without sleeping.
	now func() time.Time
}

func NewSession(cfg Config) *Session {
	return &Session{
		cfg:    cfg,
		client: &http.Client{Timeout: createTimeout + 30*time.Second},
		now:    time.Now,
	}
}

func (s *Session) setHub(h Hub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hub = h
}

// Configured mirrors Config.Configured so callers holding only the session can
// ask. Nil-safe: a build with no device has a nil session everywhere.
func (s *Session) Configured() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Configured()
}

// udid names the phone this session drives — the local address the bridge
// mapped it onto, which is what adb and Appium know it by.
func (s *Session) udid() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.DeviceUDID
}

// held reports whether this pod has a lease on the phone right now.
func (s *Session) held() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID != ""
}

// take claims the phone for one run, or refuses with errDeviceBusy.
//
// Refusing is the whole point: the caller is walking a list of phones looking
// for a free one, and a take that quietly shared a live session would hand two
// runs the same device instead of sending the second one to the next phone.
func (s *Session) take(ctx context.Context, run uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.Configured() {
		return errNotConfigured
	}
	if s.sessionID != "" && s.owner != run {
		return errDeviceBusy
	}
	s.owner = run
	if err := s.ensureLocked(ctx, ""); err != nil {
		// Not held after all, so the claim goes back: keeping it would make the
		// next run walk past a phone that is merely unreachable as though
		// somebody were using it.
		s.owner = uuid.Nil
		return err
	}
	return nil
}

// Reconfigure points the session at a different device, or at none.
//
// The tools hold this session for the life of the process, so an operator
// attaching a phone in the settings UI has to be able to change what it drives
// without a restart. Any lease on the OLD device is released first: leaving it
// held would keep a phone the installation no longer claims away from whoever
// does claim it, and the far side would only free it after Appium's own
// timeout.
func (s *Session) Reconfigure(cfg Config) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == cfg {
		return
	}
	s.closeLocked(context.Background())
	s.cfg = cfg
}

// Probe reports whether the device is free right now, without taking it. The
// device sweeper calls this every 10 minutes to decide whether a parked task
// can run; it must not itself acquire the phone, or the sweep would take the
// lease and hand the resumed run a device that is already claimed.
//
// A hub that cannot be reached counts as busy: resuming a task onto a phone
// that is not there would only fail the run.
func (s *Session) Probe(ctx context.Context) bool {
	if !s.Configured() {
		return false
	}
	s.mu.Lock()
	held, cfg, hub := s.sessionID != "", s.cfg, s.hub
	s.mu.Unlock()
	if held {
		// This pod holds it. Nothing to resume onto.
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var out struct {
		Value []struct {
			ID           string                 `json:"id"`
			Capabilities map[string]interface{} `json:"capabilities"`
		} `json:"value"`
	}
	if err := s.do(ctx, cfg, http.MethodGet, "/sessions", nil, &out); err != nil {
		// A probe never starts the hub — it runs every sweep and every time the
		// settings page asks. A hub that starts on demand and is not running
		// holds no session, and the resumed run's first call starts it; read as
		// busy, the task it would resume stays parked until a hub nobody is
		// going to start comes back.
		if hub != nil && hub.Manages(cfg.HubURL) && notListening(err) {
			return true
		}
		log.Debug().Err(err).Msg("mobile: device probe failed, treating as busy")
		return false
	}
	// Only sessions on THIS phone count. The hub is shared — one Appium with one
	// adb server drives several UDIDs at once — so treating any live session as
	// "the device is busy" would have parked every task the moment one phone was
	// taken, which is exactly the bug this whole change exists to remove.
	//
	// A session that does not say which device it is on is still counted, and
	// that asymmetry is deliberate: an unidentifiable session might be this
	// phone's, and reporting a held device as free resumes a task straight into
	// a failed run, while the opposite costs one sweep interval.
	for _, sess := range out.Value {
		if udid := sessionUDID(sess.Capabilities); udid == "" || udid == cfg.DeviceUDID {
			return false
		}
	}
	return true
}

// sessionUDID digs the device out of a session's capabilities. Both spellings
// are checked because the prefix is a request-side convention: Appium echoes
// the capability back under whichever name the client sent it with.
func sessionUDID(caps map[string]interface{}) string {
	for _, key := range []string{"appium:udid", "udid", "deviceUDID"} {
		if v, ok := caps[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// run is the one function every tool goes through: it lazily takes the lease,
// refreshes the idle clock, and issues one Appium command against the live
// session. body may be nil for GET/DELETE; out may be nil to discard the value.
func (s *Session) run(ctx context.Context, method, path string, body, out interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(ctx, ""); err != nil {
		return err
	}
	err := s.callSessionLocked(ctx, method, path, body, out)
	if err != nil && isStaleSession(err) {
		// The phone rebooted, the UiAutomator2 server died, or an operator
		// killed the session from the hub. Drop the dead lease so the next tool
		// call starts a fresh one instead of every call failing until restart.
		log.Info().Msg("mobile: session went away, dropping the lease")
		s.closeLocked(context.WithoutCancel(ctx))
	}
	return err
}

// launch takes the lease with an explicit app, restarting an existing session
// when the requested package differs from the one currently under test.
func (s *Session) launch(ctx context.Context, appID, appURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID != "" && s.appID != appID {
		s.closeLocked(context.WithoutCancel(ctx))
	}
	if err := s.ensureLocked(ctx, appID); err != nil {
		return err
	}
	if appURL != "" {
		// Installing on every launch would reinstall the same build for every
		// tool call in a run; the session's app capability already put the
		// requested build on the device when the lease was taken.
		//
		// One endpoint for all three kinds, and that is not an oversight:
		// install_app is a driver-level command, so the .apk a UiAutomator2
		// session installs and the .app/.ipa an XCUITest session installs go
		// through the identical call. Which artifact arrives is the deploy
		// target's business (repository_deploy_targets.app_url), not this
		// layer's — the tool must not start guessing a file format from a
		// device kind and refusing a build a human recorded on purpose.
		if err := s.callSessionLocked(ctx, http.MethodPost, "/appium/device/install_app",
			map[string]interface{}{"appPath": appURL}, nil); err != nil {
			return fmt.Errorf("install app: %w", err)
		}
	}
	return s.callSessionLocked(ctx, http.MethodPost, "/appium/device/activate_app",
		map[string]interface{}{"appId": appID}, nil)
}

// ensureLocked takes the lease if this pod does not already hold it. Every
// tool path that talks to the hub comes through here, so it is also where an
// on-demand hub is started and told it is in use.
func (s *Session) ensureLocked(ctx context.Context, appID string) error {
	if !s.cfg.Configured() {
		return errNotConfigured
	}
	if s.hub != nil {
		if err := s.hub.Ensure(ctx, s.cfg.HubURL); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &hubUnavailableError{err: err}
		}
	}
	if s.sessionID != "" {
		s.lastUsed = s.now()
		return nil
	}

	caps := capabilitiesFor(s.cfg, appID)

	createCtx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	var created struct {
		Value struct {
			SessionID string `json:"sessionId"`
		} `json:"value"`
		SessionID string `json:"sessionId"`
	}
	err := s.do(createCtx, s.cfg, http.MethodPost, "/session", map[string]interface{}{
		"capabilities": map[string]interface{}{"alwaysMatch": caps},
	}, &created)
	if err != nil {
		return err
	}
	id := created.Value.SessionID
	if id == "" {
		id = created.SessionID
	}
	if id == "" {
		return errors.New("appium returned no session id")
	}
	s.sessionID = id
	s.appID = appID
	s.lastUsed = s.now()
	s.startIdleWatchLocked()
	log.Info().Str("udid", s.cfg.DeviceUDID).Msg("mobile: device lease taken")
	return nil
}

// capabilitiesFor builds the W3C capability set for one device kind.
//
// This is the ONLY place the three kinds differ inside the tool layer. Every
// mobile_* tool above it — tap, type, swipe, read_ui, screenshot — speaks plain
// Appium and is identical for all of them, which is what "parity" means here:
// a QA agent driving a simulator runs the same tools, hits the same grounding
// gates and reads the same role prompt as one driving a phone. If that stops
// being true, it stops here first.
//
// A free function rather than a method so the capability set can be asserted
// without a hub, a session or a lease.
func capabilitiesFor(cfg Config, appID string) map[string]interface{} {
	caps := map[string]interface{}{
		"appium:udid":    cfg.DeviceUDID,
		"appium:noReset": true,
		// Appium's own watchdog. If this pod dies mid-run without a DELETE, the
		// hub reaps the orphan and the device frees itself — belt to the idle
		// timer's braces, and the only one that survives a pod being killed.
		"appium:newCommandTimeout": int(idleRelease.Seconds()) + 60,
	}
	if cfg.PlatformVersion != "" {
		caps["appium:platformVersion"] = cfg.PlatformVersion
	}

	if cfg.Kind == domain.DeviceKindIOSSimulator {
		caps["platformName"] = "iOS"
		caps["appium:automationName"] = "XCUITest"
		if appID != "" {
			// bundleId, not appPackage: XCUITest names an app by its bundle
			// identifier, and appPackage is a UiAutomator2 capability it would
			// simply ignore — leaving a session that opens nothing and a run
			// that photographs the home screen.
			caps["appium:bundleId"] = appID
		}
		// Nothing here mirrors autoGrantPermissions or the unlock capabilities.
		// A simulator has no lock screen to unlock and no permission grants adb
		// could force; sending Android-only capabilities to XCUITest is at best
		// ignored and at worst a rejected session.
		return caps
	}

	// Both Android kinds, physical and emulated. From Appium's side an emulator
	// IS a phone — same driver, same capabilities, same everything — so there is
	// deliberately no branch between them here: a difference would be a way for
	// a bug to reproduce on one and not the other.
	caps["platformName"] = "Android"
	caps["appium:automationName"] = "UiAutomator2"
	caps["appium:autoGrantPermissions"] = true
	if appID != "" {
		caps["appium:appPackage"] = appID
	}
	// The PIN travels as a capability so Appium unlocks the phone as part of
	// taking the session: a run that has to remember to unlock first is a run
	// that photographs a lock screen and reports the app as broken.
	if cfg.DevicePIN != "" {
		caps["appium:unlockType"] = "pin"
		caps["appium:unlockKey"] = cfg.DevicePIN
		caps["appium:unlockStrategy"] = "uiautomator"
		caps["appium:skipUnlock"] = false
	}
	return caps
}

// startIdleWatchLocked releases a lease nobody is using. It polls rather than
// arming a timer per call because every tool call would otherwise have to reset
// one, and a missed reset is a device released mid-run.
func (s *Session) startIdleWatchLocked() {
	stop := make(chan struct{})
	s.stopIdle = stop
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.mu.Lock()
				if s.sessionID == "" || s.now().Sub(s.lastUsed) < idleRelease {
					s.mu.Unlock()
					continue
				}
				log.Info().Msg("mobile: releasing idle device lease")
				s.closeLocked(context.Background())
				s.mu.Unlock()
				return
			}
		}
	}()
}

// Release drops the lease so another run can take the phone. Safe to call when
// nothing is held.
func (s *Session) Release(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(ctx)
}

// Close is the shutdown hook. Same as Release; the session stays reusable.
func (s *Session) Close() { s.Release(context.Background()) }

func (s *Session) closeLocked(ctx context.Context) {
	if s.stopIdle != nil {
		close(s.stopIdle)
		s.stopIdle = nil
	}
	id := s.sessionID
	s.sessionID = ""
	s.appID = ""
	s.owner = uuid.Nil
	if id == "" {
		return
	}
	delCtx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()
	if err := s.do(delCtx, s.cfg, http.MethodDelete, "/session/"+id, nil, nil); err != nil {
		// The lease is dropped locally regardless: holding a session this pod
		// believes is dead only keeps the phone away from everyone. Appium's
		// newCommandTimeout reaps the far side.
		log.Warn().Err(err).Msg("mobile: releasing the device lease failed")
	}
}

// callSessionLocked issues a command scoped to the live session.
func (s *Session) callSessionLocked(ctx context.Context, method, path string, body, out interface{}) error {
	ctx, cancel := context.WithTimeout(ctx, executeTimeout)
	defer cancel()
	s.lastUsed = s.now()
	err := s.do(ctx, s.cfg, method, "/session/"+s.sessionID+path, body, out)
	s.lastUsed = s.now()
	return err
}

// It maps the two responses that mean something structural — the device is
// taken, the session is gone — onto sentinel errors and leaves the rest as the
// message Appium gave, which is usually the most useful thing the agent can
// read ("element not found", "app not installed").
// do is the raw Appium call. cfg is passed rather than read from the session
// because Reconfigure can swap it: every caller but Probe holds mu, and Probe
// deliberately does not want to hold it across a network round-trip.
func (s *Session) do(ctx context.Context, cfg Config, method, path string, body, out interface{}) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.HubURL, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errHubUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 24<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return classifyError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// classifyError turns an Appium error body into either a sentinel or a flat
// message. The busy detection is string-based because Appium reports a taken
// device as a generic session-creation failure whose only distinguishing mark
// is the driver's text.
func classifyError(status int, raw []byte) error {
	var body struct {
		Value struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		} `json:"value"`
	}
	_ = json.Unmarshal(raw, &body)
	msg := strings.TrimSpace(body.Value.Message)
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	lower := strings.ToLower(msg + " " + body.Value.Error)
	switch {
	case strings.Contains(lower, "already in use"),
		strings.Contains(lower, "session already exists"),
		strings.Contains(lower, "device is busy"),
		strings.Contains(lower, "is locked by another"),
		strings.Contains(lower, "no free devices"),
		status == http.StatusConflict:
		return errDeviceBusy
	case strings.Contains(lower, "invalid session id"),
		strings.Contains(lower, "terminated") && strings.Contains(lower, "session"),
		status == http.StatusNotFound && strings.Contains(lower, "session"):
		return fmt.Errorf("%w: %s", errStaleSession, truncate(msg, 300))
	}
	return fmt.Errorf("appium %d: %s", status, truncate(msg, 600))
}

var errStaleSession = errors.New("appium session is gone")

func isStaleSession(err error) bool { return errors.Is(err, errStaleSession) }

// notListening is a hub nothing answers at, as opposed to one that is there and
// slow: a timeout may be a hung hub still holding a session.
func notListening(err error) bool {
	if !errors.Is(err, errHubUnreachable) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return !errors.As(err, &netErr) || !netErr.Timeout()
}

// deviceBlock is the parked-task signal the tools hand back instead of an
// error when the phone is taken. Kept here so every tool words it identically —
// the text reaches the board as the task's blocked reason.
func deviceBlock(name string) domain.ToolResult {
	return domain.ToolResult{
		Name:    name,
		Content: prompt.MobileDeviceBusyContent(),
		ResourceBlock: &domain.ResourceBlock{
			Resource: domain.ResourceMobileDevice,
			Detail:   "waiting for a free mobile test device",
		},
	}
}

// NewSessionOn is NewSession for a hub this process cannot dial directly.
//
// A Mac's Appium listens on that Mac's loopback, behind a home router with no
// port forwarded. The only way in is the tunnel it dialled OUT on, reached by
// asking the control plane to carry the call — which means every request needs
// three identity headers this WebDriver client has no business knowing about.
//
// So the knowledge goes in a RoundTripper instead of into Config, for two
// reasons. Config is compared by == in Reconfigure, and a field holding a
// function or a map would either stop compiling or start panicking; and more
// importantly the client above this must stay an ORDINARY Appium client. The
// lease is the hub's own refusal of a second session against a busy device, and
// a client that spoke some framed protocol instead would have to re-derive that
// refusal from an envelope. Nothing here changes: same paths, same bodies, same
// classifyError reading the same W3C body.
func NewSessionOn(cfg Config, rt http.RoundTripper) *Session {
	s := NewSession(cfg)
	if rt != nil {
		s.client = &http.Client{Timeout: createTimeout + 30*time.Second, Transport: rt}
	}
	return s
}

// liveSessionUDIDs is which devices this hub currently has a session on.
//
// One call answers for every device on the Mac, which is why it exists beside
// Session.Probe rather than being expressed as N probes: a sweep asking "is any
// of this member's twelve simulators free" would otherwise be twelve round
// trips down a home tunnel every pass.
//
// A session that does not say which device it is on is reported under the empty
// string, and the caller treats that as occupying SOMETHING — the same
// asymmetry Probe applies, and for the same reason: reporting a held device as
// free resumes a task straight into a failed run, while the opposite costs one
// sweep interval.
func (s *Session) liveSessionUDIDs(ctx context.Context) (map[string]bool, error) {
	var out struct {
		Value []struct {
			ID           string                 `json:"id"`
			Capabilities map[string]interface{} `json:"capabilities"`
		} `json:"value"`
	}
	if err := s.do(ctx, s.cfg, http.MethodGet, "/sessions", nil, &out); err != nil {
		return nil, err
	}
	held := make(map[string]bool, len(out.Value))
	for _, sess := range out.Value {
		held[sessionUDID(sess.Capabilities)] = true
	}
	return held, nil
}

// idleAndFree reports that this session holds no lease AND is not in the middle
// of a call.
//
// Non-blocking on purpose. A session on the network holds mu for the whole
// round trip — a tap on a cold device is seconds — so a caller sweeping a map
// of sessions must not queue behind one. A lock it cannot take instantly is
// read as "alive", which is the safe answer for anything deciding whether to
// forget a session: forgetting one that is working would hand its run a second
// device mid-flow.
func (s *Session) idleAndFree() bool {
	if s == nil {
		return true
	}
	if !s.mu.TryLock() {
		return false
	}
	defer s.mu.Unlock()
	return s.sessionID == ""
}
