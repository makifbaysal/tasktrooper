---
name: authn-sessions-and-tokens
category: security
description: Use when the diff touches login, password storage, sessions, cookies, JWT verification, OAuth/OIDC or SAML flows, MFA and step-up, passkeys/WebAuthn, account linking and recovery, password reset, webhook signatures, API keys, mTLS identity or headers used to identify a caller
tech_stack: Web & API
source: original; cloudflare/security-audit-skill (MIT) browser-session, federated-identity, MFA/passkey, account-transition, API-key and mTLS attack classes, adapted; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP Top 10:2025 A07, ASVS 5.0 V6/V7/V9/V10 and CWE cited by name only
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
- **Logout and invalidation:** a server-side session or refresh token that survives logout (only the cookie is cleared) — MEDIUM; sessions, refresh tokens and API tokens that survive a password change, an account disable or a role removal — HIGH when the old credential still writes.
- **Cookie scope:** a session cookie with `Domain=.example.com` is sent to every subdomain — including one that serves user content or a third-party app; a sibling cookie with the same name and a broader path can shadow it.
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

## 7. MFA and Step-Up

- Enrolling, replacing or disabling a second factor, regenerating recovery codes or adding a trusted device with only the first factor and no fresh authentication — an attacker with a stolen password makes MFA theirs.
- A step-up challenge that upgrades the session but is not bound to it (one tab completes MFA, another session gains the assurance), or to the action it was asked for (approve one transfer, perform another).
- An alternate route — the mobile API, a legacy endpoint, a resumed flow, a batch job — that performs the protected action without the assurance check the web route has.
- A "remember this device" token that is not bound to the user and the device, or survives a password reset.

## 8. Passkeys and WebAuthn

Use the library's verify call and read what it is given: the challenge must be the one issued to **this** session and consumed once; the RP ID and expected origin must be fixed server-side values; at authentication, the credential must belong to the account being signed in, and the `userHandle` → account mapping must come from the server's own record. Require user verification when the policy says so. A signature counter matters only where the product treats a regression as a clone signal.

## 9. SAML

- The element whose signature is verified is not the element the identity is read from (signature wrapping): verify, then take the assertion from the verified node, never by a fresh XPath over the whole document.
- An unsigned assertion accepted when the response is signed, or neither required.
- `Audience`, `Recipient`, `NotOnOrAfter` and `InResponseTo` not checked; assertions not recorded to stop replay.
- The XML parser for the response resolves DTDs or external entities (injection-and-dangerous-sinks §8).

## 10. Account Linking and Recovery

Draw the account before and after: each new edge — an IdP, a passkey, an email, a phone, a device — needs a current authenticated session, proof of ownership of the new identity, the step-up the policy requires, and callback state bound to the account that started it.

- Linking or signing in by an email the provider marks unverified (§4), or by an email the user typed and never confirmed.
- Changing the account email or phone without re-authentication or confirmation from the old address — the next password reset goes to the attacker.
- An invite accepted by whoever holds the link, joining the account the link names rather than the one signed in; invites keyed by email that collide across tenants.
- A reset token not bound to the user and the action, not single-use, or one that leaves existing sessions alive after the reset.

## 11. API Keys, Service Credentials and mTLS

- A key that authenticates for more than its server-side record grants — another tenant, environment or scope — because a request parameter (`?org=`, `X-Org-Id`) overrides the key's own binding.
- Lookup by key prefix without verifying the full secret; publishable and secret key types accepted interchangeably; a revoked key still accepted from a cache with no expiry.
- A client-certificate identity header (`X-Client-Cert`, `X-SSL-Client-S-DN`) trusted from any peer rather than only from the terminating proxy (http-cache-and-proxy-trust §5); a certificate subject's free text mapped to an account; a certificate that fails validation falling back to bearer-only or anonymous access.

## Severity Guide

Authentication bypass, forged tokens or SAML assertions accepted, or account takeover → CRITICAL. Reset-link host injection, unsigned state-changing webhooks, OAuth `state`/`redirect_uri` flaws, MFA enrollment without the existing factor, account linking without ownership proof, an API key reaching another tenant → HIGH. Fixation, logout that does not revoke → MEDIUM.

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
- An MFA enroll/disable, email-change or link-identity handler with no re-authentication.
- SAML code that verifies a signature and then queries the document again for the `NameID`.
- A request parameter or header that selects the tenant for an API-key-authenticated call.

## References (names and links only)

[OWASP Top 10:2025 A07 Authentication Failures](https://owasp.org/Top10/2025/) · [ASVS 5.0](https://github.com/OWASP/ASVS/tree/master/5.0/en) V6 Authentication, V7 Session Management, V9 Self-contained Tokens, V10 OAuth and OIDC · CWE-208, 290, 304, 347, 348, 384, 613, 640, 916 · [W3C WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/) · [OWASP SAML Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/SAML_Security_Cheat_Sheet.html) · [RFC 9700 OAuth 2.0 Security Best Current Practice](https://www.rfc-editor.org/rfc/rfc9700)
