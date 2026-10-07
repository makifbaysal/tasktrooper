package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestGuardProseUnchanged pins the exact wording the destination guard hands
// back to the model, ahead of moving it into catalog/system/guards — see
// catalog/system/README.md. A mismatch here means the migration changed what
// an LLM reads, not just where the sentence lives.
func TestGuardProseUnchanged(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"chrome_not_found", chromeNotFoundMsg, "Chrome not found — install Google Chrome, Chromium or Microsoft Edge on this machine, or set CHROME_BIN to its executable, then retry."},
		{"nav_failed", navFailedMsg, "could not open that URL"},
		{"page_unavailable", pageUnavailableMsg, "could not use the current page"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestReadDOMNotFoundReportProseUnchanged pins notFoundReport's wording ahead
// of moving its instructive sentences into catalog/system/prompts/tool_results.
func TestReadDOMNotFoundReportProseUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		selector string
		probe    domProbe
		want     string
	}{
		{
			name:     "no anchors",
			selector: "#missing",
			probe:    domProbe{URL: "http://example.test/", Title: "Example"},
			want: "no element matches \"#missing\" after 3s.\nurl: http://example.test/\ntitle: Example\n" +
				"\nThe page has no elements with an id, no buttons, links or inputs at all — it is most likely blank or still loading. Check the url above is the one you meant.",
		},
		{
			name:     "with anchors",
			selector: "#missing",
			probe:    domProbe{URL: "http://example.test/", Title: "Example", Anchors: []string{"button#save", "a#home"}},
			want: "no element matches \"#missing\" after 3s.\nurl: http://example.test/\ntitle: Example\n" +
				"\nElements that ARE on this page:\n- button#save\n- a#home\n" +
				"\nPick one of these, or call browser_wait_for if the element renders later. Repeating this exact call will return this same answer.",
		},
	}
	for _, tc := range cases {
		if got := notFoundReport(tc.selector, tc.probe); got != tc.want {
			t.Errorf("%s: notFoundReport() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestReadDOMReadReportProseUnchanged pins readReport's instructive sentences
// ahead of the same move.
func TestReadDOMReadReportProseUnchanged(t *testing.T) {
	header := "url: http://example.test/\ntitle: Example\n"

	t.Run("contains not found", func(t *testing.T) {
		probe := domProbe{URL: "http://example.test/", Title: "Example", Matches: 1}
		got := readReport("#el", "widget", true, probe)
		want := header + "selector: #el — 1 match(es)\n" +
			"\n\"widget\" appears nowhere in the text or attributes of #el on this page. It is not rendered here — this is a definitive answer, do not re-read the DOM to confirm it. If you expected it, the page is stale (reload), the build did not include your change, or the element is on another route."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("not visible then empty text", func(t *testing.T) {
		probe := domProbe{URL: "http://example.test/", Title: "Example", Matches: 1, Visible: false, HTMLLen: 42}
		got := readReport("#el", "", true, probe)
		want := header + "selector: #el — 1 match(es)\n" +
			"note: this element is in the DOM but NOT visible (hidden, zero-size or transparent). What follows is what the page holds, not what a user sees.\n" +
			"\nThe element matched but its rendered text is empty (outer HTML is 42 chars, element is NOT visible). " +
			"Rendered text never includes hidden elements, icon-only buttons or attribute values — an empty read is NOT proof the content is missing. " +
			"Call again with as_text:false to read the HTML, or with contains:\"<what you are looking for>\" to search text and attributes."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("html empty", func(t *testing.T) {
		probe := domProbe{URL: "http://example.test/", Title: "Example", Matches: 1, Visible: true}
		got := readReport("#el", "", false, probe)
		want := header + "selector: #el — 1 match(es)\n" +
			"\nThe element matched but has no outer HTML, which should not happen — re-read with a different selector."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("html truncated", func(t *testing.T) {
		probe := domProbe{URL: "http://example.test/", Title: "Example", Matches: 1, Visible: true, HTML: "<div>x</div>", HTMLLen: 100}
		got := readReport("#el", "", false, probe)
		want := header + "selector: #el — 1 match(es)\n" +
			"html (100 chars, first 12 shown — use contains to search the rest instead of paging through it):\n<div>x</div>"
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})
}

// TestViewportResponsiveReportProseUnchanged pins renderResponsiveReport's
// instructive sentences ahead of the same move.
func TestViewportResponsiveReportProseUnchanged(t *testing.T) {
	header := "url: http://example.test/\ntitle: Example\n" +
		"viewport: 375x812 css px, dpr 2, touch true, mobile user agent true\n" +
		"css state: (max-width: 768px) true, (pointer: coarse) true\n"

	t.Run("no overflow", func(t *testing.T) {
		r := responsiveReport{URL: "http://example.test/", Title: "Example", Width: 375, Height: 812, DPR: 2, Touch: true, Mobile: true, CoarsePointer: true, NarrowMedia: true}
		got := renderResponsiveReport("mobile", r)
		want := "emulating: mobile\n" + header +
			"\nlayout: no horizontal scrolling and nothing overflows the viewport at this size."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("scrolls with no element pinned", func(t *testing.T) {
		r := responsiveReport{URL: "http://example.test/", Title: "Example", Width: 375, Height: 812, DPR: 2, Touch: true, Mobile: true, CoarsePointer: true, NarrowMedia: true, ScrollsX: true, DocWidth: 500}
		got := renderResponsiveReport("mobile", r)
		want := "emulating: mobile\n" + header +
			"\nlayout: the page scrolls horizontally — content is 500 px wide in a 375 px viewport.\n" +
			"No single element could be pinned down as the cause — a fixed width or a min-width on a container is the usual reason."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("overflow items reported", func(t *testing.T) {
		r := responsiveReport{
			URL: "http://example.test/", Title: "Example", Width: 375, Height: 812, DPR: 2, Touch: true, Mobile: true, CoarsePointer: true, NarrowMedia: true,
			OverflowCount: 1,
			Overflow:      []overflowItem{{Selector: "div.banner", Left: 375, Right: 600, Width: 225}},
		}
		got := renderResponsiveReport("mobile", r)
		want := "emulating: mobile\n" + header +
			"\nlayout: the page itself does not scroll horizontally, but content sticks out past the edge.\n" +
			"\n1 element(s) extend past the viewport:\n" +
			"- div.banner — spans x 375…600 (225 px wide)\n" +
			"\nElements inside a clipping or scrolling container are excluded, so these are real overflow. Take a screenshot to see them."
		if got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})
}

// TestExplainSelectorFailureProseUnchanged pins explainSelectorFailure's
// instructive sentences ahead of the same move. It needs a real Chromium —
// see testChromePath in integration_test.go — because the branches it takes
// depend on s.locate() actually probing a live page.
func TestExplainSelectorFailureProseUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Locate Test</title></head><body>
<div id="visible-el">shown</div>
<div id="hidden-el" style="display:none">hidden</div>
</body></html>`)
	})
	mux.HandleFunc("/blank", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Blank</title></head><body>just text, no anchors</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	session := NewSession()
	defer session.Close()

	navigate := func(t *testing.T, path string) {
		t.Helper()
		if err := session.run(context.Background(), executeTimeout, chromedp.Navigate(srv.URL+path)); err != nil {
			t.Fatalf("navigate: %v", err)
		}
	}
	someErr := errors.New("context deadline exceeded")

	t.Run("no match, no anchors", func(t *testing.T) {
		navigate(t, "/blank")
		res := explainSelectorFailure(context.Background(), session, "browser_click", "click", "#nope", someErr)
		want := "click failed: no element matches \"#nope\" on this page.\nurl: " + srv.URL + "/blank\ntitle: Blank\n" +
			"\nThe page has no elements with an id, no buttons, links or inputs at all — it is blank or still loading. Check the url above is the one you meant."
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("no match, with anchors", func(t *testing.T) {
		navigate(t, "/page")
		res := explainSelectorFailure(context.Background(), session, "browser_click", "click", "#nope", someErr)
		want := "click failed: no element matches \"#nope\" on this page.\nurl: " + srv.URL + "/page\ntitle: Locate Test\n" +
			"\nElements that ARE on this page:\n- div#visible-el\n- div#hidden-el (hidden)\n" +
			"\nUse one of these, or browser_read_dom with contains to search the page. Repeating this exact call will fail the same way."
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("matches but not visible", func(t *testing.T) {
		navigate(t, "/page")
		res := explainSelectorFailure(context.Background(), session, "browser_click", "click", "#hidden-el", someErr)
		want := "click failed: \"#hidden-el\" matches 1 element(s) in the DOM, but none of them is visible (hidden, zero-size or transparent), so it cannot be interacted with.\n" +
			"url: " + srv.URL + "/page\ntitle: Locate Test\n" +
			"\nThe markup is there and the rendering is not: open the container that holds it first, or fix the styling that hides it. browser_read_dom with contains shows its HTML."
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})

	t.Run("matches and visible but action failed", func(t *testing.T) {
		navigate(t, "/page")
		res := explainSelectorFailure(context.Background(), session, "browser_click", "click", "#visible-el", someErr)
		want := "click failed although \"#visible-el\" matches 1 visible element(s).\nurl: " + srv.URL + "/page\ntitle: Locate Test\n" +
			"\nThe element is on the page but the action did not complete — it may be covered by an overlay, disabled, or moving. Take a screenshot to see the state of the page before trying again."
		if res.Content != want {
			t.Errorf("= %q, want %q", res.Content, want)
		}
	})
}

// TestScreenshotBrokenImageWarningProseUnchanged pins the broken-image
// warning ahead of moving it into catalog/system/prompts/tool_results.
func TestScreenshotBrokenImageWarningProseUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping browser integration test in -short mode")
	}
	t.Setenv("CHROME_BIN", testChromePath(t))

	mux := http.NewServeMux()
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>Broken</title></head><body><img src="/nonexistent.png"></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	session := NewSession()
	defer session.Close()
	if err := session.run(context.Background(), executeTimeout, chromedp.Navigate(srv.URL+"/broken")); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	res := newScreenshotTool(session).Execute(context.Background(), `{}`)
	if res.IsError {
		t.Fatalf("screenshot failed: %s", res.Content)
	}
	want := "\n\nWARNING: 1 image(s) on this page FAILED TO LOAD and render as a broken-image placeholder:\n- /nonexistent.png\n" +
		"The page does NOT render correctly. Fix these before any \"looks correct\" verdict: the referenced asset file is missing, " +
		"its path is wrong, or it is not a real image. If the asset does not exist yet, download the real one with download_file."
	if len(res.Content) < len(want) || res.Content[len(res.Content)-len(want):] != want {
		t.Errorf("content = %q, want it to end with %q", res.Content, want)
	}
}
