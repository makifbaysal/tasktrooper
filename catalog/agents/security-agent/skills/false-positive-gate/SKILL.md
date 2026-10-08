---
name: false-positive-gate
category: security
description: Use when you have a candidate security finding and before it goes in a comment - restate it, prove reachability, impact and a concrete exploit, apply triage dismissals, then the self-refute pass naming attacker and victim
tech_stack: Secure review method
source: informed by trailofbits/skills fp-check (CC BY-SA 4.0, ideas only, own wording); anthropics/claude-code-security-review (MIT) false-positive filtering, adapted
---
# False-Positive Gate

## Overview

Reviewers — language models especially — are biased toward seeing vulnerabilities and toward rating them higher than they are. A pattern that looks dangerous ("string concatenated into a query", "path from a variable") is a candidate, not a finding. Every false block costs a developer round-trip and teaches the team to skim your comments; that is how the real one gets missed.

**Core principle:** try to kill each finding before you write it. What survives an honest attempt is worth blocking on.

## 1. Restate the Claim

Write it in one sentence, concretely: "An unauthenticated caller can send `name=../../etc/passwd` to `GET /export` and read any file the server can read." Many candidates collapse here — you cannot name who sends what to where, or the sentence is obviously not true once written down.

## 2. Five Conditions — All Must Hold

1. **Reachability.** The attacker controls the data at the source. Trace it: a value from config, an environment variable, a CLI flag, a constant, a server-generated id or an admin-only form is not attacker input for the user-facing threat model.
2. **Real impact.** Reaching the sink produces code execution, privilege escalation, data disclosure or integrity loss for someone other than the attacker. A crash, a 500 or an ugly log line is a bug, not a vulnerability.
3. **A concrete exploit.** You can write the request, the payload or a unit test that would demonstrate it. If you cannot, the confidence is below 0.8.
4. **The bounds hold.** Length limits, type coercion, an integer parse, an enum switch, a regex anchor, a UUID format check — read them; one of them often makes the payload impossible.
5. **No environment control fully blocks it.** A framework default (auto-escaping, parameter binding, CSRF middleware), a gateway rule visible in the repo, a network policy. Read it — "probably the WAF catches it" is not an argument either way.

## 3. Triage Dismissals

Drop the candidate when:

- **No threat model fits.** You cannot complete "an attacker with [capability] can [action] → [impact]".
- **Exploit from the heavens.** It needs a capability that already equals the impact: "an attacker who can edit the server's config file can make it run commands".
- **Not in actual usage.** The vulnerable function has no caller reachable from input (`grep_code` the symbol) — unless this diff adds one.
- **Documented, intended behaviour.** An admin tool that runs admin-supplied SQL; a CLI that reads the path its user gives it.
- **The cure is worse than the disease.** Hardening that would break the feature for a theoretical gain.
- **A CVE number alone.** An advisory against a library is evidence only when the vulnerable function or configuration is actually used here (supply-chain-and-dependency-review).

## 4. Self-Refute Pass (before anything blocks)

Name the **attacker** (who, with what access) and the **victim** (whose data or privilege). Then refute the finding if any is true:

- **Pre-existing.** The vulnerable line has no `+` and the diff adds no new path into it. Off-diff findings must name the enabling `+` or `-` line, or they are not this review's.
- **Already guarded.** A sanitiser, authorization check, type or allowlist on the path stops it. Open it; don't assume it exists and don't assume it works.
- **Harmless sink.** The value lands in a place where its content cannot do damage (a typed integer column, a text node React escapes, an `exec` argv slot after `--`).
- **Attacker == victim.** Self-XSS, a user reading their own data, a developer running their own script on their own machine.

## 5. Worked Examples

**Collapses at the restatement.**

```python
# candidate: "SQL injection in report query"
query = f"SELECT * FROM {REPORT_TABLES[kind]} WHERE org_id = %s"
cur.execute(query, (org_id,))
```

Restated: "an attacker controls `kind` and injects SQL". But `REPORT_TABLES[kind]` raises `KeyError` for anything not in a fixed dict of table names; the user value never reaches the string. No finding.

**Refuted after reading the router.**

```python
# + lines in this diff
@app.get("/files/<name>")
def download(name):
    return send_file(os.path.join(UPLOAD_DIR, name))
```

Candidate: path traversal. But Werkzeug decodes the URL before routing and the default string converter matches no `/`, so neither `../../etc/passwd` nor `..%2F..%2Fetc%2Fpasswd` reaches the handler, and `name=".."` resolves to a directory, which `send_file` refuses. Refuted — you read the converter instead of asserting traversal.

**Survives.**

```python
# + lines in this diff
@app.get("/files/<path:name>")
def download(name):
    return send_file(os.path.join(UPLOAD_DIR, name))
```

Attacker: any visitor — the route has no auth decorator, unlike its siblings. Victim: the server's own files. The `path` converter accepts the decoded slashes of `/files/..%2F..%2Fapp%2Fconfig.py`, `os.path.join` keeps the `..` segments, and `send_file` serves the file. HIGH (CWE-22), confidence 0.9; fix: `send_from_directory(UPLOAD_DIR, name)`, which rejects paths that escape the directory.

## 6. What the Survivor Carries

A finding that passed the gate goes into the comment with: the restated claim as the exploit sentence, the trace (`source file:line → sink file:line`), the confidence you can defend, and the fix. Keep the refuted ones out of the comment entirely — not even as "considered and dismissed".

## Common Mistakes

- Treating "I could not find a sanitiser" as proof there is none — look upstream at the router, the binder, the validator and the type.
- Calling a server-side config value "user input" because it reaches a dangerous function.
- Promoting a finding because the code is ugly or the pattern is famous.
- Blocking on a pre-existing issue the diff merely sits next to.
- Letting a scanner's severity label replace conditions 1–5.

## Red Flags

- The exploit sentence needs the words "if", "assuming" or "in some configurations" more than once.
- You cannot say what the attacker sends.
- The victim turns out to be the attacker.
- The finding's `file:line` points at a line the diff did not touch and you cannot name the line that did.
