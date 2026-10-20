---
name: screen-mockup-html
category: design
description: Use when you draw screens as the design task's HTML mockup - ONE canvas document holding every screen and every variant side by side, the sanitizer-safe recipe, phone and wide frames, the states grid, static control states, and the loopback self-check with screenshots at 375, 768 and 1440 attached to the task
---
# Screen Mockup HTML

## Overview

The design is ONE HTML document on the task: a canvas that holds every screen of the task and every variant of each screen, side by side, titled `design: <feature>` (`design: Invoices`). The review page opens it as a pan-and-zoom canvas, like Figma: frames sit at their real widths, every `<h2>` is a page listed beside the canvas, and the reviewer drags and pinches to move around, selects passages and comments on them. Developers receive the document in their run context as plain text. Nothing in it runs, loads or reacts — so every state is drawn, side by side, as its own static frame, and every decision a reviewer may question is text they can select.

**Core principle:** One file, one canvas, everything comparable. Draw states, don't simulate them. Lay each frame out by its own class, not by media queries — the review pane is not the device. Frames sit at their real widths in rows as wide as they need — never put frames in a scrolling box.

Never one document per screen or per variant: a reviewer who has to open five files cannot compare anything. The only reason to add a second document is the size budget (see Size), and then it splits by flow, each still a full canvas.

## Document anatomy

Keep these ids stable across revisions; review comments and the hand-off cite them.

1. `header#top` — `<h1>` the feature (`Invoices`); one meta line: task key, design system version (base and layer), date, content language.
2. `section#notes` — purpose in one sentence; per screen with variants: each variant's idea, whom it serves, what it costs, and which one you recommend (design-variants); the decisions on palette, type, imagery and layout and why (visual-craft, avoid-ai-default-looks); tokens or components this design proposes to add; draft copy; any state that does not apply, with the reason; the self-check result.
3. `section#structure` — the ASCII wireframes of every screen (each variant) at 375 and the wide width in `<pre>`. They survive the text rendition developers get; the pixels do not.
4. One page per screen — `section#screen-<slug>` with ONE `<h2>` (`Invoices — list`). Inside, one row per variant (`div.variant-row#screen-<slug>-a`, …) that opens with a `<h3 class="variant-title">Variant A — table first</h3>` and one line on the trade-off, then the strip of that variant's default frames: 375 and the wide width. Rows are stacked top to bottom, A above B, frames in the same order and left-aligned, so the same frame of two variants sits in the same column and the eye compares them directly. A screen with a single variant has one row.
5. One states page per screen — `section#states-<slug>`, `<h2>Invoices — list · states</h2>`: again one row per variant, every state at 375, and at the wide frame the default plus each state whose layout differs there; dark frames in the same grid.
6. `section#controls` — `<h2>Controls</h2>`: each control the feature uses in default, hover, focus-visible, active, disabled and loading, side by side. Shared by all screens, drawn once.
7. For a flow, `section#flow` — `<h2>Flow</h2>`: the screens as small frames in order with arrows (inline SVG) and the label of each transition, before the per-screen pages.

Every frame is a `<figure>` with an id carrying its screen slug and variant letter (`invoices-list-a-375-empty`), so a citation names one frame, and a `<figcaption>` (`A · 375 · empty — no invoices yet`). The wide frame is 1440 for web; for a mobile app it is a 768 tablet (`.w768`), plus a landscape phone frame when the screen supports landscape. Draw the platform's own chrome where it shapes the layout — browser or window chrome for a web page when the screen's context needs it, status bar and home indicator safe areas on iOS, the system bars on Android.

## Template

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>design: Invoices</title>
<style>
:root{--doc-bg:#F6F7F8;--doc-fg:#1F2328;--doc-muted:#5C6670;--doc-line:#D0D7DE;
--doc-sans:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;--doc-mono:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
@media (prefers-color-scheme:dark){:root{--doc-bg:#0D1117;--doc-fg:#E6EDF3;--doc-muted:#9198A1;--doc-line:#30363D}}
*{box-sizing:border-box}
body{margin:0;background:var(--doc-bg);color:var(--doc-fg);font:14px/1.55 var(--doc-sans)}
main{padding:24px}
h2{margin:56px 0 8px;font-size:20px}
h3.variant-title{margin:24px 0 2px;font-size:15px}
.variant-row{padding:0 0 24px;border-bottom:1px dashed var(--doc-line)}
pre{font:12px/1.35 var(--doc-mono);border:1px solid var(--doc-line);padding:12px;overflow:auto}
.strip{display:flex;gap:32px;align-items:flex-start;width:max-content;padding:8px 0 16px}
figure{margin:0;flex:none}
figcaption{font:12px/1.4 var(--doc-sans);color:var(--doc-muted);margin:0 0 6px}

.ds{--background:#FFFFFF;--foreground:#1C2024;--surface:#F4F6F5;--muted-foreground:#5A6560;--border:#D9DFDC;
--primary:#1F5F4A;--on-primary:#FFFFFF;--destructive:#B42318;--ring:#1F5F4A;
--radius-md:6px;--space-1:4px;--space-2:8px;--space-3:12px;--space-4:16px;--space-6:24px;
--font-sans:"Source Sans 3",-apple-system,"Segoe UI",Roboto,sans-serif;
background:var(--background);color:var(--foreground);font:16px/1.5 var(--font-sans)}
.ds.dark{--background:#111416;--foreground:#E8ECEA;--surface:#1A1F1D;--muted-foreground:#9AA5A0;--border:#2C3431;
--primary:#5FB894;--on-primary:#0B1F17;--ring:#5FB894}

.frame{border:1px solid var(--doc-line);overflow:hidden}
.w375{width:375px;min-height:720px}
.w1440{width:1440px;min-height:900px}
.w768{width:768px;min-height:1024px}
.w375 .layout{display:flex;flex-direction:column;gap:var(--space-4);padding:var(--space-4)}
.w1440 .layout{display:grid;grid-template-columns:240px 1fr;gap:var(--space-6);padding:var(--space-6)}

.btn{display:inline-flex;align-items:center;justify-content:center;gap:var(--space-2);min-height:44px;padding:0 var(--space-4);border-radius:var(--radius-md);font-weight:600}
.btn-primary{background:var(--primary);color:var(--on-primary)}
.btn-primary.is-hover{background:color-mix(in srgb,var(--primary) 88%,#000)}
.btn-primary.is-active{background:color-mix(in srgb,var(--primary) 78%,#000)}
.is-focus{outline:2px solid var(--ring);outline-offset:2px}
.is-disabled{opacity:.5}
.label{font-weight:600;font-size:14px}
.input{display:flex;align-items:center;min-height:44px;padding:0 var(--space-3);border:1px solid var(--border);border-radius:var(--radius-md);background:var(--background)}
.input.is-invalid{border-color:var(--destructive)}
.placeholder{color:var(--muted-foreground)}
.error-text{color:var(--destructive);font-size:14px}
.skeleton{display:block;background:var(--surface);border-radius:var(--radius-md);height:16px}
.pad{padding:var(--space-4)}
</style>
</head>
<body><main>
<header id="top">
  <h1>Invoices</h1>
  <p>D-14 · design system Harbor base v3 + web layer v1 · 2026-10-08 · content language: English</p>
</header>
<section id="notes"><h2>Notes</h2><ul><li>Purpose: find an invoice and see what is overdue at a glance.</li><li>Invoices — list, variant A (table first), recommended: finance staff scan 50+ invoices a day. Variant B (summary first) costs a new InvoiceSummary organism.</li></ul></section>
<section id="structure"><h2>Structure</h2>
<pre>375
[ Invoices            (New invoice) ]
[ Search: customer or number        ]
[ row: customer · number   amount   ]
[      due date            status   ]</pre></section>
<section id="screen-invoices-list"><h2>Invoices — list</h2>
<div class="variant-row" id="screen-invoices-list-a">
<h3 class="variant-title">Variant A — table first</h3><p>Optimises scanning many invoices; the summary moves below the table.</p>
<div class="strip">
  <figure id="invoices-list-a-375-default"><figcaption>A · 375 · default</figcaption>
    <div class="frame w375 ds"><div class="layout">
      <div><span class="label">Search invoices</span>
        <span class="input"><span class="placeholder">Customer or invoice number</span></span></div>
      <span class="btn btn-primary">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>
        New invoice</span>
    </div></div>
  </figure>
  <figure id="invoices-list-a-1440-default"><figcaption>A · 1440 · default</figcaption>
    <div class="frame w1440 ds"><div class="layout"><nav>…</nav><div>…</div></div></div>
  </figure>
</div></div>
<div class="variant-row" id="screen-invoices-list-b">
<h3 class="variant-title">Variant B — summary first</h3><p>Optimises the overdue overview; the table is one scroll down.</p>
<div class="strip">
  <figure id="invoices-list-b-375-default"><figcaption>B · 375 · default</figcaption>
    <div class="frame w375 ds"><div class="layout">…</div></div></figure>
  <figure id="invoices-list-b-1440-default"><figcaption>B · 1440 · default</figcaption>
    <div class="frame w1440 ds"><div class="layout">…</div></div></figure>
</div></div></section>
<section id="states-invoices-list"><h2>Invoices — list · states</h2>
<div class="variant-row">
<h3 class="variant-title">Variant A — table first</h3>
<div class="strip">
  <figure id="invoices-list-a-375-loading"><figcaption>A · 375 · loading</figcaption>
    <div class="frame w375 ds"><div class="layout"><span class="skeleton"></span><span class="skeleton"></span></div></div></figure>
  <figure id="invoices-list-a-375-error"><figcaption>A · 375 · error — invoices could not load</figcaption>
    <div class="frame w375 ds"><div class="layout"><p>Invoices couldn't load. Check your connection and try again.</p><span class="btn btn-primary">Retry</span></div></div></figure>
  <figure id="invoices-list-a-375-dark"><figcaption>A · 375 · dark · default</figcaption>
    <div class="frame w375 ds dark"><div class="layout">…</div></div></figure>
</div></div>
<div class="variant-row">
<h3 class="variant-title">Variant B — summary first</h3>
<div class="strip">…same states in the same order, so each column compares one state…</div></div></section>
<section id="controls"><h2>Controls</h2>
<div class="strip">
  <figure><figcaption>default</figcaption><div class="ds pad"><span class="btn btn-primary">New invoice</span></div></figure>
  <figure><figcaption>hover</figcaption><div class="ds pad"><span class="btn btn-primary is-hover">New invoice</span></div></figure>
  <figure><figcaption>focus-visible</figcaption><div class="ds pad"><span class="btn btn-primary is-focus">New invoice</span></div></figure>
  <figure><figcaption>active</figcaption><div class="ds pad"><span class="btn btn-primary is-active">New invoice</span></div></figure>
  <figure><figcaption>disabled</figcaption><div class="ds pad"><span class="btn btn-primary is-disabled">New invoice</span></div></figure>
  <figure><figcaption>field · invalid</figcaption><div class="ds pad"><span class="label">Customer email</span>
    <span class="input is-invalid">dana.ortiz@</span><span class="error-text">Enter an email address like name@company.com</span></div></figure>
</div></section>
</main></body></html>
```

The `:root` block styles the review page itself — neutral, not the product. The `.ds` block is the design system: every token you use, as a CSS variable, light on `.ds` and dark on `.ds.dark` — one place in the document, so a token change in review is one edit.

Every `<h2>` is a page: the review lists them like Figma's pages and jumps to one. So one `<h2>` per screen and one per screen's states — never one per variant: variants are rows inside the page, which is what puts them side by side. Use `<h3>` for the variant titles. Name pages by the screen (`Invoices — list`, `Invoices — list · states`), never by a decoration.

A strip runs as wide as its frames (`width:max-content`), wider than the page — that is the point; the canvas pans over it. No `overflow:auto`/`scroll` on a strip, a section or `main`, and no `overflow-x:hidden` on `body`: a scrolling strip shows one frame and a scrollbar, and a hidden one cuts the wide frames off. The canvas opens what it can, but the document should not depend on it.

## Drawing without form controls

| You need | Draw it as |
|---|---|
| button | `span.btn` + `.is-hover` / `.is-focus` / `.is-active` / `.is-disabled` / `.is-loading` |
| text field | a label above as text, `span.input` holding the value or a `span.placeholder` |
| checkbox, radio, switch | inline SVG box / circle / track next to its label text |
| select, combobox | `span.input` with the value and an inline SVG chevron |
| link | `a` with no `href`, or `span.link` |
| icon | inline SVG, path data copied from the icon library the repository already uses, sized by token |
| image | the product's own asset as `data:image/…` when small; otherwise an inline SVG placeholder with the image's purpose written in it |
| menu, dialog, tooltip, toast | its own frame showing it open, over the screen it belongs to |

Fonts: a system font stack by default, with the real face named first in `--font-sans` and in the notes. Embed the real face only when the repository ships it as a file: subset it and inline it as `@font-face{src:url(data:font/woff2;base64,…)}` — the viewer loads fonts from `data:` and from nowhere else. A Google Fonts `<link>` is dropped.

## Size

The whole canvas is one document: aim for under 600 KB, the hard limit is 1 MiB. Keep it small by styling with classes (never repeating long inline `style=""` runs), defining a repeated SVG icon once as a `<symbol>` in a hidden `<svg>` and referencing it with `<use href="#id">`, and keeping base64 images tiny. Repeated markup per state is fine; a large base64 image is not. Captions on every frame keep the text rendition readable for the developers who only get the text. Only when the canvas would pass about 900 KB — a many-screen flow drawn with every state — split it by flow into several documents (`design: <feature> — <flow>`), each a complete canvas of its own with its own notes; never split by screen or variant.

## Self-check

Write, serve, look, stop — all outside the repository, the one canvas document in one pass:

1. `mkdir -p /tmp/tt-<task key>/design`
2. Write the file with `run_terminal`: `cat > /tmp/tt-<task key>/design/<slug>.html <<'TT_HTML'` … `TT_HTML`. The quoted delimiter passes `$`, backticks and quotes through untouched; a long document goes in parts with `cat >>`.
3. Sanitizer check: `grep -nE '<(script|button|input|select|textarea|form|link|iframe)\b| on[a-z]+=' /tmp/tt-<task key>/design/<slug>.html` — expect no output.
4. Make the look-at copy: the real canvas has rows wider than any screenshot, so wrap them in a copy used only for looking — `sed 's#</style>#.strip{flex-wrap:wrap!important;width:auto!important}</style>#' /tmp/tt-<task key>/design/<slug>.html > /tmp/tt-<task key>/design/<slug>-look.html`. The attached document is the original, never the copy.
5. Serve it on loopback in the background and read the port: `nohup python3 -u -m http.server 0 --bind 127.0.0.1 --directory /tmp/tt-<task key>/design > /tmp/tt-<task key>/serve.log 2>&1 & echo $! > /tmp/tt-<task key>/serve.pid; sleep 1; cat /tmp/tt-<task key>/serve.log` → `Serving HTTP on 127.0.0.1 port <port>` (`python` instead of `python3` on Windows).
6. `browser_navigate` to `http://127.0.0.1:<port>/<slug>-look.html`. The response names the page's title — if it is not your document's, nothing after this is about your design.
7. For each width — `browser_set_viewport` `{device: "desktop"}` (1440), `{device: "tablet"}` (768), `{device: "mobile", width: 375}` — then `browser_screenshot` with `full_page: true`, NO `width`, `attach_to_task: true` and `title: "<slug>-<width>"` (`invoices-375`). A long canvas is read in parts: after the full-page shot, screenshot each screen's page by scrolling it into view (`browser_read_dom` with `contains` to find it) and title those `<slug>-<screen>-<width>`. A re-check after a fix re-takes the changed shots with `-r2` on the title, so the final shot is the one that says so.
8. Read every screenshot with the eye of a design reviewer, not a tester: each frame rendered with its caption; nothing spills out of its frame; the states are distinct; dark frames are dark; text is legible; icons are drawn, not blank — and then the visual-craft checklist: would this pass for a shipped, professionally designed product? Fix what would not. `browser_read_dom` with `contains: "<a caption>"` settles whether an ambiguous frame is there.
9. Stop the server: `kill "$(cat /tmp/tt-<task key>/serve.pid)"` — never by name (rule stop-what-you-started).

Collect every finding at all three widths, fix them in one pass, re-check once — two rounds at most. Write what you checked into `#notes` and the summary comment, naming the attached shots by title. If the browser tools are unavailable or fail to open the page, say exactly that ("self-check not run: <reason>") — never describe a look you did not take.

## Common Mistakes

- One document per screen or per variant — the human cannot lay them side by side. Everything goes on the one canvas.
- A bare wireframe look (grey boxes, one border colour, system-font text, no imagery) — the design is judged as a finished product, not a sketch (visual-craft).
- `@media` queries to switch the phone and desktop layouts — the review pane's width decides, not the frame's. Key the layout to `.w375` / `.w1440`.
- `:hover` styles — nobody can hover a sanitized static page. Draw `.is-hover` as its own frame.
- A `<button>` or `<input>` — dropped on save, the frame renders empty.
- Hard-coded hex in a component rule instead of `var(--token)`.
- Captions missing, so a reviewer cannot say which state a comment is about.

## Red Flags

- A frame that only exists at 1440.
- A document whose loading, empty or error frames are blank rectangles.
- A self-check claimed without an attached `browser_screenshot` of each document at each of the three widths.
- A second document for another screen or variant (unless the 900 KB split applies).
- Variants stacked in different column order, so the same state cannot be compared by eye.
