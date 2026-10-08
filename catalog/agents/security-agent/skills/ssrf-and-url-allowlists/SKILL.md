---
name: ssrf-and-url-allowlists
category: security
description: Use when the server side of the diff fetches, renders, redirects to or validates a URL that input can influence - webhooks, link previews, import-from-URL, PDF renderers, OAuth/JWKS discovery, allowlist and blocklist checks
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP Top 10:2025 A01 and CWE-918 cited by name only
---
# SSRF and URL Allowlists

## Overview

Server-side request forgery turns your server into the attacker's network client — inside your VPC, past your firewall, next to the cloud metadata service. Since OWASP Top 10:2025 it sits under A01 Broken Access Control (CWE-918).

**Core principle:** validate the address you actually connect to, after DNS, on every hop — not the string the user typed.

## 1. Is It a Finding?

- The attacker must control the **scheme or host** (or the whole URL). Path-only control on a fixed host is excluded.
- The fetch happens **server-side**. SSRF in browser or mobile client code is not a finding.
- Impact: response returned to the attacker (full read), or side effects on internal services (blind). Blind SSRF still matters when internal endpoints act on GET.
- Cloud metadata raises severity: AWS IMDSv1 (`http://169.254.169.254/latest/meta-data/iam/security-credentials/`) answers a plain GET with credentials. IMDSv2 (PUT-token), GCP (`Metadata-Flavor: Google`) and Azure (`Metadata: true`) require a header a simple SSRF usually cannot set — check what the deployment runs before calling it CRITICAL.

## 2. Allowlist Bypasses

```ts
// ❌ substring: "https://example.com.evil.net" and "https://evil.net/?x=example.com" pass
if (!url.includes("example.com")) throw new Error("blocked");

// ❌ suffix without a dot boundary: "https://evilexample.com" passes
if (!new URL(url).hostname.endsWith("example.com")) throw ...

// ❌ regex with an unescaped dot and no end anchor: "https://api.example.com.evil.net"
if (!/^https:\/\/api.example.com/.test(url)) throw ...

// ✅ parse once, compare exact host (or a dot-bounded suffix), fixed scheme
const u = new URL(url);
const ok = u.protocol === "https:" && (u.hostname === "example.com" || u.hostname.endsWith(".example.com"));
```

Parser tricks that defeat string checks:

- **Userinfo:** `https://example.com@evil.net/` — the host is `evil.net`.
- **Backslash and parser differentials:** `https://evil.net\@example.com/` — validators and HTTP clients disagree (WHATWG vs RFC 3986). Validate with the same parser the fetcher uses, or better, the fetcher's resolved address.
- **Base-URL resolution:** `new URL(userPath, base)`, Python `urljoin(base, user)` and Java `URI.resolve` let `//evil.net/x` or a full `https://evil.net` replace the base host.

## 3. Private-Address Blocklist Bypasses

A blocklist on the URL string misses:

- alternate IPv4 forms: `http://2130706433/`, `http://0x7f.1/`, `http://0177.0.0.1/`, `http://127.1/`, `http://0.0.0.0/`;
- IPv6: `http://[::1]/`, `http://[::ffff:127.0.0.1]/`, `http://[fd00::1]/`;
- names that resolve inside: `localhost.`, `127.0.0.1.nip.io`, an attacker domain with an A record of `10.0.0.5`;
- **DNS rebinding:** the validator resolves to a public IP, the fetch re-resolves to `127.0.0.1`;
- **redirects:** the allowed URL answers `302 Location: http://169.254.169.254/…` and the client follows it.

```go
// ✅ the check runs on the address actually dialled — after DNS, on every redirect hop
dialer := &net.Dialer{Control: func(_, address string, _ syscall.RawConn) error {
	host, _, _ := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return errBlockedAddress
	}
	return nil
}}
client := &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: dialer.DialContext}}
```

`Proxy: nil` matters: through a proxy the dialled address is the proxy's, and the check never sees the target. Add any internal ranges the deployment uses (for example `100.64.0.0/10`). Node: a request-filtering agent that checks the resolved IP; Python: resolve, check with `ipaddress` (`is_private`, `is_loopback`, `is_link_local`, `is_reserved`), then connect to that IP with the original `Host` header and SNI.

## 4. Schemes Other Than HTTP(S)

`file://`, `gopher://`, `dict://`, `ftp://`, Java `jar:`/`netdoc:`, and everything libcurl supports. A fetcher that accepts a user URL must restrict the scheme to `https` (or `http`/`https`) before anything else.

## 5. Indirect Fetchers

- **HTML-to-PDF and screenshot services** (headless Chrome, wkhtmltopdf, Puppeteer) rendering user HTML: `<iframe src="http://169.254.169.254/…">`, `<img src="file:///etc/passwd">`.
- **SVG/image processors** that follow `xlink:href`; **XML parsers** with external entities (injection-and-dangerous-sinks); **OAuth/OIDC** flows fetching a `jwks_uri` or discovery document from a token-supplied issuer; **webhook test buttons**; **LLM browsing tools** whose URL comes from model output (llm-and-agent-security).
- An allowlisted host with an **open redirect** turns the allowlist into SSRF.

## Severity Guide

Full-read SSRF to internal services or credential-bearing metadata → HIGH to CRITICAL. Blind SSRF to an internal network with no known side effect → MEDIUM. Path-only control → not reported.

## Common Mistakes

- Reporting SSRF when the host is fixed in server config and the user controls only a path segment or query.
- Accepting a check on `url.hostname` as sufficient while the client follows redirects.
- Calling IMDS exposure CRITICAL without checking whether the platform enforces IMDSv2.
- Reporting `fetch(userUrl)` in a React component.

## Red Flags

- `http.Get(u)`, `requests.get(u)`, `fetch(u)`, `RestTemplate`/`WebClient`, `HttpClient.send` where `u` comes from a request body, a stored profile field or a webhook config.
- An allowlist implemented with `includes`, `contains`, `endsWith` without a leading dot, or an unanchored regex.
- Validation on the URL string followed by a client with default redirect following.
- A headless browser or PDF renderer fed user HTML with network access.

## References (names and links only)

[OWASP Top 10:2025 A01 Broken Access Control](https://owasp.org/Top10/2025/) · [CWE-918](https://cwe.mitre.org/data/definitions/918.html) · [OWASP API Security Top 10 2023 API7](https://owasp.org/API-Security/editions/2023/en/0x11-t10/)
