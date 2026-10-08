---
name: authn-sessions-and-tokens
category: security
description: Use when the diff touches login, password storage, sessions, cookies, JWT verification, OAuth/OIDC flows, password reset, webhook signatures, API keys or headers used to identify a caller
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP Top 10:2025 A07, ASVS 5.0 V6/V7/V9/V10 and CWE cited by name only
---
# Authentication, Sessions and Tokens

## Overview

Authentication bugs are rarely missing code; they are code that verifies the wrong thing, verifies with the wrong key, or trusts a value the attacker sends. Read every check as "what exactly is compared to what, and who chose each side?"

**Core principle:** the server decides the algorithm, the key, the audience and the identity — never the token, the header or the client.

## 1. Password Storage (CWE-916)

- ✅ argon2id, bcrypt (cost ≥ 10; note its 72-byte input limit), scrypt, or PBKDF2 with a high iteration count — through the framework's password hasher when there is one.
- ❌ MD5, SHA-1, SHA-256/512 (single or few rounds, salted or not), or reversible encryption of passwords.

```python
# ❌
user.password = hashlib.sha256(pw.encode()).hexdigest()
# ✅
user.password = argon2.PasswordHasher().hash(pw)   # or Django's make_password
```

## 2. JWT Verification (CWE-347)

```js
// ❌ decode() does not verify; the header's alg is trusted
const claims = jwt.decode(token);
// ❌ no algorithm pinned: an RS256 public key can be used as an HS256 secret
const claims = jwt.verify(token, publicKey);
// ✅
const claims = jwt.verify(token, publicKey, { algorithms: ["RS256"], audience: "api", issuer: ISS });
```

```python
# ❌
jwt.decode(token, options={"verify_signature": False})
# ✅
jwt.decode(token, key, algorithms=["RS256"], audience="api", issuer=ISS)
```

Also: `alg: none` accepted; `exp`/`aud`/`iss` not checked; the key chosen by a `kid` header used in a file path or SQL query; keys fetched from a token-supplied `jku`/`x5u` URL (SSRF and forged keys); a token accepted for a different audience than this service.

## 3. Sessions and Cookies

- **Fixation (CWE-384):** the session id is not rotated at login or privilege change.
- **Logout:** a server-side session or refresh token that survives logout (only the cookie is cleared) — MEDIUM.
- **Cookie flags:** `HttpOnly`, `Secure`, `SameSite` missing is hardening — report only with a concrete chain (a session cookie readable by script on a page with a real XSS sink in this diff).
- **Token in `localStorage`:** not a finding by itself; it raises the impact of an XSS.
- **Step skipping:** an endpoint reachable after the password step but before MFA completes treats the half-authenticated session as full.

## 4. OAuth 2 and OIDC

- `state` missing or not compared on callback → login CSRF / account linking to the attacker's identity (CWE-352).
- Public clients (SPA, mobile, desktop) without PKCE.
- `redirect_uri` validated by prefix or substring on the authorization server side → code/token theft.
- OIDC `nonce` not checked in flows that return an ID token from the front channel.
- An access token treated as proof of identity, or an ID token from another client accepted (missing `aud` check).
- Account linking or sign-in by email when the provider says `email_verified: false` → account takeover.

## 5. Identity from Headers (CWE-290, CWE-348)

```ts
// ❌ any client sets this header
const userId = req.header("X-User-Id");
// ❌ Express trusts every hop's X-Forwarded-For for IP allowlists
app.set("trust proxy", true);
```

Headers like `X-User-Id`, `X-Remote-User`, `X-Forwarded-For`, `X-Real-IP` identify a caller only when an authenticating proxy sets them and strips client copies — confirm that in the repo's deployment config, not by assumption. `X-Forwarded-Host`/`Host` used to build a password-reset or magic-link URL sends the token to the attacker's domain:

```python
# ❌ host header injection: the reset link points wherever the request's Host said
link = f"https://{request.headers['Host']}/reset?token={token}"
# ✅
link = f"{settings.PUBLIC_BASE_URL}/reset?token={token}"
```

## 6. Reset Tokens, API Keys and Webhooks

- Reset, invite and magic-link tokens: from a CSPRNG, single-use, short-lived, stored hashed (secrets-and-crypto-review for randomness).
- Webhook endpoints that change state (payment succeeded, user upgraded) with no signature verification → anyone can forge the event: HIGH.
- Missing timestamp tolerance on a signed webhook → replay: MEDIUM.
- MAC, API-key or token comparison with `==` instead of `hmac.Equal`/`subtle.ConstantTimeCompare`, `hmac.compare_digest`, `crypto.timingSafeEqual`, `MessageDigest.isEqual`: a non-blocking note at most — remote timing is rarely practical.

## Severity Guide

Authentication bypass, forged tokens accepted, or account takeover → CRITICAL. Reset-link host injection, unsigned state-changing webhooks, OAuth `state`/`redirect_uri` flaws → HIGH. Fixation, logout that does not revoke → MEDIUM.

## Common Mistakes

- Reporting missing rate limiting or lockout on login — excluded.
- Reporting user enumeration via different error messages as a block — MEDIUM at most, usually dropped.
- Calling `jwt.decode` unsafe in code that only reads claims of a token the server already verified upstream — check the middleware.
- Reporting a missing `SameSite` attribute with no state-changing endpoint relying on that cookie.

## Red Flags

- `verify=False`, `verify_signature: False`, `ignoreExpiration: true`, `algorithms` omitted.
- A login handler that does not regenerate the session.
- A reset or verification URL built from request headers.
- `trust proxy` set to `true`, or `X-Forwarded-*` read for an authorization decision.
- A webhook route with no signature check beside routes that have one.

## References (names and links only)

[OWASP Top 10:2025 A07 Authentication Failures](https://owasp.org/Top10/2025/) · [ASVS 5.0](https://github.com/OWASP/ASVS/tree/master/5.0/en) V6 Authentication, V7 Session Management, V9 Self-contained Tokens, V10 OAuth and OIDC · CWE-208, 290, 347, 348, 384, 916 · [RFC 9700 OAuth 2.0 Security Best Current Practice](https://www.rfc-editor.org/rfc/rfc9700)
