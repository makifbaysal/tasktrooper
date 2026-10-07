package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func testChromePath(t *testing.T) string {
	t.Helper()
	if p, err := resolveExecPath(); err == nil {
		return p
	}
	t.Skip("no chromium/chrome binary available; skipping browser integration test")
	return ""
}

const integrationPage = `<!DOCTYPE html>
<html>
<head><title>Browser Tool Test</title></head>
<body>
  <h1 id="headline">Hello QA</h1>
  <button id="reveal" onclick="document.getElementById('secret').style.display='block'">Reveal</button>
  <div id="secret" style="display:none">the secret is visible</div>
  <form action="/submitted" method="get">
    <input type="text" id="q" name="q" />
  </form>
</body>
</html>`

func TestBrowserToolsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, integrationPage)
	})
	mux.HandleFunc("/submitted", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Submitted</title></head><body><p id="echo">query=%s</p></body></html>`, r.URL.Query().Get("q"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	executors := NewExecutors(session)
	execute := func(name, args string) (content string, isError bool, images int) {
		t.Helper()
		for _, tool := range executors {
			if tool.Name() != name {
				continue
			}
			res := tool.Execute(context.Background(), args)
			return res.Content, res.IsError, len(res.Images)
		}
		t.Fatalf("tool %s not found", name)
		return "", false, 0
	}

	content, isErr, _ := execute("browser_navigate", fmt.Sprintf(`{"url":%q}`, srv.URL))
	if isErr {
		t.Fatalf("navigate failed: %s", content)
	}
	if !strings.Contains(content, "Browser Tool Test") || !strings.Contains(content, "Hello QA") {
		t.Fatalf("navigate content missing title/body text: %s", content)
	}

	content, isErr, _ = execute("browser_wait_for", `{"selector":"#headline","timeout_seconds":5}`)
	if isErr {
		t.Fatalf("wait_for failed: %s", content)
	}

	content, isErr, _ = execute("browser_click", `{"selector":"#reveal"}`)
	if isErr {
		t.Fatalf("click failed: %s", content)
	}
	content, isErr, _ = execute("browser_wait_for", `{"selector":"#secret","timeout_seconds":5}`)
	if isErr {
		t.Fatalf("wait_for after click failed: %s", content)
	}

	content, isErr, _ = execute("browser_read_dom", `{"selector":"#secret"}`)
	if isErr || !strings.Contains(content, "the secret is visible") {
		t.Fatalf("read_dom text = %q, err=%v", content, isErr)
	}
	content, isErr, _ = execute("browser_read_dom", `{"selector":"#secret","as_text":false}`)
	if isErr || !strings.Contains(content, "<div id=\"secret\"") {
		t.Fatalf("read_dom html = %q, err=%v", content, isErr)
	}

	content, isErr, _ = execute("browser_fill", `{"selector":"#q","value":"tasktrooper","submit":true}`)
	if isErr {
		t.Fatalf("fill failed: %s", content)
	}
	content, isErr, _ = execute("browser_wait_for", `{"selector":"#echo","timeout_seconds":5}`)
	if isErr {
		t.Fatalf("wait_for submitted page failed: %s", content)
	}
	content, isErr, _ = execute("browser_read_dom", `{"selector":"#echo"}`)
	if isErr || !strings.Contains(content, "query=tasktrooper") {
		t.Fatalf("submit did not land: %q err=%v", content, isErr)
	}

	sc, isErr, images := executeScreenshot(t, execute, `{"width":1440}`)
	if isErr {
		t.Fatalf("screenshot failed: %s", sc)
	}
	if images != 1 {
		t.Fatalf("screenshot attached %d images, want 1", images)
	}
	sc, isErr, images = executeScreenshot(t, execute, `{"full_page":true,"width":390}`)
	if isErr {
		t.Fatalf("mobile full-page screenshot failed: %s", sc)
	}
	if images != 1 {
		t.Fatalf("mobile screenshot attached %d images, want 1", images)
	}
	if !strings.Contains(sc, "390x844") {
		t.Fatalf("mobile screenshot content missing viewport info: %s", sc)
	}
}

func executeScreenshot(t *testing.T, execute func(string, string) (string, bool, int), args string) (string, bool, int) {
	t.Helper()
	content, isErr, images := execute("browser_screenshot", args)
	return content, isErr, images
}

// TestScreenshotImagePayload verifies the Images field carries decodable
// base64 of the declared media type.
func TestScreenshotImagePayload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Shot</title></head><body><p>pixel check</p></body></html>`)
	}))
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	nav := newNavigateTool(session)
	if res := nav.Execute(context.Background(), fmt.Sprintf(`{"url":%q}`, srv.URL)); res.IsError {
		t.Fatalf("navigate: %s", res.Content)
	}
	shot := newScreenshotTool(session)
	res := shot.Execute(context.Background(), `{}`)
	if res.IsError {
		t.Fatalf("screenshot: %s", res.Content)
	}
	if len(res.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(res.Images))
	}
	img := res.Images[0]
	if img.MediaType != "image/png" && img.MediaType != "image/jpeg" {
		t.Fatalf("media type = %q", img.MediaType)
	}
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		t.Fatalf("image data is not valid base64: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("image data is empty")
	}
	pngMagic := []byte{0x89, 'P', 'N', 'G'}
	jpegMagic := []byte{0xFF, 0xD8}
	switch img.MediaType {
	case "image/png":
		if !strings.HasPrefix(string(raw), string(pngMagic)) {
			t.Fatal("declared png but magic bytes differ")
		}
	case "image/jpeg":
		if !strings.HasPrefix(string(raw), string(jpegMagic)) {
			t.Fatal("declared jpeg but magic bytes differ")
		}
	}
}

// The exploit chain from the audit, run against a real browser.
//
// baitHost stands in for 169.254.169.254: chromium is told (via
// --host-resolver-rules) to resolve it to a local server that really serves a
// page, while the guard's resolver answers 169.254.169.254 for it. So the
// internal page genuinely loads and renders in the shared tab — the tests then
// assert that no tool ever hands it to the model.
const (
	baitHost     = "metadata.attacker.example"
	secretMarker = "INTERNAL-SERVICE-ACCOUNT-TOKEN"
)

func startInternalServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>metadata</title></head><body><pre id="token">%s</pre></body></html>`, secretMarker)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func startAttackerServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Attacker</title></head><body>%s</body></html>`, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// guardedSession is production's wiring with two test-only substitutions: the
// guard resolves baitHost to the metadata address, and chromium resolves it to
// the local stand-in server.
func guardedSession(t *testing.T, internal *httptest.Server) *Session {
	t.Helper()
	session := NewSession(
		WithURLPolicy(testPolicy(stubResolver{baitHost: {"169.254.169.254"}})),
		withChromeFlag("host-resolver-rules", "MAP "+baitHost+" "+strings.TrimPrefix(internal.URL, "http://")),
	)
	t.Cleanup(session.Close)
	return session
}

// TestClickToInternalDestinationIsRefused is F2: browser_navigate passes the
// guard on an attacker page, the page contains a link to an internal address,
// and browser_click moves the shared tab there with no check of its own. Before
// the fix, browser_read_dom then returned the internal body to the model.
func TestClickToInternalDestinationIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	internal := startInternalServer(t)
	attacker := startAttackerServer(t, fmt.Sprintf(
		`<h1 id="bait">nothing to see</h1><a id="go" href="http://%s/computeMetadata/v1/">go</a>`, baitHost))
	session := guardedSession(t, internal)

	ctx := context.Background()
	if res := newNavigateTool(session).Execute(ctx, fmt.Sprintf(`{"url":%q}`, attacker.URL)); res.IsError {
		t.Fatalf("navigate to the attacker page failed: %s", res.Content)
	}

	read := newReadDOMTool(session)
	refused := false
	check := func(res domain.ToolResult) {
		t.Helper()
		if strings.Contains(res.Content, secretMarker) {
			t.Fatalf("%s handed the internal page to the model: %s", res.Name, res.Content)
		}
		if res.IsError && res.Content == pageUnavailableMsg {
			refused = true
		}
	}

	check(newClickTool(session).Execute(ctx, `{"selector":"#go"}`))

	// The click dispatches; the navigation commits a moment later. Whichever
	// call is the first to see the tab on the internal page must be the one that
	// refuses, and none of them may return its content.
	for deadline := time.Now().Add(15 * time.Second); !refused && time.Now().Before(deadline); {
		check(read.Execute(ctx, `{"selector":"body"}`))
		if refused {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !refused {
		t.Fatal("the click chain was never refused: the tab reached an internal destination and the tools kept working")
	}

	// And the tab must not be parked on that page waiting for the next tool.
	after := read.Execute(ctx, `{"selector":"body"}`)
	if after.IsError {
		t.Fatalf("the tab was not left usable after the refusal: %s", after.Content)
	}
	if strings.Contains(after.Content, secretMarker) {
		t.Fatalf("the blocked page survived the refusal: %s", after.Content)
	}
}

// TestIframeToInternalDestinationIsRefused is the same finding through the
// screenshot half of it: the main frame stays on the attacker's own URL, so a
// check that only reads document.location sees nothing wrong, while the
// rendered internal page goes to the model as an image.
func TestIframeToInternalDestinationIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	internal := startInternalServer(t)
	attacker := startAttackerServer(t, fmt.Sprintf(
		`<h1 id="bait">nothing to see</h1><iframe id="f" width="800" height="600" src="http://%s/computeMetadata/v1/"></iframe>`, baitHost))
	session := guardedSession(t, internal)

	ctx := context.Background()
	refused := false
	check := func(res domain.ToolResult) {
		t.Helper()
		if strings.Contains(res.Content, secretMarker) {
			t.Fatalf("%s handed the internal page to the model: %s", res.Name, res.Content)
		}
		if res.IsError && (res.Content == pageUnavailableMsg || res.Content == navFailedMsg) {
			if len(res.Images) != 0 {
				t.Fatalf("%s refused the page but still attached %d images", res.Name, len(res.Images))
			}
			refused = true
		}
	}

	check(newNavigateTool(session).Execute(ctx, fmt.Sprintf(`{"url":%q}`, attacker.URL)))

	shot := newScreenshotTool(session)
	for deadline := time.Now().Add(15 * time.Second); !refused && time.Now().Before(deadline); {
		check(shot.Execute(ctx, `{}`))
		if refused {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !refused {
		t.Fatal("an iframe pointed at an internal destination was screenshotted for the model")
	}
}

// readDOMPage is the shape that made a real run spin: the element the developer
// added is in the DOM but hidden, one element renders no text at all, and a
// counter changes the page's text between two identical reads.
const readDOMPage = `<!DOCTYPE html>
<html><head><title>Read DOM</title></head>
<body>
  <div id="visible">visible text</div>
  <div id="drawer" style="display:none"><button id="android-btn" aria-label="Add Android">Android</button></div>
  <div id="empty"></div>
  <span id="clock"></span>
  <script>setInterval(function(){document.getElementById('clock').textContent = String(performance.now());}, 50)</script>
</body></html>`

// TestReadDOMAnswersTheQuestionItWasAsked covers what a UI check actually needs
// from a DOM read, each case being one the old implementation answered with
// silence: a selector that is not there (30 seconds, then "context deadline
// exceeded"), an element with no rendered text (an empty tool result), and a
// hidden element (an empty tool result again, indistinguishable from absence).
func TestReadDOMAnswersTheQuestionItWasAsked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, readDOMPage)
	}))
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	ctx := context.Background()
	read := newReadDOMTool(session)
	if res := newNavigateTool(session).Execute(ctx, fmt.Sprintf(`{"url":%q}`, srv.URL)); res.IsError {
		t.Fatalf("navigate: %s", res.Content)
	}

	t.Run("missing selector fails fast and names what is there", func(t *testing.T) {
		start := time.Now()
		res := read.Execute(ctx, `{"selector":"#not-here"}`)
		if !res.IsError {
			t.Fatalf("a selector matching nothing must be an error: %s", res.Content)
		}
		if elapsed := time.Since(start); elapsed > executeTimeout/2 {
			t.Fatalf("took %s; a missing selector must not burn the execute timeout", elapsed)
		}
		for _, want := range []string{"#not-here", "button#android-btn", srv.URL} {
			if !strings.Contains(res.Content, want) {
				t.Fatalf("missing-selector answer does not mention %q: %s", want, res.Content)
			}
		}
	})

	t.Run("every read says where it read from", func(t *testing.T) {
		res := read.Execute(ctx, `{"selector":"#visible"}`)
		if res.IsError || !strings.Contains(res.Content, srv.URL) || !strings.Contains(res.Content, "Read DOM") {
			t.Fatalf("read does not carry url and title: %q err=%v", res.Content, res.IsError)
		}
		if !strings.Contains(res.Content, "visible text") {
			t.Fatalf("read lost the element text: %s", res.Content)
		}
	})

	t.Run("empty and hidden elements explain themselves", func(t *testing.T) {
		res := read.Execute(ctx, `{"selector":"#empty"}`)
		if res.IsError || strings.TrimSpace(res.Content) == "" {
			t.Fatalf("an element with no text must still answer: %q err=%v", res.Content, res.IsError)
		}
		if !strings.Contains(res.Content, "as_text:false") || !strings.Contains(res.Content, "contains") {
			t.Fatalf("empty read does not say what to do next: %s", res.Content)
		}
		res = read.Execute(ctx, `{"selector":"#drawer"}`)
		if res.IsError || !strings.Contains(res.Content, "NOT visible") {
			t.Fatalf("a hidden element must be reported as hidden: %q err=%v", res.Content, res.IsError)
		}
	})

	t.Run("contains finds a hidden element and denies an absent one", func(t *testing.T) {
		res := read.Execute(ctx, `{"selector":"body","contains":"Android"}`)
		if res.IsError {
			t.Fatalf("contains search failed: %s", res.Content)
		}
		if !strings.Contains(res.Content, "button#android-btn") {
			t.Fatalf("contains did not locate the hidden button: %s", res.Content)
		}
		if !strings.Contains(res.Content, "NOT visible") {
			t.Fatalf("contains did not report the button as hidden: %s", res.Content)
		}

		res = read.Execute(ctx, `{"selector":"body","contains":"Windows Phone"}`)
		if res.IsError {
			t.Fatalf("a search that finds nothing is an answer, not an error: %s", res.Content)
		}
		if !strings.Contains(res.Content, "appears nowhere") {
			t.Fatalf("absent string not reported as absent: %s", res.Content)
		}
	})

	t.Run("an invalid selector is named as one", func(t *testing.T) {
		res := read.Execute(ctx, `{"selector":"#("}`)
		if !res.IsError || !strings.Contains(res.Content, "not a valid CSS selector") {
			t.Fatalf("invalid selector = %q err=%v", res.Content, res.IsError)
		}
	})
}

// TestInteractionFailuresNameTheCause covers the other half of the same
// problem: a click, a fill and a wait against an element that is absent, or
// present but invisible, used to spend the whole execute timeout and come back
// with "context deadline exceeded" — one message for three different bugs.
func TestInteractionFailuresNameTheCause(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, readDOMPage)
	}))
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	ctx := context.Background()
	if res := newNavigateTool(session).Execute(ctx, fmt.Sprintf(`{"url":%q}`, srv.URL)); res.IsError {
		t.Fatalf("navigate: %s", res.Content)
	}

	t.Run("click on a missing element lists what is there", func(t *testing.T) {
		start := time.Now()
		res := newClickTool(session).Execute(ctx, `{"selector":"#not-here"}`)
		if !res.IsError {
			t.Fatalf("clicking nothing must fail: %s", res.Content)
		}
		if elapsed := time.Since(start); elapsed > executeTimeout {
			t.Fatalf("took %s; a missing element must not cost the execute timeout twice", elapsed)
		}
		for _, want := range []string{"no element matches", "button#android-btn", srv.URL} {
			if !strings.Contains(res.Content, want) {
				t.Fatalf("click failure does not mention %q: %s", want, res.Content)
			}
		}
	})

	t.Run("click on a hidden element says it is hidden", func(t *testing.T) {
		res := newClickTool(session).Execute(ctx, `{"selector":"#android-btn"}`)
		if !res.IsError {
			t.Fatalf("clicking a hidden element must fail: %s", res.Content)
		}
		if !strings.Contains(res.Content, "none of them is visible") {
			t.Fatalf("hidden element not reported as hidden: %s", res.Content)
		}
	})

	t.Run("wait_for that times out says why", func(t *testing.T) {
		res := newWaitForTool(session).Execute(ctx, `{"selector":"#android-btn","timeout_seconds":2}`)
		if !res.IsError {
			t.Fatalf("waiting for a hidden element must fail: %s", res.Content)
		}
		if !strings.Contains(res.Content, "in the DOM") {
			t.Fatalf("wait failure does not distinguish hidden from absent: %s", res.Content)
		}
	})

	t.Run("fill on a missing input names the page", func(t *testing.T) {
		res := newFillTool(session).Execute(ctx, `{"selector":"#search","value":"x"}`)
		if !res.IsError || !strings.Contains(res.Content, "no element matches") {
			t.Fatalf("fill failure = %q err=%v", res.Content, res.IsError)
		}
	})

	t.Run("a successful click reports where it landed", func(t *testing.T) {
		res := newClickTool(session).Execute(ctx, `{"selector":"#visible"}`)
		if res.IsError || !strings.Contains(res.Content, srv.URL) {
			t.Fatalf("click result = %q err=%v", res.Content, res.IsError)
		}
	})

	t.Run("a screenshot says which page it is of", func(t *testing.T) {
		res := newScreenshotTool(session).Execute(ctx, `{}`)
		if res.IsError || !strings.Contains(res.Content, srv.URL) {
			t.Fatalf("screenshot result = %q err=%v", res.Content, res.IsError)
		}
	})
}

// responsivePage is broken the way real pages are: one fixed-width block that
// fits a desktop and hangs off the right edge of a phone, plus a carousel that
// parks content off-screen on purpose and must NOT be reported as breakage.
const responsivePage = `<!DOCTYPE html>
<html><head><title>Responsive</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  body { margin: 0; }
  #wide { width: 1200px; height: 40px; background: #c00; }
  #rail { overflow-x: auto; white-space: nowrap; }
  #rail div { display: inline-block; width: 900px; height: 30px; }
  @media (max-width: 768px) { #narrow-only { display: block; } }
  #narrow-only { display: none; }
</style></head>
<body>
  <div id="wide">fixed width block</div>
  <div id="rail"><div>carousel item</div></div>
  <div id="narrow-only">mobile menu</div>
</body></html>`

// TestViewportEmulationAndResponsiveReport is the check an agent could not make
// at all: switching to a phone (size, touch and user agent together) and being
// told, in words, whether the page holds up at that size.
func TestViewportEmulationAndResponsiveReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, responsivePage)
	}))
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	ctx := context.Background()
	viewport := newViewportTool(session)
	if res := newNavigateTool(session).Execute(ctx, fmt.Sprintf(`{"url":%q}`, srv.URL)); res.IsError {
		t.Fatalf("navigate: %s", res.Content)
	}

	desktop := viewport.Execute(ctx, `{"device":"desktop"}`)
	if desktop.IsError {
		t.Fatalf("desktop emulation failed: %s", desktop.Content)
	}
	if !strings.Contains(desktop.Content, "nothing overflows") {
		t.Fatalf("the desktop layout is fine and must be reported as such: %s", desktop.Content)
	}

	mobile := viewport.Execute(ctx, `{"device":"mobile"}`)
	if mobile.IsError {
		t.Fatalf("mobile emulation failed: %s", mobile.Content)
	}
	for _, want := range []string{
		"touch true",
		"mobile user agent true",
		"(max-width: 768px) true",
		"scrolls horizontally",
		"div#wide",
	} {
		if !strings.Contains(mobile.Content, want) {
			t.Fatalf("mobile report is missing %q: %s", want, mobile.Content)
		}
	}
	if strings.Contains(mobile.Content, "div#rail >") {
		t.Fatalf("content parked inside a scrolling container is not overflow: %s", mobile.Content)
	}

	// The emulation is the tab's state, not the call's: what follows must see
	// the phone too, or every check after the switch is a desktop check.
	shot := newScreenshotTool(session).Execute(ctx, `{}`)
	if shot.IsError || !strings.Contains(shot.Content, "390x844") {
		t.Fatalf("emulation did not persist into the screenshot: %q err=%v", shot.Content, shot.IsError)
	}

	custom := viewport.Execute(ctx, `{"device":"mobile","width":320}`)
	if custom.IsError || !strings.Contains(custom.Content, "320x") {
		t.Fatalf("custom width not applied: %q err=%v", custom.Content, custom.IsError)
	}

	if res := viewport.Execute(ctx, `{"device":"watch"}`); !res.IsError {
		t.Fatalf("an unknown device must be refused: %s", res.Content)
	}
}
