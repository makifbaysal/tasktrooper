---
name: coverage-and-variant-pass
category: security
description: Use after the first trace of a diff and before the verdict - pick the attack-class skills each touched surface needs, give every risky surface a disposition, ask the coverage-critic questions (parallel paths, lifecycle modes, handoffs, second-order use), search variants of every HIGH/CRITICAL root cause, and run the literal obvious-things sweep
tech_stack: Secure review method
source: cloudflare/security-audit-skill (MIT) coverage ledger, coverage-critic, variant-search and obvious-things classes, adapted from a repository audit to a single diff; own wording
---
# Coverage and Variant Pass

## Overview

A diff review fails quietly in two ways: a risky surface nobody looked at gets approved, and one hole is reported while its twin three files over ships. Cloudflare's audit harness measured that a single pass finds roughly half of what repeated passes find. One review run cannot repeat itself, but it can do what that harness's coverage critic does: list what the diff touched, mark what was actually checked, and go back for the gaps before deciding.

**Core principle:** an approval claims that every risky surface in the diff was checked. Make that claim checkable — to yourself first.

## 1. Pick the Classes by Surface

After risk-tiering (security-diff-review-method), map each HIGH- or MEDIUM-tier file to the skills it needs and load them. A surface with no skill selected is a gap, not a pass.

| The diff touches | Load |
|------------------|------|
| a route, resolver, RPC, job or query that takes an id, a role, a request binding or a tenant | access-control-and-idor, data-isolation-and-lifecycle |
| a workflow with states, money, credits, quotas, approvals, invites, counters or expiry | business-logic-and-state-abuse |
| login, sessions, JWT, OAuth/OIDC, SAML, MFA, passkeys, account linking, API keys | authn-sessions-and-tokens |
| `Host`/`X-Forwarded-*` use, trusted-proxy settings, cache rules, CDN or reverse-proxy config, headers built from input | http-cache-and-proxy-trust |
| gRPC, GraphQL, protobuf, queue producers or consumers, pub/sub, inbound webhooks, event handlers | rpc-messaging-and-webhooks |
| query, shell/argv, template, eval, XML or deserializer built from input | injection-and-dangerous-sinks |
| file paths, uploads, archives, temp files | path-traversal-and-file-handling |
| a server-side fetch of a URL input can influence | ssrf-and-url-allowlists |
| credentials, logging, randomness, encryption, TLS | secrets-and-crypto-review |
| HTML rendering, links, CORS, CSP, postMessage, WebSockets, service workers, browser storage | web-frontend-security |
| a loopback server, Unix socket, named pipe, privileged helper, Electron/Tauri main process, URL scheme handler | desktop-and-local-ipc-security |
| iOS, Android, Flutter | mobile-app-security |
| C, C++, Objective-C, Rust `unsafe`, cgo, JNI, ctypes, Swift unsafe pointers, native plugins | native-memory-safety-and-ffi |
| a model call, tool, MCP config, prompt, agent memory or agent settings | llm-and-agent-security |
| manifests, lockfiles, `uses:`, `FROM`, vendored code, updaters, signing | supply-chain-and-dependency-review |
| workflows, Dockerfiles, Terraform, Kubernetes, Helm, cloud IAM | ci-cd-and-iac-security |
| Go, Java/Kotlin, TypeScript/Node, Python, game code | the matching `*-security-pitfalls` skill |

One path often crosses several domains: a webhook consumer that writes a tenant record and enqueues a refund job is rpc-messaging-and-webhooks, data-isolation-and-lifecycle and business-logic-and-state-abuse at once. Keep their checks together on that path.

## 2. Give Every Risky Surface a Disposition

Keep a private list — it is not the comment — with one line per HIGH-tier file or new entry point:

`surface — invariant checked — disposition (file:line that settles it)`

- **covered** — you read the path and the control holds; the line that enforces it is named.
- **finding** — it survived the false-positive gate.
- **needs validation** — the source trace is complete, and one decisive fact lives outside the repository (source-visibility-needs-validation).
- **blocked** — it could not be read (unreviewable-never-passes).

A surface with no disposition means the review is not finished. "Looked fine" is not a disposition: name the line that makes it fine.

## 3. Coverage-Critic Questions

Ask them once the first full trace is done, against the list:

1. **Unmapped entry points.** Does the diff add or change a route, handler, consumer, RPC method, resolver, CLI subcommand, deep link, scheduled job, migration or agent tool that is not on the list?
2. **Parallel paths.** Is the same state change reachable another way — batch or bulk, import, export, admin, legacy or `v1`, GraphQL beside REST, a gRPC stream beside the unary call, a WebSocket message, a background job, a retry handler? Does each path carry the same check? The effective control is the weakest path, not the most polished one.
3. **Lifecycle modes.** For each changed operation: what happens on error, retry, cancellation, timeout, partial failure, duplicate delivery, rollback or restore, migration or backfill, account switch, revoke or logout, and with the config missing or the flag off?
4. **Handoffs.** Where component A validates and component B consumes, does B assume more than A guarantees — truncation, normalization, case-folding, type coercion, units, tenant scope?
5. **Second-order use.** Does a value the diff stores become dangerous where it is read later — a slug that becomes a path, a name that becomes a JSON path or SQL identifier, escaped text rendered raw, a string that becomes a URL, regex, template or policy expression?
6. **Loaded but not run.** Is there a skill you selected whose checklist you never ran on the surface you selected it for?

Every "yes" goes back to tracing. The pass ends when a round adds nothing new; whatever is still open goes into the comment's `Reviewed:` line as a named coverage limit.

## 4. Variant Search

A HIGH or CRITICAL candidate is a pattern. Before writing it up, look for the pattern's siblings:

- **Lexical** — the same call or idiom: `grep_code "repo.Get(ctx"`, `grep_code "shell=True"`, `grep_code "trust proxy"`.
- **Structural** — the same shape under other names: other handlers registered outside the authenticated group, other consumers decoding the same message type, other resolvers on the same type.
- **Logical** — the same broken assumption elsewhere: every place that trusts the body's `orgId`, every cache key built without the tenant, every webhook that parses before it verifies.

Search the diff first, then the files the diff touches. One root cause that one fix closes is one SEC-n listing every location; independent missing controls are separate SEC-n, each with its own trace. Establish each variant's conditions and impact on its own — the sibling may be guarded where the original was not. A pre-existing sibling in code the diff did not touch is not raised; when the same fix would naturally cover it, say so in one clause of the fix line.

## 5. Obvious-Things Sweep

Literal, not creative — run it on every review, because everyone assumes someone else did:

```sh
git diff "$base" | grep -E '^\+' | grep -iE 'passw(or)?d|secret|api[_-]?key|token|bearer|BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|sk_live_|xox[abp]-|gh[pousr]_'
git diff "$base" | grep -E '^\+' | grep -iE 'TODO|FIXME|HACK|XXX' | grep -iE 'auth|secur|valid|permi|csrf|saniti|escape|verif|tenant'
git diff --name-only "$base" | grep -iE '(^|/)\.env|credential|\.pem$|\.key$|\.p12$|\.pfx$|id_rsa|\.npmrc$|\.pypirc$|kubeconfig'
```

Then read for:

- a debug or dev mode the diff adds or changes — can a request, header, query parameter or unset variable switch it on in production?
- a new `/debug`, `/admin`, `/internal`, `/metrics`, `/env`, `/actuator`, `/pprof` route — which middleware covers it?
- a seed, example or test credential that production code would accept;
- `.gitignore` or `.dockerignore` lines removed that kept secrets or local config out;
- `eval`, `exec`, `new Function`, `vm`, dynamic `import()` fed by input; CORS reflecting the origin with credentials; error responses carrying stack traces, SQL or internal paths.

Every hit is a candidate, not a finding. A cookie without `HttpOnly` that holds no credential, or a `TODO: add auth` on a route the group middleware already guards, is dropped at the false-positive gate.

## Common Mistakes

- Pasting the disposition list into the comment — only findings, needs-validation notes and named coverage limits reach the developer.
- Writing the comment at the first HIGH; its variant is the next most likely finding.
- Raising pre-existing variants in code the diff did not touch.
- Loading every skill "to be safe" and running none of their checklists.

## Red Flags

- An approval where a HIGH-tier file has no disposition.
- A fix suggested for one call site of a pattern the diff repeats in three.
- A new batch, import or export endpoint next to a single-item endpoint whose check it does not repeat.
- A new consumer, stream or job for an operation whose HTTP route carries a check the new path does not.
