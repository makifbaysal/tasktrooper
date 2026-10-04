package htmldoc_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/htmldoc"
)

func sanitize(t *testing.T, doc string) string {
	t.Helper()
	out, err := htmldoc.Sanitize(doc)
	require.NoError(t, err)
	return out
}

func TestSanitizeDropsActiveAndDocumentLoadingElements(t *testing.T) {
	cases := map[string]string{
		"script":            `<script>alert(1)</script>`,
		"iframe":            `<iframe src="https://example.com"></iframe>`,
		"frame":             `<frameset><frame src="x"></frameset>`,
		"object":            `<object data="x.swf"></object>`,
		"embed":             `<embed src="x.swf">`,
		"link":              `<link rel="stylesheet" href="https://cdn.example.com/x.css">`,
		"meta http-equiv":   `<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		"base":              `<base href="https://evil.example/">`,
		"form":              `<form action="https://evil.example"><p>inside</p></form>`,
		"input":             `<input value="x">`,
		"button":            `<button>go</button>`,
		"textarea":          `<textarea>x</textarea>`,
		"select":            `<select><option>a</option></select>`,
		"svg script":        `<svg><script>alert(1)</script></svg>`,
		"svg set":           `<svg><a href="#"><set attributeName="href" to="javascript:alert(1)"/></a></svg>`,
		"noscript":          `<noscript><iframe src="x"></iframe></noscript>`,
		"img without https": `<img src="http://tracker.example/p.gif">`,
	}
	markers := map[string]string{
		"script": "<script", "iframe": "<iframe", "frame": "<frame", "object": "<object",
		"embed": "<embed", "link": "<link", "meta http-equiv": "http-equiv", "base": "<base",
		"form": "<form", "input": "<input", "button": "<button", "textarea": "<textarea",
		"select": "<select", "svg script": "<script", "svg set": "<set", "noscript": "<noscript",
		"img without https": "<img",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out := sanitize(t, "<!doctype html><html><head></head><body>"+body+"<p>kept</p></body></html>")
			assert.NotContains(t, strings.ToLower(out), markers[name])
			assert.Contains(t, out, "<p>kept</p>")
		})
	}
}

func TestSanitizeStripsEventHandlersAndScriptURLs(t *testing.T) {
	out := sanitize(t, `<body>
		<a href="javascript:alert(1)" onclick="x()">a</a>
		<a href="  JaVa&#x09;Script:alert(1)">b</a>
		<a href="vbscript:msgbox">c</a>
		<a href="data:text/html;base64,PHNjcmlwdD4=">d</a>
		<a href="data:application/xhtml+xml;base64,PHNjcmlwdD4=">x</a>
		<a href="data:text/plain,hi">p</a>
		<div onmouseover="x()" ONLOAD="y()">e</div>
		<svg><a xlink:href="javascript:alert(1)"><text>f</text></a></svg>
		<a href="https://example.com/ok">ok</a>
	</body>`)
	lower := strings.ToLower(out)
	assert.NotContains(t, lower, "javascript:")
	assert.NotContains(t, lower, "vbscript:")
	assert.NotContains(t, lower, "data:text/html")
	assert.NotContains(t, lower, "data:application")
	assert.NotContains(t, lower, "data:text/plain")
	assert.NotContains(t, lower, "onclick")
	assert.NotContains(t, lower, "onmouseover")
	assert.NotContains(t, lower, "onload")
	assert.Contains(t, out, `href="https://example.com/ok"`)
}

func TestSanitizeKeepsStylesSVGAndSafeImages(t *testing.T) {
	out := sanitize(t, `<!DOCTYPE html><html><head><meta charset="utf-8"><style>:root{--fg:#111}</style></head>
<body><section id="summary" style="color:var(--fg)"><h1>Report</h1>
<svg viewBox="0 0 10 10" aria-label="flow"><rect width="10" height="10"/></svg>
<img src="data:image/png;base64,iVBORw0KGgo=" alt="dot">
<img src="https://example.com/a.png" alt="remote">
</section></body></html>`)
	assert.True(t, strings.HasPrefix(out, "<!DOCTYPE html>"), "a full document comes back")
	assert.Contains(t, out, "<style>:root{--fg:#111}</style>")
	assert.Contains(t, out, `<meta charset="utf-8"/>`)
	assert.Contains(t, out, `style="color:var(--fg)"`)
	assert.Contains(t, out, `id="summary"`)
	assert.Contains(t, out, "<svg")
	assert.Contains(t, out, "<rect")
	assert.Contains(t, out, `src="data:image/png;base64,iVBORw0KGgo="`)
	assert.Contains(t, out, `src="https://example.com/a.png"`)
}

func TestSanitizeWrapsAFragmentInAFullDocument(t *testing.T) {
	out := sanitize(t, `<h1>Only a heading</h1>`)
	assert.True(t, strings.HasPrefix(out, "<!DOCTYPE html><html><head></head><body>"))
	assert.Contains(t, out, "<h1>Only a heading</h1>")
	assert.True(t, strings.HasSuffix(out, "</body></html>"))
}

func TestSanitizeIsIdempotent(t *testing.T) {
	once := sanitize(t, `<html><body><p onclick="x">a &amp; b</p><script>x</script></body></html>`)
	assert.Equal(t, once, sanitize(t, once))
}

func TestTextRendersReadableStructure(t *testing.T) {
	doc := `<!DOCTYPE html><html><head><title>ignored</title><style>body{color:red}</style></head><body>
<h1 id="summary">Analysis: CSV export</h1>
<p>Users   export a project&#39;s tasks &amp; download them.</p>
<h2>Plan</h2>
<ol><li>Add <code>TaskExporter</code></li><li>Wire the route<ul><li>GET /export</li></ul></li></ol>
<table><thead><tr><th>File</th><th>Change</th></tr></thead>
<tbody><tr><td><code>service.go</code></td><td>new | method</td></tr></tbody></table>
<pre><code>func Export() {
	return nil
}</code></pre>
<svg><title>Request flow</title><rect/></svg>
<p>Line one<br>Line two</p>
</body></html>`

	got := htmldoc.Text(doc)

	assert.NotContains(t, got, "ignored")
	assert.NotContains(t, got, "color:red")
	assert.NotContains(t, got, "<")
	assert.Contains(t, got, "# Analysis: CSV export")
	assert.Contains(t, got, "Users export a project's tasks & download them.")
	assert.Contains(t, got, "## Plan")
	assert.Contains(t, got, "1. Add `TaskExporter`")
	assert.Contains(t, got, "2. Wire the route")
	assert.Contains(t, got, "  - GET /export")
	assert.Contains(t, got, "| File | Change |")
	assert.Contains(t, got, "| service.go | new \\| method |")
	assert.Contains(t, got, "```\nfunc Export() {\n\treturn nil\n}\n```")
	assert.Contains(t, got, "[diagram: Request flow]")
	assert.Contains(t, got, "Line one\nLine two")
	assert.NotContains(t, got, "\n\n\n", "blank lines collapse")
}

func TestTextOfPlainTextIsThatText(t *testing.T) {
	assert.Equal(t, "just words", htmldoc.Text("just   words"))
}
