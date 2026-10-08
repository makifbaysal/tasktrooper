---
name: secrets-and-crypto-review
category: security
description: Use when the diff adds or moves a credential, logs or returns data, builds a URL or argv with a token, generates a random value, encrypts, signs, hashes, configures TLS or writes a credential file
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording) and anthropics/claude-code security-guidance plugin (ideas only); OWASP Top 10:2025 A04/A07/A09 and CWE cited by name only
---
# Secrets and Crypto Review

## Overview

A committed secret is leaked the moment the branch is pushed; deleting it in a later commit does not un-leak it. Crypto bugs are the opposite: invisible in behaviour, obvious in the parameters. Both are found by reading values, not flows.

**Core principle:** secrets live only in the secret store and in memory; crypto parameters are chosen by the library's safe default, never hand-assembled.

## 1. Live Secrets in Code (CWE-798) — a blocking category

Recognisable shapes: `AKIA…`/`ASIA…` (AWS), `ghp_`/`github_pat_`/`gho_` (GitHub), `xoxb-`/`xoxp-` (Slack), `sk_live_`/`rk_live_` (Stripe), `-----BEGIN … PRIVATE KEY-----`, a GCP service-account JSON (`"type": "service_account"`), a database URL with a password, a JWT signing secret, a committed `.env`, `kubeconfig`, `.npmrc` with `_authToken`, `~/.docker/config.json`.

Decide whether it is **live** before blocking:

- Placeholder (`changeme`, `xxx`, `<token>`), documented test key (`sk_test_`), or a value the provider marks publishable (`pk_live_`, Firebase web config) → not a finding.
- Production-shaped, high-entropy, referenced by non-test code, or found by the pipeline's secret scanner as verified → live secret, block.
- **Never use the credential to test it.** Calling the provider with someone's key is not a review step.
- The fix always includes **rotation**, not only removal.
- Redact it in your comment to a recognisable prefix (`sk_live_51H…`).

## 2. Secrets Where They Leak

```go
// ❌ token on argv — visible to every local user via ps and /proc
exec.Command("curl", "-H", "Authorization: Bearer "+token, url)
// ✅ the child reads it from its environment or stdin
cmd := exec.Command("curl", "-H", "@-", url); cmd.Stdin = strings.NewReader("Authorization: Bearer " + token)
```

- **Logs (CWE-532):** a request, header map, config struct or exception that carries a token, password or session id. Logging a URL is fine; logging one with `?token=` is not.
- **URLs (CWE-598):** credentials in query strings end up in access logs, proxies, browser history and `Referer`.
- **Error responses (CWE-209):** connection strings, stack traces with secrets, upstream error bodies echoed to the client.
- **Client bundles:** `NEXT_PUBLIC_*`, `VITE_*`, `REACT_APP_*` variables and anything in a mobile binary are public. A server secret placed there is leaked by design.
- **argv (CWE-214):** see above.

## 3. Randomness (CWE-338)

```ts
// ❌
const token = Math.random().toString(36).slice(2);
// ✅
const token = crypto.randomBytes(32).toString("base64url");
```

| Stack | ❌ for secrets | ✅ |
|-------|----------------|----|
| Go | `math/rand`, `math/rand/v2` | `crypto/rand` (`rand.Text()` in Go 1.24+) |
| Python | `random` | `secrets.token_urlsafe`, `secrets.token_bytes` |
| JVM | `java.util.Random`, `kotlin.random.Random` | `SecureRandom` |
| Node / browser | `Math.random` | `crypto.randomBytes`, `crypto.randomUUID`, `crypto.getRandomValues` |
| C# | `System.Random` | `RandomNumberGenerator` |

Weak randomness is a finding only when the value is a secret: session ids, reset tokens, API keys, nonces, invite codes. Shuffling a UI list is not.

## 4. Encryption and Hashing (CWE-327, CWE-328, CWE-329)

- **ECB mode** (`AES/ECB/…`, Java's bare `Cipher.getInstance("AES")` defaults to ECB) → patterns leak.
- **Static or reused IV/nonce** — with AES-GCM, nonce reuse under one key breaks confidentiality and integrity.
- **Unauthenticated encryption** (CBC/CTR with no MAC) where ciphertext comes back from clients → tampering, padding oracles.
- **Hard-coded keys or IVs** in source.
- **MD5/SHA-1 for signatures, integrity of untrusted data or password-derived keys.** MD5 as a cache key or non-security checksum is fine.
- **RSA < 2048 bits, PKCS#1 v1.5 encryption** for new code; home-made constructions (XOR "encryption", custom MACs as `hash(key + msg)` → length extension; use HMAC).

```java
// ❌ ECB by default, static key
Cipher c = Cipher.getInstance("AES");
// ✅ AEAD with a fresh random 12-byte nonce per message
Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
c.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(128, nonce));
```

## 5. TLS Verification Disabled (CWE-295)

`InsecureSkipVerify: true` (Go), `verify=False` (requests/httpx), `rejectUnauthorized: false` or `NODE_TLS_REJECT_UNAUTHORIZED=0` (Node), a trust-all `X509TrustManager` or `HostnameVerifier` returning `true` (JVM), `ServerCertificateCustomValidationCallback = (…) => true` (.NET), `URLSession` delegates that accept any challenge (iOS). A finding when it is in a production code path talking to real hosts; a local test harness is excluded.

## 6. Credential Files (CWE-276, CWE-732)

```go
// ❌ world-readable token file
os.WriteFile(path, token, 0o644)
// ✅
os.WriteFile(path, token, 0o600)
```

Python: `open()` respects the umask — use `os.open(path, os.O_WRONLY | os.O_CREAT, 0o600)`. Only a finding on multi-user machines or shared volumes; say which.

## Severity Guide

Live production secret in code → CRITICAL. Secret in logs or error bodies reachable by other users → HIGH. TLS verification disabled on a production path → HIGH. Weak randomness for session or reset tokens → HIGH. ECB/static IV on data at rest → MEDIUM to HIGH by what it protects.

## Common Mistakes

- Blocking on `pk_live_`, a Firebase web `apiKey`, a Sentry DSN or an analytics write key.
- Blocking on `os.getenv("SECRET", "dev")` — excluded unless production demonstrably runs without the variable.
- Flagging MD5 used for an ETag or a cache key.
- Flagging `verify=False` inside a test or a local-only dev script.

## Red Flags

- A new file named `.env`, `*.pem`, `*.p12`, `id_rsa`, `credentials.json`, `service-account*.json`.
- A logger call that receives a whole request, headers map or config object.
- `Cipher.getInstance("AES")`, `AES.MODE_ECB`, a constant `iv =` or `nonce =`.
- `Math.random`, `math/rand`, `random.` near the words token, secret, code, nonce, session.
- A `.gitleaks.toml` or secret-scanner allowlist widened in the same diff that adds a credential.

## References (names and links only)

[OWASP Top 10:2025](https://owasp.org/Top10/2025/) A04 Cryptographic Failures, A07 Authentication Failures, A09 Security Logging and Alerting Failures · [ASVS 5.0 V11 Cryptography, V13 Configuration, V14 Data Protection](https://github.com/OWASP/ASVS/tree/master/5.0/en) · CWE-209, 214, 276, 295, 327, 328, 329, 338, 532, 598, 798
