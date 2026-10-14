---
name: security-hard-exclusions-and-precedents
category: security
description: Use when a candidate finding falls into a noisy category - the do-not-report list, the precedents that settle recurring edge cases, and the narrow exceptions where an excluded category still counts
tech_stack: Secure review method
source: anthropics/claude-code-security-review (MIT) hard exclusions and precedents, adapted (the prompt-injection exclusion is replaced, not kept); cloudflare/security-audit-skill (MIT) domain core disciplines and availability criteria, adapted; OWASP and CWE cited by name only
---
# Hard Exclusions and Precedents

## Overview

Some categories produce far more noise than signal in a diff review, or are handled by another process. They are excluded up front so you never spend a finding — or a developer's revision — on them. Precedents settle the cases that come up every week, so two reviews of the same pattern reach the same answer.

**Core principle:** an exclusion is not a judgement that the issue is harmless; it is a judgement that this review is the wrong place for it.

## 1. Never Report

**Availability and load**
- Denial of service, resource exhaustion, memory or CPU blow-up, missing rate limiting, unbounded loops — except the narrow single-input cases in §4.
- Regex injection and ReDoS.

**Hardening without a vulnerability**
- Missing security headers, HSTS advice, TLS/HTTPS in local development, "should use a stronger setting" with no concrete attack.
- Missing input validation on a field that has no security effect.
- Missing audit logs; log spoofing / log injection.

**Out of this review's scope**
- Outdated libraries in general — dependency risk is judged by supply-chain-and-dependency-review, version by version.
- Memory-safety issues in memory-safe languages (Go, Java, Kotlin, C#, Rust safe code, Python, JS). Native code, `unsafe` blocks, cgo and FFI are in scope (native-memory-safety-and-ffi).
- Anything only in test files, fixtures, examples or documentation (Markdown, comments).
- Theoretical race conditions and timing issues without a demonstrated window and impact. A check-then-act race the attacker drives with two concurrent requests, for money, quota, a unique claim or a privilege, is not theoretical (business-logic-and-state-abuse §2).
- Secrets stored on disk with otherwise appropriate protection (a mounted secret file, an OS keychain).
- Pre-existing issues the diff did not introduce or enable.

**Usually noise — only at very high confidence**
- Open redirect, tabnabbing, XS-Leaks, prototype pollution: report only with a concrete chain to real impact (token theft via redirect in an OAuth flow, pollution reaching an auth decision or a `child_process` option).

## 2. Precedents

| Situation | Ruling |
|-----------|--------|
| Logging a secret, token, password or session id | Vulnerability (CWE-532) |
| Logging a URL | Fine — unless the URL carries a token or credential in the query |
| UUIDv4 ids in URLs | Unguessable; enumeration is not the attack — the missing ownership check is |
| Values from environment variables, CLI flags, server config files | Trusted input |
| ORM query builders, parameterised SQL | Safe; raw/`text()`/string-built SQL is not |
| React, Vue, Angular, Svelte templates | Safe by default; only `dangerouslySetInnerHTML`, `v-html`, `{@html}`, `bypassSecurityTrust*`, direct DOM sinks count |
| Missing permission check in client-side JS/TS or a mobile UI | Not a finding — the server is the boundary; check the server |
| SSRF or path traversal in browser code | Not a finding — the browser is the attacker's own machine |
| SSRF where the attacker controls only the path, not host or scheme | Not reported |
| Shell scripts and `.ipynb` notebooks | Injection only with a specific untrusted-input path into them |
| GitHub Actions inputs | Only when an untrusted event (fork PR, issue, comment) can trigger them; `workflow_dispatch` inputs come from people with write access |
| Resource leaks (unclosed files, connections) | Bugs, not vulnerabilities |
| Crashes, panics, 500s | Bugs, unless the error path itself leaks data or fails open |
| Hard-coded non-secret config (project ids, hostnames, public client ids) | Fine |
| Dev fallback such as `os.environ.get("SECRET_KEY", "dev")` | Fine — unless production demonstrably runs without the variable set |
| Publishable keys (Stripe `pk_`, Firebase web config, analytics write keys) | Fine by design |
| Secret in a test fixture | Excluded when it is a dummy or a documented test key; a real production credential pasted into a test is still a live secret |
| "Internal" service, private network, "only other services call it" | Not authentication. Judge by who can reach it; if that is decided outside the repository, it is a needs-validation note |
| JSON Schema, protobuf, zod, Pydantic or a typed DTO on the input | Proves shape, not authority, provenance or safe values |
| A `tenant_id` / `owner_id` column on the record | Not isolation until a query, policy or key on this path enforces it |
| A secret reference (vault path, env var name, secret ARN, key id) | Not a disclosed secret |
| Missing cookie flags, missing MFA prompt, missing least privilege, missing pinning | Hardening, unless a concrete path uses the gap |
| A user installing a plugin, extension or script into their own app | Not a boundary |
| A file, token or config readable only by the same OS user | Not a finding — the attacker is the victim |
| Different responses that reveal whether a public account exists | Not a finding; an oracle on a protected predicate (another tenant's record, a hidden field's value) is |
| `Access-Control-Allow-Origin: *` together with credentials | Browsers refuse it — the finding shape is a reflected or loosely matched origin with credentials |
| A system prompt or guardrail instruction | Not a control; tool permissions, scoping and approval are |

## 3. The Rule That Was Replaced

The upstream list excluded "user content in an AI system prompt". This agent does **not** keep that exclusion. Prompt injection IS a finding when the model that reads untrusted content also holds privileged tools, private data or an outbound channel: then the model is the attacker's instrument and the user or the organisation is the victim (llm-and-agent-security). Untrusted text reaching a model that can do nothing but answer the same user is still not a finding.

## 4. Where an Exclusion Stops

- **Test files excluded — but** a diff that changes production behaviour behind a test-only flag reachable in production (`if os.getenv("TEST_MODE")` disabling auth) is production code.
- **Documentation excluded — but** an agent instruction file (`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, a prompt template) is configuration an LLM executes; a hidden instruction or a permission change there is in scope.
- **Hardening excluded — but** removing an existing control (deleting CSRF middleware, setting `verify=False`, dropping `HttpOnly`) is a removed guard, judged by its concrete impact.
- **Availability excluded — but** one bounded input (not sustained volume) is in scope when the effect is persistent, amplified onto everyone, or spends operator money without limit:
  - a poisoned cache entry, or a persisted record or message that crashes every reader or blocks a shared queue partition until someone removes it by hand — integrity loss as much as availability;
  - a decompression, archive or image bomb expanded before any size check, on a shared worker, by an unauthenticated or low-trust sender;
  - a panic outside the request goroutine (Go's `net/http` recovers only the handler's own) or an unhandled rejection that kills the shared process, reachable from one request;
  - an untrusted trigger — an issue, an inbound email, a webhook, a public form — that starts work looping or fanning out paid API or model calls with no per-request budget, cap or cancellation.

  Write the input-to-cost path: the input, the work it triggers, the missing bound, who else is affected, and how the system recovers. MEDIUM by default; HIGH when unauthenticated and persistent. A missing rate limit, a slow endpoint or "many requests could…" stays excluded.

## Common Mistakes

- Reporting "no rate limit on login" — excluded; brute force is handled outside this review.
- Reporting `pk_live_` or a Firebase web `apiKey` as a leaked secret.
- Reporting a stack trace on a 500 as data exposure when it contains no secret, path or query.
- Flagging `{userInput}` in JSX as XSS.
- Dropping a prompt-injection finding because "user content in prompts is excluded" while the model can call `run_shell`.
- Using the availability exception for anything that needs sustained volume to hurt.

## Red Flags

- A comment full of excluded categories padding one real finding — cut them.
- An exclusion used to wave away a removed guard.
- A "test" helper imported from production code paths.
