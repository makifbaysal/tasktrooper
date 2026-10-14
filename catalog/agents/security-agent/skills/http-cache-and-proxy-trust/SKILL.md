---
name: http-cache-and-proxy-trust
category: security
description: Use when the diff reads Host or X-Forwarded-* headers, configures trusted proxies, builds absolute URLs or response headers from input, sets cache headers or CDN/reverse-proxy cache rules, or changes an ingress, auth proxy or custom HTTP parser - host and forwarded-header trust, cache poisoning and deception, header injection, trusted-proxy bypass and request framing
tech_stack: Web & API
source: cloudflare/security-audit-skill (MIT) HTTP-protocol attack classes, adapted to diff review; own wording; OWASP Top 10:2025 and CWE cited by name only
---
# HTTP Cache and Proxy Trust

## Overview

Between the client and the handler sit a CDN, a load balancer, an auth proxy, a reverse proxy, a framework router. Each one parses the request, and each can disagree with the next about which host it was for, which client sent it, which user it belongs to, and whether its response may be shared. Every finding in this skill names **two components** and the value they disagree about.

**Core principle:** a header the client can send is attacker input until a component you can point at strips or overwrites it. Find that component in the repository, or the claim is not confirmed.

## 1. Host and Forwarded Headers (CWE-348, CWE-644)

`Host`, `X-Forwarded-Host`, `X-Forwarded-Proto`, `X-Forwarded-For`, `X-Real-IP`, `Forwarded` and `X-Original-URL` decide real things: absolute URLs in emails and redirects, tenant routing by subdomain, the client IP an allowlist trusts, which scheme cookies are set for.

Framework switches that make them trusted:

| Stack | Setting |
|-------|---------|
| Express | `app.set("trust proxy", true)` trusts every hop; a hop count or subnet is the narrow form |
| Fiber v2 | `ProxyHeader` set while `EnableTrustedProxyCheck` is false or `TrustedProxies` is empty |
| Gin | `SetTrustedProxies(nil)` disables trust; the default trusts all — check `ClientIP()` users |
| Django | `USE_X_FORWARDED_HOST = True`, `SECURE_PROXY_SSL_HEADER`, `ALLOWED_HOSTS = ["*"]` |
| Werkzeug/Flask | `ProxyFix(x_for=…, x_host=…)` counts larger than the real number of proxies |
| Spring | `server.forward-headers-strategy=framework` without a proxy that overwrites the headers |

```go
// ❌ Fiber: c.IP() returns the client's own X-Forwarded-For value — the admin allowlist is bypassable
app := fiber.New(fiber.Config{ProxyHeader: fiber.HeaderXForwardedFor})
// ✅ only the load balancer's subnet may set it
app := fiber.New(fiber.Config{
	ProxyHeader:             fiber.HeaderXForwardedFor,
	EnableTrustedProxyCheck: true,
	TrustedProxies:          []string{"10.0.0.0/8"},
})
```

The reset-link variant (a token sent to the host the request named) is in authn-sessions-and-tokens §5.

## 2. Cache Poisoning Through Unkeyed Input

A request value changes the response, but the cache key does not include it — the first requester's response is served to everyone. Common unkeyed inputs: `X-Forwarded-Host` or `X-Forwarded-Scheme` reflected into links, script URLs or redirects; a language or device header; a cookie that selects content; query parameters the cache ignores.

Where the cache lives in a repo: CDN rules (Cloudflare page or cache rules, Fastly VCL, CloudFront behaviors), `nginx` `proxy_cache_key`, Varnish, framework caches (Next.js `fetch` cache and `unstable_cache`, Rails/Django page caching). A response that varies on a header without `Vary` on it, behind a cache that honours `Vary`, is the shape to look for.

## 3. Cache Deception and Private Responses

- An authenticated, personalized response sent with `Cache-Control: public`, `s-maxage`, or nothing at all behind a "cache everything" rule.
- Path tricks the router ignores but the cache keys on: `/account/settings/x.css`, `/api/me;.js`, `/profile%2F..%2Fstatic.png` — the app serves the account page, the cache stores it as a static asset.
- Next.js: a page reading cookies or headers rendered statically or with `revalidate`, so one user's render is cached for all.

```ts
// ❌ the session-specific JSON is cacheable by any shared cache
res.setHeader("Cache-Control", "public, max-age=300");
res.json(await loadAccount(req.user.id));
// ✅
res.setHeader("Cache-Control", "private, no-store");
```

## 4. Response Header Injection (CWE-113)

Most current frameworks reject CR/LF in header values — confirm the version before reporting. Still check custom writers and lower-level APIs, `Content-Disposition: attachment; filename="<user value>"` (quote and encode it), `Set-Cookie` built by concatenation, and `Location` built from input (open redirect rules in web-frontend-security §6).

## 5. Trusted-Proxy Bypass

An auth proxy (oauth2-proxy, an identity-aware proxy, an API gateway, a mesh sidecar) authenticates and then passes identity in a header — `X-Auth-Request-Email`, `X-Forwarded-User`, `X-Client-Cert`, `X-SSL-Client-DN`. The backend that trusts it is safe only if no one else can reach it.

- A second listener, port, `Service` of type `LoadBalancer`/`NodePort`, or health/legacy route reaches the backend without the proxy.
- The proxy forwards a client-supplied copy of the identity header instead of overwriting it.
- mTLS terminates at the proxy and the backend accepts the client-certificate header from any peer.

```yaml
# ❌ the backend that trusts X-Forwarded-User is also published straight to the internet
apiVersion: v1
kind: Service
metadata: { name: api }
spec: { type: LoadBalancer, ports: [{ port: 80, targetPort: 8080 }] }
```

Exposure in a manifest is source; whether the deployed network blocks it may not be — then it is a needs-validation note (source-visibility-needs-validation).

## 6. Request Framing and Desynchronization

Only in scope when the diff writes or configures a component that frames HTTP: a custom parser, a reverse proxy (`httputil.ReverseProxy` with a custom `Director` or `Rewrite`, a Node proxy), or front-end/back-end settings (`proxy_http_version`, `underscores_in_headers`, HTTP/2 downgrade, `Transfer-Encoding` handling). A smuggling finding names both parsers and the bytes each assigns to the next request; without that it is not reported.

## Severity Guide

Cache poisoning or deception serving one user's private data or a script to others → HIGH. Identity header trusted from a path that bypasses the auth proxy → CRITICAL when it grants any user's identity. IP allowlist bypass through a spoofed `X-Forwarded-For` → HIGH when it guards admin functions, MEDIUM otherwise. Header injection with no security-relevant response change → not reported.

## Common Mistakes

- Reporting a missing `Vary`, `Cache-Control` or security header with no shared cache in front of the response.
- Calling `X-Forwarded-For` spoofable without reading the trusted-proxy configuration that may already strip it.
- Asserting a smuggling issue from a single component.
- Treating a deployed CDN or proxy behaviour as known when its configuration is not in the repository.

## Red Flags

- `trust proxy` set to `true`, `ProxyHeader` without `EnableTrustedProxyCheck`, `ALLOWED_HOSTS = ["*"]`.
- `X-Forwarded-*`, `X-Original-URL` or `X-Real-IP` read for routing, authorization, tenant selection or URL building.
- `Cache-Control: public` or `s-maxage` on a handler that reads the session.
- An identity header (`X-User`, `X-Auth-*`, `X-Client-Cert`) read by a service that also has a public `Service`, port or route.

## References (names and links only)

[OWASP Web Security Testing Guide](https://owasp.org/www-project-web-security-testing-guide/) — Host header injection, HTTP request smuggling · [RFC 9111 HTTP Caching](https://www.rfc-editor.org/rfc/rfc9111) · [RFC 7239 Forwarded](https://www.rfc-editor.org/rfc/rfc7239) · CWE-113, 348, 444, 524, 525, 644 · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
