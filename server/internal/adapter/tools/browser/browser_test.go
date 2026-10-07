package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// stubResolver answers from a table instead of from DNS, so the tests that
// matter here — a hostname that resolves to the metadata service, an integer
// that a permissive resolver turns into one — are deterministic and do not
// depend on the machine having a network at all.
type stubResolver map[string][]string

func (r stubResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := r[strings.ToLower(host)]
	if !ok {
		return nil, fmt.Errorf("stub resolver: no such host %q", host)
	}
	out := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return nil, fmt.Errorf("stub resolver: bad IP %q", ip)
		}
		out = append(out, net.IPAddr{IP: parsed})
	}
	return out, nil
}

// testPolicy is production's policy for this package (public internet plus
// loopback, see defaultPolicy) with DNS replaced.
func testPolicy(r urlguard.Resolver) urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	p.Resolver = r
	return p
}

// navigateWithPolicy runs browser_navigate against a session whose only
// difference from production is the resolver. CHROME_BIN points at nothing, so
// a URL that gets past the guard fails with chromeNotFoundMsg — which is how
// these tests tell "refused" from "allowed" without a browser.
func navigateWithPolicy(t *testing.T, r urlguard.Resolver, rawURL string) string {
	t.Helper()
	t.Setenv("CHROME_BIN", "/nonexistent/chromium-for-test")
	session := NewSession(WithURLPolicy(testPolicy(r)))
	defer session.Close()
	res := newNavigateTool(session).Execute(context.Background(), fmt.Sprintf(`{"url":%q}`, rawURL))
	if !res.IsError {
		t.Fatalf("navigate to %q unexpectedly succeeded: %s", rawURL, res.Content)
	}
	return res.Content
}

func TestDefinitions(t *testing.T) {
	session := NewSession()
	tools := NewExecutors(session)
	if len(tools) != 7 {
		t.Fatalf("expected 7 executors, got %d", len(tools))
	}

	tests := []struct {
		name     string
		required []string
	}{
		{name: "browser_navigate", required: []string{"url"}},
		{name: "browser_wait_for", required: []string{"selector"}},
		{name: "browser_click", required: []string{"selector"}},
		{name: "browser_fill", required: []string{"selector", "value"}},
		{name: "browser_read_dom", required: nil},
		{name: "browser_screenshot", required: nil},
		{name: "browser_set_viewport", required: nil},
	}

	// The description is filled by application/registry.Register from
	// catalog/system/tools/<name>.md, not Definition() itself — see board's
	// golden_tool_definitions_test.go (WP8a) for the same shift. Registered
	// once here so the per-tool loop below can look up the decorated version
	// alongside the raw one.
	reg := registry.New()
	for _, tool := range tools {
		reg.Register(tool)
	}
	decoratedByName := map[string]domain.ToolDefinition{}
	for _, d := range reg.Definitions() {
		decoratedByName[d.Function.Name] = d
	}

	byName := map[string]bool{}
	for i, tool := range tools {
		def := tool.Definition()
		if tool.Name() != def.Function.Name {
			t.Errorf("tool %d: Name() %q != definition name %q", i, tool.Name(), def.Function.Name)
		}
		if def.Type != "function" {
			t.Errorf("%s: definition type = %q, want function", tool.Name(), def.Type)
		}
		if decoratedByName[tool.Name()].Function.Description == "" {
			t.Errorf("%s: empty description", tool.Name())
		}
		raw, err := json.Marshal(def)
		if err != nil {
			t.Errorf("%s: marshal definition: %v", tool.Name(), err)
		}
		var roundTrip map[string]interface{}
		if err := json.Unmarshal(raw, &roundTrip); err != nil {
			t.Errorf("%s: definition is not valid JSON: %v", tool.Name(), err)
		}
		byName[tool.Name()] = true
	}

	for _, tc := range tests {
		if !byName[tc.name] {
			t.Errorf("missing tool %s", tc.name)
		}
	}

	for _, tool := range tools {
		for _, tc := range tests {
			if tc.name != tool.Name() {
				continue
			}
			params := tool.Definition().Function.Parameters
			required, _ := params["required"].([]string)
			if len(required) != len(tc.required) {
				t.Errorf("%s: required = %v, want %v", tc.name, required, tc.required)
				continue
			}
			for i, want := range tc.required {
				if required[i] != want {
					t.Errorf("%s: required[%d] = %q, want %q", tc.name, i, required[i], want)
				}
			}
		}
	}
}

func TestNewExecutorsNilSession(t *testing.T) {
	if got := NewExecutors(nil); got != nil {
		t.Fatalf("expected nil executors for nil session, got %d", len(got))
	}
}

func TestExecuteInvalidArguments(t *testing.T) {
	session := NewSession()
	for _, tool := range NewExecutors(session) {
		res := tool.Execute(context.Background(), "{not json")
		if !res.IsError {
			t.Errorf("%s: expected error for invalid JSON arguments", tool.Name())
		}
		if !strings.Contains(res.Content, "invalid arguments") {
			t.Errorf("%s: content = %q, want invalid arguments error", tool.Name(), res.Content)
		}
		if res.Name != tool.Name() {
			t.Errorf("%s: result name = %q", tool.Name(), res.Name)
		}
	}
}

func TestExecuteMissingRequiredArgs(t *testing.T) {
	session := NewSession()
	tests := []struct {
		tool string
		args string
		want string
	}{
		{tool: "browser_navigate", args: `{}`, want: "url is required"},
		{tool: "browser_wait_for", args: `{}`, want: "selector is required"},
		{tool: "browser_click", args: `{}`, want: "selector is required"},
		{tool: "browser_fill", args: `{"value":"x"}`, want: "selector is required"},
	}
	executors := NewExecutors(session)
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			for _, tool := range executors {
				if tool.Name() != tc.tool {
					continue
				}
				res := tool.Execute(context.Background(), tc.args)
				if !res.IsError {
					t.Fatalf("expected error for args %s", tc.args)
				}
				if !strings.Contains(res.Content, tc.want) {
					t.Fatalf("content = %q, want %q", res.Content, tc.want)
				}
				return
			}
			t.Fatalf("tool %s not registered", tc.tool)
		})
	}
}

// TestNavigateGuardRefusesInternalDestinations is F3: the pre-flight check used
// to be net.ParseIP plus an IsLinkLocal test, which a hostname walked straight
// past and which three spellings of 169.254.169.254 walked straight past. Every
// case here must be refused before chromium is even started.
func TestNavigateGuardRefusesInternalDestinations(t *testing.T) {
	// What an attacker's authoritative DNS would answer, and what a permissive
	// system resolver makes of the integer forms of 169.254.169.254 that
	// net.ParseIP rejects but chromium's URL parser accepts.
	resolver := stubResolver{
		"metadata.attacker.example": {"169.254.169.254"},
		"internal.attacker.example": {"10.0.0.5"},
		"mixed.attacker.example":    {"93.184.216.34", "10.0.0.5"},
		"2852039166":                {"169.254.169.254"},
		"0xa9fea9fe":                {"169.254.169.254"},
		"0251.0376.0251.0376":       {"169.254.169.254"},
	}

	tests := []struct {
		name string
		url  string
	}{
		{name: "metadata endpoint", url: "http://169.254.169.254/computeMetadata/v1/"},
		{name: "link local with port", url: "http://169.254.1.2:8080/"},
		{name: "ipv6 link local", url: "http://[fe80::1]/"},
		{name: "rfc1918 ten", url: "http://10.0.0.5/admin"},
		{name: "rfc1918 192.168", url: "http://192.168.1.1/"},
		{name: "cgnat", url: "http://100.64.0.1/"},
		{name: "ipv6 ula", url: "http://[fd00::1]/"},
		{name: "v4 mapped metadata", url: "http://[::ffff:169.254.169.254]/"},
		{name: "v4 compatible metadata", url: "http://[::169.254.169.254]/"},
		{name: "6to4 metadata", url: "http://[2002:a9fe:a9fe::]/"},
		{name: "nat64 metadata", url: "http://[64:ff9b::a9fe:a9fe]/"},

		// F3 point 1: a name, not a literal. The old check saw net.ParseIP
		// return nil and waved it through, and so did the post-redirect check,
		// because chromedp.Location reports the hostname it was given.
		{name: "hostname resolving to metadata", url: "http://metadata.attacker.example/computeMetadata/v1/"},
		{name: "hostname resolving to rfc1918", url: "http://internal.attacker.example/"},
		{name: "hostname with one bad answer among good ones", url: "http://mixed.attacker.example/"},

		// F3 point 2: the encodings chromium canonicalises to 169.254.169.254.
		{name: "decimal integer", url: "http://2852039166/"},
		{name: "hex integer", url: "http://0xA9FEA9FE/"},
		{name: "dotted octal", url: "http://0251.0376.0251.0376/"},

		{name: "ftp scheme", url: "ftp://example.com/"},
		{name: "file scheme", url: "file:///etc/passwd"},
		{name: "javascript scheme", url: "javascript:alert(1)"},
		{name: "no host", url: "http:///nowhere"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := navigateWithPolicy(t, resolver, tc.url)
			if got == chromeNotFoundMsg {
				t.Fatalf("%s reached the browser: the guard did not refuse it", tc.url)
			}
			if got != navFailedMsg {
				t.Fatalf("content = %q, want the flat %q", got, navFailedMsg)
			}
		})
	}
}

// A resolver that answers nothing must not turn into an accept: the encoded
// forms fail closed even when nothing resolves them.
func TestNavigateGuardRefusesUnresolvableEncodings(t *testing.T) {
	for _, raw := range []string{"http://2852039166/", "http://0xA9FEA9FE/", "http://0251.0376.0251.0376/"} {
		if got := navigateWithPolicy(t, stubResolver{}, raw); got != navFailedMsg {
			t.Fatalf("%s: content = %q, want %q", raw, got, navFailedMsg)
		}
	}
}

// TestNavigateAllowsWorkspaceLoopback is the constraint the fix had to respect.
// The QA agent boots the branch it is testing in its own workspace and drives
// that dev server with these tools (seeddata/prompts/qa-agent.md step 3), so
// http://localhost:8080/login has to keep working; see defaultPolicy for why
// loopback is the one exception and why it stops there.
func TestNavigateAllowsWorkspaceLoopback(t *testing.T) {
	resolver := stubResolver{
		"localhost":   {"127.0.0.1"},
		"example.com": {"93.184.216.34"},
	}
	for _, raw := range []string{
		"http://localhost:8080/login",
		"http://127.0.0.1:3000/",
		"http://[::1]:5173/",
		"https://example.com/",
	} {
		got := navigateWithPolicy(t, resolver, raw)
		if got != chromeNotFoundMsg {
			t.Fatalf("%s: content = %q, want it to pass the guard and fail on the missing browser", raw, got)
		}
	}
}

// The operator switch still closes loopback for installs that want it closed.
func TestNavigateLoopbackCanBeClosed(t *testing.T) {
	p := testPolicy(stubResolver{"localhost": {"127.0.0.1"}})
	p.AllowLoopback = false
	t.Setenv("CHROME_BIN", "/nonexistent/chromium-for-test")
	session := NewSession(WithURLPolicy(p))
	defer session.Close()
	res := newNavigateTool(session).Execute(context.Background(), `{"url":"http://localhost:8080/login"}`)
	if !res.IsError || res.Content != navFailedMsg {
		t.Fatalf("content = %q (error=%v), want %q", res.Content, res.IsError, navFailedMsg)
	}
}

func TestLoopbackAllowed(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{env: "", want: true},       // unset: the QA flow needs it
		{env: "true", want: true},   // operator agrees
		{env: "1", want: true},      //
		{env: "false", want: false}, // operator closes it
		{env: "0", want: false},     //
		{env: "maybe", want: true},  // a typo must not silently break QA
	}
	for _, tc := range tests {
		got := loopbackAllowed(func(string) string { return tc.env })
		if got != tc.want {
			t.Errorf("loopbackAllowed(%q) = %v, want %v", tc.env, got, tc.want)
		}
	}
}

// defaultPolicy is what production runs with; assert the shape directly rather
// than only through a tool.
func TestDefaultPolicy(t *testing.T) {
	t.Setenv(urlguard.AllowLoopbackEnv, "")
	p := defaultPolicy()
	if !p.AllowLoopback {
		t.Fatal("defaultPolicy must allow loopback: the QA agent tests the app it just booted")
	}
	if p.AllowPrivate {
		t.Fatal("defaultPolicy must not allow RFC1918: that is other devices on the operator's network and the database")
	}
	if _, err := p.Precheck("http://127.0.0.1:8080/login"); err != nil {
		t.Fatalf("loopback must pass: %v", err)
	}
	for _, raw := range []string{"http://169.254.169.254/", "http://10.0.0.5/", "http://100.64.0.1/", "ftp://example.com/"} {
		if _, err := p.Precheck(raw); err == nil {
			t.Fatalf("%s must be refused by the default policy", raw)
		} else if !errors.Is(err, urlguard.ErrBlocked) {
			t.Fatalf("%s: error %v does not match urlguard.ErrBlocked", raw, err)
		}
	}
}

// TestPageURLAllowed covers the choke point's classifier: what counts as a
// destination worth judging when it is read off the live tab.
func TestPageURLAllowed(t *testing.T) {
	p := testPolicy(stubResolver{
		"localhost":                 {"127.0.0.1"},
		"example.com":               {"93.184.216.34"},
		"metadata.attacker.example": {"169.254.169.254"},
	})
	tests := []struct {
		name    string
		url     string
		blocked bool
	}{
		{name: "empty", url: ""},
		{name: "about blank", url: "about:blank"},
		{name: "about srcdoc", url: "about:srcdoc"},
		{name: "chromium error page", url: "chrome-error://chromewebdata/"},
		{name: "data frame", url: "data:text/html,<p>x</p>"},
		{name: "blob frame", url: "blob:http://localhost:8080/2f1c-9a"},
		{name: "workspace app", url: "http://localhost:8080/login"},
		{name: "public site", url: "https://example.com/docs"},
		{name: "metadata literal", url: "http://169.254.169.254/computeMetadata/v1/", blocked: true},
		{name: "metadata by name", url: "http://metadata.attacker.example/", blocked: true},
		{name: "private", url: "http://10.0.0.5/", blocked: true},
		{name: "local file", url: "file:///etc/passwd", blocked: true},
		{name: "view-source of metadata", url: "view-source:http://169.254.169.254/", blocked: true},
		{name: "chrome internals", url: "chrome://net-internals/", blocked: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pageURLAllowed(context.Background(), p, tc.url)
			if tc.blocked && err == nil {
				t.Fatalf("%s must be refused", tc.url)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("%s must be allowed: %v", tc.url, err)
			}
		})
	}
}

// The model must not be able to tell a refused destination from a refused
// connection from a timeout: that difference is a port scanner it operates.
func TestErrorsDoNotDistinguishBlockedFromUnreachable(t *testing.T) {
	refused := errors.New(`page load error net::ERR_CONNECTION_REFUSED`)
	unreachable := errors.New(`page load error net::ERR_ADDRESS_UNREACHABLE`)
	unresolved := errors.New(`page load error net::ERR_NAME_NOT_RESOLVED`)

	for _, tc := range []struct{ name string }{{"browser_click"}, {"browser_read_dom"}, {"browser_screenshot"}} {
		want := runError(tc.name, "op", errBlockedPage).Content
		if want != pageUnavailableMsg {
			t.Fatalf("%s: blocked message = %q, want %q", tc.name, want, pageUnavailableMsg)
		}
		for _, err := range []error{refused, unreachable, unresolved} {
			if got := runError(tc.name, "op", err).Content; got != want {
				t.Fatalf("%s: %v produced %q, want the same %q", tc.name, err, got, want)
			}
		}
	}

	if got := navigateError(errBlockedPage).Content; got != navFailedMsg {
		t.Fatalf("navigate blocked message = %q, want %q", got, navFailedMsg)
	}
	for _, err := range []error{refused, unreachable, unresolved, context.DeadlineExceeded} {
		if got := navigateError(err).Content; got != navFailedMsg {
			t.Fatalf("navigate %v produced %q, want %q", err, got, navFailedMsg)
		}
	}

	// A page-level failure stays descriptive: the QA agent needs to know its
	// selector was not there, and that says nothing about what is listening.
	missing := errors.New(`could not find node with given id`)
	if got := runError("browser_click", `click "#submit"`, missing).Content; !strings.Contains(got, "#submit") {
		t.Fatalf("selector failures must stay useful, got %q", got)
	}
}

func TestChromeNotFound(t *testing.T) {
	// A set CHROME_BIN is authoritative — no PATH fallback — which makes this
	// deterministic on machines that do have a system chromium.
	t.Setenv("CHROME_BIN", "/nonexistent/chromium-for-test")
	session := NewSession(WithURLPolicy(testPolicy(stubResolver{"example.com": {"93.184.216.34"}})))
	defer session.Close()
	tool := newNavigateTool(session)
	res := tool.Execute(context.Background(), `{"url":"https://example.com"}`)
	if !res.IsError {
		t.Fatal("expected error when chromium is missing")
	}
	if res.Content != chromeNotFoundMsg {
		t.Fatalf("content = %q, want %q", res.Content, chromeNotFoundMsg)
	}
}

func TestCloseWithoutStart(t *testing.T) {
	session := NewSession()
	session.Close()
	session.Close()
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 100); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	long := strings.Repeat("a", 200)
	got := truncate(long, 100)
	if !strings.HasPrefix(got, strings.Repeat("a", 100)) {
		t.Fatal("truncate did not keep prefix")
	}
	if !strings.Contains(got, "[truncated at 100 bytes]") {
		t.Fatalf("truncate note missing: %q", got)
	}
}

func TestChromeBinWinsOverEveryInstalledBrowser(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "my-chromium")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHROME_BIN", bin)
	got, err := resolveExecPath()
	if err != nil || got != bin {
		t.Fatalf("resolveExecPath() = %q, %v; want %q", got, err, bin)
	}
}

func TestChromeCandidatesCoverEachOSStockInstall(t *testing.T) {
	unixNames := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable"}
	cases := []struct {
		name      string
		goos      string
		env       map[string]string
		wantPaths []string
		wantNames []string
	}{
		{
			name: "macOS app bundles",
			goos: "darwin",
			wantPaths: []string{
				"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				"/Applications/Chromium.app/Contents/MacOS/Chromium",
				"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			},
			wantNames: unixNames,
		},
		{
			name:      "Linux distro chromium, then PATH",
			goos:      "linux",
			wantPaths: []string{"/usr/bin/chromium"},
			wantNames: unixNames,
		},
		{
			name: "Windows machine-wide, per-user, then Edge",
			goos: "windows",
			env: map[string]string{
				"ProgramFiles":      `C:\Program Files`,
				"ProgramFiles(x86)": `C:\Program Files (x86)\`,
				"LOCALAPPDATA":      `C:\Users\dev\AppData\Local`,
			},
			wantPaths: []string{
				`C:\Program Files\Google\Chrome\Application\chrome.exe`,
				`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
				`C:\Users\dev\AppData\Local\Google\Chrome\Application\chrome.exe`,
				`C:\Users\dev\AppData\Local\Chromium\Application\chrome.exe`,
				`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
				`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			},
			wantNames: []string{"chrome", "msedge"},
		},
		{
			name: "Windows skips locations whose variable is unset",
			goos: "windows",
			env:  map[string]string{"ProgramFiles": `C:\Program Files`},
			wantPaths: []string{
				`C:\Program Files\Google\Chrome\Application\chrome.exe`,
				`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			},
			wantNames: []string{"chrome", "msedge"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths, names := chromeCandidates(tc.goos, func(key string) string { return tc.env[key] })
			if !reflect.DeepEqual(paths, tc.wantPaths) {
				t.Fatalf("paths = %q, want %q", paths, tc.wantPaths)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Fatalf("names = %q, want %q", names, tc.wantNames)
			}
		})
	}
}
