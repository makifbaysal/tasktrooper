// Package browser drives a headless Chromium so QA/PM agents can exercise the
// live product the way a user would: navigate, wait for elements, click, fill
// forms, read the DOM and take screenshots the model can actually see (via
// domain.ToolResult.Images).
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// chromeNotFoundMsg is deliberately operator-facing: this process ships
// without a bundled browser, so only a host with Chromium already installed
// (or CHROME_BIN pointed at one) can use these tools.
var chromeNotFoundMsg = prompt.BrowserChromeNotFoundText()

// errChromeNotFound lets tools surface chromeNotFoundMsg verbatim instead of
// wrapped in an action prefix like "navigate: ...".
var errChromeNotFound = errors.New(chromeNotFoundMsg)

const (
	executeTimeout        = 30 * time.Second
	defaultViewportWidth  = 1440
	defaultViewportHeight = 900

	// guardTimeout bounds one destination check, and scrubTimeout bounds the
	// about:blank that follows a failed one. Both are short: they are two CDP
	// round-trips to a browser that is already running, and a browser that
	// cannot answer them in this long is a browser that gets torn down.
	guardTimeout = 5 * time.Second
	scrubTimeout = 5 * time.Second

	// browserStartTimeout bounds chromium printing its DevTools address.
	// chromedp's default is 20s, which a cold first launch — a fresh CI
	// runner, a first Chrome start on Windows with Defender scanning it —
	// overruns, and every browser_* call then fails as "could not open that
	// URL" though nothing is wrong with the page.
	browserStartTimeout = 60 * time.Second
)

// Session is the single shared browser behind all browser_* tools. One tab is
// reused across calls on purpose — navigate then click then screenshot must
// all see the same page — so run serializes callers under the mutex.
//
// That shared tab is also why the destination guard lives here rather than in
// the tools. Each tool used to guard its own arguments, which covered exactly
// one of them: browser_navigate vetted its URL, and browser_click then moved
// the very same tab anywhere it liked by clicking a link, after which
// browser_read_dom and browser_screenshot handed the result to the model.
// run is the one function every tool goes through, so the check goes there and
// covers click, fill, wait_for, read_dom, screenshot and anything added later
// without those tools having to remember it.
type Session struct {
	mu          sync.Mutex
	tabCtx      context.Context
	tabCancel   context.CancelFunc
	allocCancel context.CancelFunc

	// policy is fixed at construction and only read afterwards, so run can use
	// it without holding mu.
	policy urlguard.Policy

	// extraFlags are additional chromium command-line flags. Tests only.
	extraFlags []chromedp.ExecAllocatorOption
}

// Option customises a Session at construction.
type Option func(*Session)

// WithURLPolicy replaces the destination policy. Production takes defaultPolicy
// (public internet plus loopback, see guard.go); tests use this to drive the
// guard with a stub resolver.
func WithURLPolicy(p urlguard.Policy) Option {
	return func(s *Session) { s.policy = p }
}

// withChromeFlag adds a chromium command-line flag. Unexported because its only
// caller is the integration test, which uses --host-resolver-rules to point a
// hostname at a local server instead of depending on real DNS; it must not
// become production wiring.
func withChromeFlag(name string, value interface{}) Option {
	return func(s *Session) { s.extraFlags = append(s.extraFlags, chromedp.Flag(name, value)) }
}

func NewSession(opts ...Option) *Session {
	s := &Session{policy: defaultPolicy()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// resolveExecPath finds the chromium binary. A set CHROME_BIN is authoritative:
// if the operator pointed at a path that does not exist, falling back to a
// PATH lookup would mask the misconfiguration.
func resolveExecPath() (string, error) {
	if p := os.Getenv("CHROME_BIN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", errChromeNotFound
	}
	paths, names := chromeCandidates(runtime.GOOS, os.Getenv)
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errChromeNotFound
}

// chromeCandidates lists where each OS's Chrome, Chromium and Edge installers
// put the browser, then the names to look up on PATH. The macOS and Windows
// installers never touch PATH, so without the fixed locations a stock install
// is invisible. Edge is last: it is Chromium underneath and ships with every
// Windows, so it is what a Windows host without Chrome still has.
func chromeCandidates(goos string, getenv func(string) string) (paths, names []string) {
	switch goos {
	case "darwin":
		paths = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		for _, c := range []struct{ env, rel string }{
			{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
			{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
			{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
			{"LOCALAPPDATA", `Chromium\Application\chrome.exe`},
			{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
			{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
		} {
			if root := strings.TrimRight(strings.TrimSpace(getenv(c.env)), `\`); root != "" {
				paths = append(paths, root+`\`+c.rel)
			}
		}
		return paths, []string{"chrome", "msedge"}
	default:
		paths = []string{"/usr/bin/chromium"}
	}
	return paths, []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable"}
}

// run executes actions on the shared tab with a per-call timeout, with the
// destination guard wrapped around them. The tab context outlives the request
// context by design (the browser must survive between tool calls), so the
// request context is only wired up for cancellation.
//
// Every browser_* tool funnels through here, which is the point: the guard runs
// for all of them, in one place, whether or not the tool knows a URL exists.
func (s *Session) run(ctx context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	// Before: a page moves itself after the previous call returned — a meta
	// refresh, a setTimeout, a deferred form submit — so "the last call approved
	// this tab" is not a statement about where it is now. Checking first is also
	// what stops browser_fill from typing a password into an internal page.
	if err := s.guardLocked(); err != nil {
		return err
	}
	runErr := s.runLocked(ctx, timeout, actions...)
	// After: this is the one that kills the click chain. browser_click on a link
	// to http://169.254.169.254/ succeeds as a click; it is the landing page that
	// is the attack, and the tool that would have read it is the next one. The
	// guard error wins over runErr because the tab state is the more serious
	// finding, and because whatever the actions collected into their output
	// variables came off a page that is not allowed to reach the model — the
	// caller must discard it, which returning an error is what makes it do.
	if err := s.guardLocked(); err != nil {
		return err
	}
	return runErr
}

// runLocked is the unguarded run. It must only be called with mu held and only
// from run or the guard itself: it is what the guard is built out of, so a tool
// reaching for it directly would be a tool opting out of the guard.
func (s *Session) runLocked(ctx context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	if s.tabCtx == nil {
		return errChromeNotFound
	}
	runCtx, cancel := context.WithTimeout(s.tabCtx, timeout)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(runCtx, actions...)
}

// guardLocked is the choke point: it asks the browser where the tab actually is
// and refuses to let any tool continue when the answer is a destination the
// policy does not permit. On a refusal it blanks the tab first, so the page
// cannot simply be read by the next call.
func (s *Session) guardLocked() error {
	// Deliberately not the caller's context. This is a safety check, not the
	// caller's work: a cancelled or expired request must not be able to skip it,
	// and must not stop the scrub that follows a refusal. It gets its own budget
	// instead, because the checks resolve hostnames and a resolver that never
	// answers would otherwise hold the session mutex for as long as it liked.
	checkCtx, cancel := context.WithTimeout(context.Background(), guardTimeout)
	defer cancel()

	urls, err := s.tabURLsLocked()
	if err != nil {
		// Not knowing where the tab is, is not a reason to let a tool read it.
		log.Warn().Err(err).Msg("browser guard: could not read the tab location; discarding the page")
		s.scrubLocked()
		return errBlockedPage
	}

	seen := make(map[string]struct{}, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, dup := seen[raw]; dup {
			continue
		}
		seen[raw] = struct{}{}
		if len(seen) > maxCheckedFrameURLs {
			log.Warn().Int("frame_urls", len(urls)).Msg("browser guard: too many distinct frame URLs to vet; discarding the page")
			s.scrubLocked()
			return errBlockedPage
		}
		if err := pageURLAllowed(checkCtx, s.policy, raw); err != nil {
			log.Warn().Err(err).Str("url", urlguard.LogRaw(raw)).Msg("browser guard: page is on a destination that is not permitted")
			s.scrubLocked()
			return errBlockedPage
		}
	}
	return nil
}

// tabURLsLocked asks chromium what the tab is currently showing: the main frame
// and every subframe of it.
//
// It goes to the protocol rather than to chromedp.Location, which evaluates
// document.location.toString() inside the page, for two reasons. The answer
// comes from the browser instead of from script running in a document the
// attacker wrote; and it covers subframes, which document location does not.
// That second one is a real hole and not a nicety: an attacker page that embeds
// <iframe src="http://169.254.169.254/..."> keeps the main frame on its own
// harmless URL, and browser_screenshot then hands the model a picture of the
// rendered internal page.
//
// It takes two calls because chromium splits the answer in two. Page.getFrameTree
// reports the frames that share the page's renderer process — which, under site
// isolation, is exactly the ones NOT worth worrying about, since a cross-origin
// iframe is moved into its own process and disappears from that tree entirely
// (verified: a cross-origin iframe leaves the tree with zero children). Those
// out-of-process frames show up in Target.getTargets as targets of type
// "iframe", so both are read and judged together.
//
// UnreachableURL is collected alongside URL because a frame that failed to load
// reports chrome-error://chromewebdata/ as its URL and keeps the destination it
// tried in UnreachableURL — the interesting half, when the destination is
// internal.
func (s *Session) tabURLsLocked() ([]string, error) {
	var urls []string
	err := s.runLocked(context.Background(), guardTimeout, chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		urls = appendFrameURLs(urls, tree, 0)

		infos, err := target.GetTargets().Do(ctx)
		if err != nil {
			return err
		}
		for _, info := range infos {
			// Only iframes. Other tabs and the browser's own background pages
			// are not what these tools read: every browser_* action runs against
			// this one tab, and a popup the page opened is not reachable from it.
			if info != nil && info.Type == "iframe" {
				urls = append(urls, info.URL)
			}
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	return urls, nil
}

// maxFrameDepth bounds the walk; nesting past this is not a page anyone tests.
const maxFrameDepth = 16

func appendFrameURLs(out []string, tree *page.FrameTree, depth int) []string {
	if tree == nil || depth > maxFrameDepth {
		return out
	}
	if f := tree.Frame; f != nil {
		out = append(out, frameURL(f), f.UnreachableURL)
	}
	for _, child := range tree.ChildFrames {
		out = appendFrameURLs(out, child, depth+1)
	}
	return out
}

func frameURL(f *cdp.Frame) string {
	if f.URLFragment != "" {
		return f.URL + f.URLFragment
	}
	return f.URL
}

// scrubLocked makes sure a page the guard just refused cannot be read by the
// next call. Blanking is the cheap way; if the tab will not blank — a wedged
// renderer, a beforeunload that will not let go — the whole browser goes, which
// is safe because the next run starts a fresh one lazily.
func (s *Session) scrubLocked() {
	if s.tabCtx == nil {
		return
	}
	if err := s.runLocked(context.Background(), scrubTimeout, chromedp.Navigate("about:blank")); err != nil {
		log.Warn().Err(err).Msg("browser guard: could not blank the tab; tearing the browser down")
		s.closeLocked()
	}
}

func (s *Session) ensureLocked() error {
	if s.tabCtx != nil {
		return nil
	}
	execPath, err := resolveExecPath()
	if err != nil {
		return err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", "new"),
		chromedp.NoSandbox,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.DisableGPU,
		chromedp.WindowSize(defaultViewportWidth, defaultViewportHeight),
		chromedp.WSURLReadTimeout(browserStartTimeout),
	)
	opts = append(opts, s.extraFlags...)
	// Background, not the request context: the browser must outlive the call
	// that happened to start it.
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	tabCtx, tabCancel := chromedp.NewContext(allocCtx)
	// An empty Run spawns the browser process eagerly so a broken binary
	// surfaces here as one clear error instead of inside every later action.
	// The context of this first Run becomes the chromium process's lifetime
	// (exec.CommandContext inside Allocate), so it must be tabCtx itself — a
	// derived timeout context would kill the browser the moment it was
	// cancelled. Startup is bounded with a timer on tabCancel instead.
	timer := time.AfterFunc(executeTimeout, tabCancel)
	err = chromedp.Run(tabCtx)
	timer.Stop()
	if err != nil {
		tabCancel()
		allocCancel()
		return fmt.Errorf("start chromium: %w", err)
	}
	s.tabCtx = tabCtx
	s.tabCancel = tabCancel
	s.allocCancel = allocCancel
	return nil
}

// Close tears the browser down. The session stays usable: the next Execute
// lazily starts a fresh browser, which is what the runtime reload path relies on.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

func (s *Session) closeLocked() {
	if s.tabCancel != nil {
		s.tabCancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
	s.tabCtx = nil
	s.tabCancel = nil
	s.allocCancel = nil
}
