---
name: web-frontend-security
category: security
description: Use when the diff renders user-controlled content as HTML, builds links or redirects, changes CSP or CORS, adds a cookie-authenticated state change, a postMessage handler, a markdown renderer or an Electron/Tauri window
tech_stack: Web & API
source: anthropics/claude-code-security-review (MIT) framework precedents, adapted; informed by anthropics/claude-code security-guidance plugin (ideas only); OWASP Top 10:2025 A05/A01 and CWE cited by name only
---
# Web Frontend Security

## Overview

Modern template engines and UI frameworks escape by default. XSS comes back through the doors that switch escaping off, through the wrong escaper for the context, and through URLs. CSRF comes back whenever cookies authenticate a state change.

**Core principle:** find the escape hatch, then trace what flows into it. `{value}` in JSX is the control working — not a finding.

## 1. XSS Sinks by Framework (CWE-79)

| Framework / engine | Escape hatches to trace |
|--------------------|-------------------------|
| React / Preact / Solid | `dangerouslySetInnerHTML`, `innerHTML` props, refs writing `el.innerHTML` |
| Vue | `v-html`, render functions setting `innerHTML` |
| Angular | `bypassSecurityTrustHtml/Url/ResourceUrl/Script/Style` (`[innerHTML]` alone is sanitised) |
| Svelte | `{@html …}` |
| DOM / jQuery | `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `.html()`, `$(userString)`, `iframe.srcdoc` |
| Jinja2 / Django | `|safe`, `Markup(…)`, `mark_safe`, `{% autoescape off %}`, `autoescape=False` |
| Go | `template.HTML(userValue)`, `text/template` used to render HTML |
| JVM | Thymeleaf `th:utext`, JSP `<%= %>` or `escapeXml="false"`, FreeMarker `?no_esc` |
| Node templates | Handlebars `{{{ }}}`, EJS `<%- %>`, Pug `!=` |
| .NET | `@Html.Raw`, `MarkupString` |

```tsx
// ❌ user-authored bio rendered as HTML
<div dangerouslySetInnerHTML={{ __html: user.bio }} />
// ✅ sanitised with an allowlist sanitiser, or rendered as text
<div dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(user.bio) }} />
```

**Markdown renderers** count as HTML sinks when raw HTML is enabled: `marked` without a sanitiser, `markdown-it` with `html: true`, `react-markdown` with `rehype-raw` and no `rehype-sanitize`.

**URLs are sinks too.** `href`, `src`, `formaction`, `location.href = …`, `window.open(…)` with user data accept `javascript:` (and `data:` for some sinks). Validate the scheme (`https:`, `mailto:`) rather than relying on the framework's version-dependent handling.

```vue
<!-- ❌ profile website link -->
<a :href="user.website">site</a>
<!-- ✅ -->
<a :href="safeHttpUrl(user.website)">site</a>   <!-- returns "#" unless new URL(v).protocol is http: or https: -->
```

**Wrong escaper for the context:** HTML-escaping a value placed inside a `<script>` block, an event-handler attribute or a CSS value does not make it safe. Data embedded into an inline script needs JSON serialisation that escapes `<` (`</script>` breaks out).

Stored XSS (one user's content shown to another) → HIGH. Reflected → MEDIUM to HIGH by reach. Self-XSS → not a finding.

## 2. CSRF (CWE-352)

A finding when **all** hold: the endpoint changes state, the browser authenticates it automatically (cookies, HTTP auth), and nothing ties the request to the app's own pages (CSRF token, `SameSite=Lax/Strict` on the session cookie with no state-changing GET, an Origin/`Sec-Fetch-Site` check, a custom-header requirement the CORS policy enforces).

- APIs authenticated only by an `Authorization: Bearer` header are not CSRF-able.
- A new state-changing `GET` route is CSRF-able even with `SameSite=Lax`.
- Removing or bypassing the CSRF middleware for a route (`csrf_exempt`, `ignoringRequestMatchers`) is a removed guard.

## 3. CORS

```ts
// ❌ any site can read authenticated responses: the request's Origin is reflected with credentials
res.setHeader("Access-Control-Allow-Origin", req.headers.origin);
res.setHeader("Access-Control-Allow-Credentials", "true");
// ✅ exact allowlist
if (ALLOWED_ORIGINS.has(req.headers.origin)) res.setHeader("Access-Control-Allow-Origin", req.headers.origin);
```

`Access-Control-Allow-Origin: *` without credentials is fine for public data; browsers refuse `*` with credentials. Watch for `null` origin allowed and suffix-matched origins (ssrf-and-url-allowlists has the matching pitfalls).

## 4. postMessage (CWE-346)

```js
// ❌ any window can send a "token" message
window.addEventListener("message", (e) => { localStorage.setItem("token", e.data.token); });
// ✅
window.addEventListener("message", (e) => { if (e.origin !== "https://auth.example.com") return; … });
```

Also `target.postMessage(secret, "*")` — any framing or opening page can receive it.

## 5. CSP

A missing CSP is not reported. Adding `'unsafe-inline'`, `'unsafe-eval'`, a wildcard source or a JSONP-capable CDN to an existing policy is a weakened control: a note, or part of the finding when the same diff adds an injection sink.

## 6. Open Redirect (CWE-601) — high confidence only

Report only with a concrete chain: a post-login `?next=` that can point off-site in an OAuth or SSO flow (code or token leakage), or a redirect used to pass an SSRF allowlist. A bare redirect on a marketing page is dropped.

## 7. Desktop Shells (Electron, Tauri)

In a desktop shell, renderer XSS can become code execution.

- Electron: `nodeIntegration: true`, `contextIsolation: false`, `sandbox: false`, `webSecurity: false`; a preload exposing `ipcRenderer` or `require` wholesale through `contextBridge`; `shell.openExternal(url)` with an unvalidated scheme; IPC handlers that run commands or read paths from renderer arguments without validation; remote content loaded into a privileged window.
- Tauri: broad `allowlist`/capabilities (shell `open`, `fs` scopes of `**`), `dangerousRemoteDomainIpcAccess`.

## Common Mistakes

- Flagging `{user.name}` in JSX, `{{ name }}` in Vue/Jinja, or `[innerHTML]` in Angular.
- Reporting CSRF on a bearer-token API.
- Reporting a missing CSP, `X-Frame-Options` or HSTS as a vulnerability.
- Reporting an open redirect with no chain.

## Red Flags

- An escape hatch whose input traces back to another user's content.
- `DOMPurify` imported but the sink fed the unsanitised variable.
- `Access-Control-Allow-Credentials: true` next to a reflected origin.
- `addEventListener("message"` with no `origin` comparison.
- An Electron `BrowserWindow` created with `nodeIntegration: true` or loading remote URLs.

## References (names and links only)

[OWASP Top 10:2025](https://owasp.org/Top10/2025/) A05 Injection, A01 Broken Access Control · [ASVS 5.0 V3 Web Frontend Security](https://github.com/OWASP/ASVS/tree/master/5.0/en) · CWE-79, 346, 352, 601, 942 · [Electron security checklist](https://www.electronjs.org/docs/latest/tutorial/security)
