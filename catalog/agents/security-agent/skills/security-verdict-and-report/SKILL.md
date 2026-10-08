---
name: security-verdict-and-report
category: security
description: Use when deciding the verdict of a security review - severity and confidence scales, when to block, approve with notes, drop or reject as unreviewable, and the exact comment templates
tech_stack: Secure review method
source: anthropics/claude-code-security-review (MIT) severity and confidence scales, adapted; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP and CWE cited by name only
---
# Security Verdict and Report

## Overview

A security reviewer that blocks on noise gets ignored; one that passes on a hunch ships a breach. The verdict follows a fixed policy so the developer can predict it and the human can trust it.

**Core principle:** block only what you can prove is exploitable and new; say nothing about what you cannot.

## 1. Severity

| Severity | Meaning | Typical examples |
|----------|---------|------------------|
| CRITICAL | Exploitable now with little or no precondition; full compromise | RCE, auth bypass, SQL injection on a public endpoint, a live production secret committed, a malicious dependency |
| HIGH | Significant, reachable, one realistic precondition | IDOR on another user's data, stored XSS, SSRF to an internal service, path traversal reading server files, `pull_request_target` running PR code with secrets |
| MEDIUM | Needs specific conditions, or real but bounded impact | CSRF on a state change, reflected XSS behind an unusual flow, user enumeration that enables a further attack, verbose error leaking internal paths |
| LOW | Defence in depth, best practice | Missing header with no vulnerability, weak-but-unused config |

Exposure moves severity, not existence: "only reachable from the internal network" can still be HIGH when internal callers are not trusted. Use the deployment shape from `get_project_brief`.

## 2. Confidence

Confidence is about exploitability **in this code**, not about whether the pattern looks bad:

- **0.9–1.0** — you traced source → sink end to end and can write the exploit input.
- **0.8–0.9** — a known vulnerable pattern; source and sink both read; no guard found on the path.
- **0.7–0.8** — suspicious, but it needs a condition you could not confirm. Not reported.
- **< 0.7** — not reported.

## 3. The Decision

Evaluate every surviving finding, then apply the strongest outcome:

1. **Unreviewable** — part of the change could not be read (unreviewable-never-passes) → `need_revision`, naming exactly what could not be verified.
2. **Block** → `need_revision` when any holds:
   - a CRITICAL or HIGH finding introduced or enabled by the diff, confidence ≥ 0.8, surviving the exclusions and the self-refute pass;
   - a live secret in non-test code;
   - a new or changed dependency that is malicious, a likely typosquat, or carries a critical/high advisory with a fixed version available;
   - CI or agent escalation: `pull_request_target` with a PR-head checkout and secrets in reach, an agent permission-bypass flag, untrusted input reaching an LLM that holds tools.
3. **Approve with notes** → `ready_for_qa` plus one comment: MEDIUM findings at ≥ 0.8, at most three, highest first.
4. **Clean** → `ready_for_qa`, no comment.

LOW and < 0.8 findings are dropped: not "for awareness", not "minor", not mentioned.

## 4. Reject Template

One `add_task_comment`, before the move. The developer receives it alongside the architect's review, so it must stand alone.

```
Security review — changes requested

SEC-1 · HIGH · Broken object-level authorization (CWE-639, OWASP A01:2025) · internal/invoice/handler.go:57
Exploit: an attacker with any customer account can GET /api/invoices/{id} with another customer's id → reads their invoices (amounts, addresses).
Evidence: r.PathValue("id") (handler.go:52) → repo.Get(ctx, id) (repo.go:31, WHERE id = $1 only) → JSON response; no OrgID check on the path. Siblings use repo.GetForOrg.
Fix: use repo.GetForOrg(ctx, id, auth.OrgID(ctx)) and return 404 when it finds nothing; add a test where org B requests org A's invoice.

SEC-2 · CRITICAL · Hard-coded credential (CWE-798, OWASP A07:2025) · config/prod.go:12
Exploit: anyone with read access to the repository can use the Stripe live key → charges and refunds on the production account.
Evidence: const stripeKey = "sk_live_…" (prod.go:12), used by billing/client.go:20.
Fix: load it from the secret manager like the other keys; the key must also be rotated — removing it from the code does not un-leak it.

Reviewed: all 14 changed files; scanners: gitleaks (diff), semgrep p/default on changed files.
```

Per finding: severity · category (CWE-x, OWASP A0x:2025) · `file:line`; the exploit sentence ("an attacker with [capability] can [action] → [impact]"); the evidence trace with `file:line`; the fix. Redact secrets to a recognisable prefix — never paste the full value into a comment.

## 5. Approve-with-Notes Template

```
Security review — approved with notes

1. MEDIUM · CSRF (CWE-352, OWASP A01:2025) · web/routes/settings.ts:88 — POST /settings/email relies on the session cookie with SameSite=None and no CSRF token; a page the user visits can change their email. Add the csrf middleware its siblings use.
```

No praise, no list of what was fine, no LOW items, no more than three notes.

## 6. Move and Stop

- Comment first, then `move_board_task`. A move without its comment hands the developer a rejection with no reason, and the one-word fallback verdict cannot add one.
- When the move tool says your verdict is recorded — the card waits for the other reviewer(s), or went to need_revision because another reviewer asked for changes — the run is over. Never move it again.
- Your run's final message is the run summary, not a second comment: one line with the verdict and the count of findings.

## Common Mistakes

- Rating by category instead of by reachable impact: an "SQL injection" on a constant table name is not CRITICAL; it is not a finding.
- Blocking on a MEDIUM because there were several of them. Count does not raise severity.
- Pasting the full secret, the full scanner output or a stack of LOW items into the comment.
- Writing "possible", "might", "could potentially" in a blocking finding — if you would hedge it, it is below 0.8.
- Repeating the architect's points (tests, naming, design) to make the comment look thorough.

## Red Flags

- A blocking finding with no exploit sentence or no `file:line`.
- A rejection whose fix is "sanitise input" with no named function, parameter or check.
- An approval while a file in the diff was never opened.
- A notes comment on a clean approval that only says "looks good".

## References (names and links only)

[OWASP Top 10:2025](https://owasp.org/Top10/2025/) · [CWE Top 25 (2025)](https://cwe.mitre.org/top25/archive/2025/2025_cwe_top25.html) · [CVSS v4.0](https://www.first.org/cvss/v4-0/) (vocabulary only; the policy above decides)
