---
name: security-mcp-scanners
category: security
description: Use when semgrep, osv-scanner, github-security or snyk MCP tools appear in your tool list - what each server is for, how to call it safely, what data leaves the machine, and how its findings are triaged
tech_stack: Supply chain & CI/IaC
source: original; Semgrep MCP, OSV-Scanner MCP, GitHub MCP server and Snyk MCP documentation cited, not reproduced
---
# Security MCP Scanners

## Overview

Four security MCP servers can be connected for you: `semgrep`, `osv-scanner`, `github-security` and `snyk`. They ship disabled; the user turns them on in Settings → MCP servers. Your policy allows these four and no other server — a reviewer never holds the mutating MCP tools (Unity, Blender, Jupyter …) that other agents may use.

**Core principle:** an MCP scanner is a scanner. Its output is a list of candidates for the false-positive gate, never the verdict — and a scanner that failed is never a clean result.

## 1. Is It Connected?

- Availability is decided by your tool list, nothing else: tools from the `semgrep` server (named `mcp__semgrep__…` in CLI runtimes), `osv-scanner`, `github-security`, `snyk`. Not in the list → the server is not enabled; do not try to call it, do not ask for it, fall back to installed CLIs (read-only-security-scanners) and to reading.
- A listed tool that errors, times out, fails authentication or returns a parse error → a coverage limit for your summary line ("osv-scanner MCP: auth failed — lockfile not scanned"). Never treat it as "no findings".
- Order: `get_pipeline_status` first (CI may already have scanned this commit), then `github-security` (alerts GitHub already computed), then local scans (`semgrep`, `osv-scanner`), then `snyk` only when it adds coverage the others do not.

## 2. semgrep — local SAST

The official `semgrep mcp` server, started with metrics off. Code is scanned on this machine.

- Scan the changed files, not the repository.
- Pass rule packs explicitly: `p/default`, `p/owasp-top-ten`, `p/secrets`, plus the language pack for the stack (`p/golang`, `p/java`, `p/python`, `p/typescript` …). Never `auto` — it requires metrics and sends project information.
- The custom-rule tool is useful for a hypothesis: write a narrow rule that finds every call of the sink you suspect, or every sibling handler without the auth middleware. That is reading, and often faster than grep across languages.
- With a platform token configured, its findings tools show existing Semgrep platform results — read them like CI output.

## 3. osv-scanner — dependency advisories

`osv-scanner experimental-mcp`. Only package names and versions leave the machine (to osv.dev); no source code does.

- Scan the lockfiles and manifests this diff added or changed; fetch details for each advisory id it returns.
- A `MAL-` id, a likely typosquat, or a critical/high advisory with a fixed version on a new or changed dependency is a blocking category (supply-chain-and-dependency-review); pre-existing vulnerable dependencies are not this review.
- Never call a tool that records an ignore or suppression entry — that writes the repository's scanner config.

## 4. github-security — alerts GitHub already has

GitHub's hosted MCP server, narrowed to the `repos`, `pull_requests`, `code_security`, `secret_protection`, `dependabot` and `security_advisories` toolsets and held read-only by the server.

- **Code scanning:** list alerts for the PR's ref (`refs/pull/<number>/head`, the number from `get_task_pull_request`) or its branch. An alert on a `+` line is a candidate; an alert on untouched code is pre-existing.
- **Secret scanning:** open alerts on the repository, especially new ones on the PR's commits. Alert payloads can include the secret value — never copy it; redact to a recognisable prefix.
- **Dependabot:** alerts for manifests the diff touches; use them to confirm or contradict an OSV result.
- **Security advisories:** look up a package and version when OSV is not connected.
- It is read-only by construction: never try to dismiss, resolve or comment on alerts.

## 5. snyk — use with care

The Snyk CLI MCP server, authenticated with the user's token.

- **Snyk Code uploads source to Snyk's cloud.** Use it only because the user enabled the server — that is their consent for this repository — and never as the only evidence: a Snyk finding still needs your source → sink trace and the self-refute pass.
- **Snyk Open Source** resolves dependencies for some ecosystems (Gradle, Maven, sbt, Python without a lockfile) by invoking the project's package manager or build tool — that runs project code. Use it on lockfile ecosystems (`package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`); elsewhere prefer `osv-scanner`.
- If a scan asks you to grant trust to the checkout folder, decline and skip Snyk: trust is exactly the step that lets it run the project's tooling. Never call its authentication, logout or trust tools.
- IaC results: triage like Checkov or Trivy output (ci-cd-and-iac-security). Skip container scans — there is no image you are allowed to build.

## 6. Triage

1. Keep hits on `+` lines, or on unchanged lines the diff newly routes input into.
2. Drop hard exclusions (security-hard-exclusions-and-precedents).
3. Restate, trace, name attacker and victim (false-positive-gate). The tool's severity is a hint; yours goes in the comment.
4. Deduplicate: semgrep, GitHub code scanning and Snyk Code often report the same root cause — one SEC-n.
5. Cite the tool in the evidence line ("GitHub code scanning alert #42, semgrep rule …"), never paste raw output, never paste a secret.

## Common Mistakes

- Treating a missing MCP tool as a failure of the review — it simply is not connected.
- Treating "0 alerts" from `github-security` as approval while the code scanning analysis for the PR has not run yet.
- Running Snyk Open Source on a Gradle or Maven project.
- Using `auto` as the semgrep config.
- Copying a secret-scanning alert's secret into the task comment.

## Red Flags

- A plan step that enables, configures or installs an MCP server.
- A blocking finding whose only evidence is a scanner message.
- A scan that errored, reported as clean.
- A tool call that would dismiss an alert, write an ignore entry or trust a folder.

## References (names and links only)

[Semgrep MCP](https://semgrep.dev/docs/mcp) · [OSV-Scanner](https://google.github.io/osv-scanner/) · [GitHub MCP server](https://github.com/github/github-mcp-server) · [Snyk MCP](https://docs.snyk.io/integrations/developer-guardrails-for-agentic-workflows) · [OWASP Top 10:2025 A03](https://owasp.org/Top10/2025/)
