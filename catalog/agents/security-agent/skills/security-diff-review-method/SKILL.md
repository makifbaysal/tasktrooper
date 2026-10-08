---
name: security-diff-review-method
category: security
description: Use when starting any security review in code_review - risk-tier each changed file, read the repo's own security patterns, compare, then trace source to sink, including removed guards and blast radius
tech_stack: Secure review method
source: anthropics/claude-code-security-review (MIT) three-phase method, adapted; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP and CWE cited by name only
---
# Security Diff Review Method

## Overview

You are reviewing a change, not auditing a repository. The question is: **what can an attacker do after this diff that they could not do before?** Every finding answers it with a source you can point at, a sink you can point at, and the diff line that connects them.

**Core principle:** risk first, then evidence. Spend your reading on the files that can hurt someone; trace one real data flow end to end rather than skimming ten.

## 1. Size Up the Change

The injected diff starts with `git diff --stat` — every file it lists is under review. Get the base once and reuse it:

```sh
base=$(git merge-base HEAD origin/HEAD 2>/dev/null || git merge-base HEAD origin/main)
git diff --stat "$base"
git diff "$base" -- path/to/file      # each file the 24,000-byte cut left out
```

Tier every file before reading any of them closely:

| Tier | What lands here | Depth |
|------|-----------------|-------|
| HIGH | auth/session, access checks, crypto, secrets, deserialization, file paths, outbound URLs, shell/exec, SQL/templates, payments, dependency manifests and lockfiles, CI workflows, Dockerfiles, IaC, agent/MCP configs, anything an LLM with tools reads | full source → sink trace |
| MEDIUM | business logic, state changes, new public endpoints or exported functions, new config | trace the inputs it accepts |
| LOW | comments, copy, styling, tests, logging that carries no secret | scan for secrets only |

A refactor is HIGH until you have shown it is LOW: moved code is where a guard silently stays behind.

## 2. Phase 1 — Repository Context

Before judging new code, learn how this repository already defends itself. A finding is strongest when it says "every neighbour does X, this one does not".

- Auth and access: the middleware group, the `@PreAuthorize`/`@RolesAllowed` convention, the "load for owner" repository method, the tenant filter.
- Input handling: the validator, the query builder, the HTML escaper, the path helper, the URL fetcher with an allowlist.
- Secrets: how config is loaded (env, secret manager), what the logger redacts.
- Deployment shape: `get_project_brief` — a public multi-tenant service, an internal tool and a local desktop app have different attackers. It changes severity, never whether you trace.

```sh
grep_code "RequireAuth|authMiddleware|@PreAuthorize|@RolesAllowed|ownerID|tenant_id"
```

## 3. Phase 2 — Comparison

Ask how the new code deviates from the established secure pattern. The most common real finding is an asymmetry: one path validates, its sibling does not.

```go
// ❌ the new route is registered on the bare app, outside the authenticated group its neighbours use
api := app.Group("/api", authMW)
api.Get("/invoices/:id", h.GetInvoice)
app.Delete("/api/invoices/:id", h.DeleteInvoice)

// ✅ same group, same middleware chain as its siblings
api.Delete("/invoices/:id", h.DeleteInvoice)
```

Other asymmetries worth a second look: the create handler checks ownership and the update handler does not; the JSON endpoint escapes output and the CSV export does not; the web route is guarded and the new gRPC/GraphQL/WebSocket entry to the same service is not; the API validates and the background job consuming the same payload does not.

## 4. Phase 3 — Source to Sink

List the sources the diff touches, then follow each one to where it is used.

- **Sources:** query/path/body/header/cookie values, uploaded files and their names, webhook and queue payloads, rows other users wrote, file contents, deep links and intents, CI event fields (`github.event.*`), and model output.
- **Sinks:** SQL/NoSQL queries, shell and argv, file paths, outbound URLs, HTML/template rendering, deserializers, redirects, authorization decisions, crypto parameters, LLM prompts with tools.
- Follow the value across files with `expand_symbol_context` and `grep_code` — a sanitiser in a helper you did not open is the most common false positive; a sanitiser applied on one branch only is the most common miss.
- Write the trace down as you go: `source (file:line) → transform (file:line) → sink (file:line)`. It becomes the evidence line of the finding, or it shows you there is no finding.

## 5. Removed Code Is a Change Too

The `-` lines can be the vulnerability: a removed check, a dropped middleware, a loosened regex, a `private` that became `public`, a default flipped from deny to allow, `verify=True` deleted.

When a removed line looks protective, find out why it was there:

```sh
git blame "$base" -L 40,60 -- internal/files/handler.go   # who added the removed lines
git log -S 'filepath.EvalSymlinks' --oneline -- internal/files/   # when the guard came and went
git show <sha> --stat                                      # read the commit that added it
```

A removed line that came from a commit mentioning a CVE, a security fix or an incident is a red flag; removal without a replacement guard is a finding when the input is still reachable.

## 6. Blast Radius

A changed shared function changes every caller. When the diff touches a validator, an auth helper, a sanitiser, a default value or a base class, count the callers (`grep_code` the symbol) and check whether any of them now passes attacker input through a weaker path. Name the caller's `file:line` in the finding — "might affect callers" is not evidence.

## 7. Rules That Keep the Review Honest

- A comment or docstring that says "safe", "sanitised" or "trusted" is not evidence; the code on the path is.
- Look for **missing** controls, not only for added sinks: a new endpoint with no ownership check has no dangerous-looking line.
- Keep reading after the first finding; a diff with one hole often has its sibling.
- A new data flow into a pre-existing dangerous sink IS a finding of this diff, even though the sink line is old.
- Stop where the evidence stops: an area you could not verify is a coverage limit in the summary line, not a finding.

## Common Mistakes

- Reading the files in diff order instead of risk order, and running out of turns before the auth change at the bottom.
- Flagging a sink without reading the helper that sanitises its input two calls up.
- Treating a moved block as unchanged and missing the check that did not move with it.
- Reviewing the whole repository: unrelated pre-existing issues are not this review.

## Red Flags

- A refactor diff with a deleted `if` on the auth or validation path and no replacement.
- A new entry point (route, consumer, RPC, CLI subcommand, deep link) with no auth line anywhere near it.
- A change to a shared validator or sanitiser with dozens of callers and no test diff.
- A guard removed from code whose `git blame` points at a security fix.

## References (names and links only)

[OWASP Top 10:2025](https://owasp.org/Top10/2025/) · [OWASP ASVS 5.0](https://github.com/OWASP/ASVS/tree/master/5.0/en) · [CWE Top 25 (2025)](https://cwe.mitre.org/top25/archive/2025/2025_cwe_top25.html)
