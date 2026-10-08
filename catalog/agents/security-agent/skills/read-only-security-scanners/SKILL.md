---
name: read-only-security-scanners
category: security
description: Use when you want deterministic evidence for a review - which already-installed scanners may be run against the checkout, their exact read-only invocations, how to triage their output, and what must never be run or installed
tech_stack: Secure review method
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); semgrep, gitleaks, TruffleHog, osv-scanner, govulncheck, gosec, Bandit, pip-audit, zizmor, Checkov and Trivy documentation cited, not reproduced
---
# Read-Only Security Scanners

## Overview

Scanners are fast, deterministic and noisy. They find candidates you might skim past; they also report hundreds of things this review excludes. They never decide the verdict — every hit goes through the false-positive gate like any other candidate, and a clean scan is not a clean review.

**Core principle:** use what is already installed, in a mode that only reads, on the files this diff changed. Never install anything; never run anything from the project.

## 1. First, What Did CI Already Run?

- `get_pipeline_status` — a secret scan, SAST or dependency-audit job may already have reported on this exact commit; its output is in the job result.
- `list_component_checks` — which security checks exist for the component and whether CI gates on them.

Do not re-run what CI already ran on this commit; triage its output instead. When `semgrep`, `osv-scanner`, `github-security` or `snyk` tools are in your tool list, those MCP servers are connected — security-mcp-scanners covers them; the CLIs below are the fallback.

## 2. Ground Rules

- **Installed only:** `command -v semgrep gitleaks trufflehog osv-scanner govulncheck gosec bandit pip-audit zizmor checkov trivy`. Missing → skip it; it is a coverage note, not a reason to fail the review.
- **Never install or fetch-and-run:** no `pip install`, `pipx run`, `uvx`, `npx`, `npm i -g`, `go install`, `go run …@latest`, `brew install`, `docker run` of a scanner image.
- **Never run project code:** no `npm install`/`ci`, no build, no `go generate`, no `make`, no CodeQL database creation for compiled languages (it runs the build) — read CI's CodeQL results instead.
- **Never modify:** no `--fix`, `--autofix`, `npm audit fix`, `osv-scanner fix`, no scanner baseline written into the repo.
- **Output goes outside the checkout:** `out=/tmp/tt-<task key>/scan; mkdir -p "$out"`. Pass `timeout_seconds` for slow tools.
- **Repo config is input, not authority:** a `.semgrepignore`, `.gitleaks.toml`, `.gitleaksignore`, `nosec`/`# nosemgrep` comment or `.trivyignore` added or widened in this diff is itself something to review.

## 3. Invocations

```sh
base=$(git merge-base HEAD origin/HEAD 2>/dev/null || git merge-base HEAD origin/main)
changed=$(git diff --name-only --diff-filter=AM "$base")
```

| Tool | Purpose | Read-only invocation | Caveat |
|------|---------|----------------------|--------|
| semgrep | SAST, many languages | `semgrep scan --metrics=off --config p/default --json -o "$out/semgrep.json" $changed` | `--config auto` needs metrics — don't; avoid `--baseline-commit`, it manipulates the git working tree; registry configs need network, a repo `.semgrep.yml` works offline |
| gitleaks | secrets in the diff's commits | `gitleaks git --log-opts="$base..HEAD" --redact --no-banner --report-format json --report-path "$out/gitleaks.json" .` | older v8: `gitleaks detect --log-opts=…`; working tree only: `gitleaks dir <path>` / `detect --no-git --source <path>` |
| trufflehog | secrets, broader detectors | `trufflehog git file://. --since-commit "$base" --no-update --no-verification --json` | verification calls the provider with the found credential — leave that to CI; `--no-update` stops self-update |
| osv-scanner | advisories for lockfiles | v2: `osv-scanner scan source -L <lockfile> --format json`; v1: `osv-scanner --lockfile=<lockfile> --format json` | reports `MAL-` malicious packages too; never `fix` |
| govulncheck | Go advisories with reachability | `GOTOOLCHAIN=local GOFLAGS=-mod=readonly govulncheck -json ./...` | loads packages via `go list` (may fill the module cache, never runs project code); `GOTOOLCHAIN=local` stops a `toolchain` line fetching another Go |
| gosec | Go SAST | `gosec -quiet -fmt=json -out="$out/gosec.json" ./...` | high noise on G104/G115 — triage hard |
| bandit | Python SAST | `bandit -q -f json -o "$out/bandit.json" <changed .py files>` | flags every `subprocess` import; only trace hits on `+` lines |
| npm / pnpm audit | JS advisories | `npm audit --package-lock-only --json` · `pnpm audit --json` | reads the lockfile only; no install, no scripts |
| pip-audit | Python advisories | `pip-audit --no-deps --disable-pip -r requirements.txt -f json` | without both flags it builds a venv and **installs** the requirements, running their setup code; needs pinned requirements |
| zizmor | GitHub Actions | `zizmor --offline --format json .github/workflows/` | `--offline` skips GitHub API audits |
| checkov | Terraform, k8s, Dockerfiles | `checkov -d <dir> --compact --quiet -o json` | never enable external module download |
| trivy | IaC misconfig, secrets, lockfiles | `trivy config <dir>` · `trivy fs --scanners vuln,secret,misconfig <dir>` | check `trivy --version` first: v0.69.4 is the malicious release from the March 2026 compromise (CVE-2026-33634) — do not run it, and say so in your run summary |

## 4. Triage

For each hit:

1. **Is it on the diff?** Keep hits on `+` lines, or on unchanged lines the diff newly routes input into. Drop the rest — pre-existing findings are not this review.
2. **Is it excluded?** DoS, ReDoS, test files, hardening-only rules, outdated-library-without-advisory → drop (security-hard-exclusions-and-precedents).
3. **Does it pass the gate?** Restate, trace source → sink, name attacker and victim (false-positive-gate). The scanner's severity is a hint; yours is the one in the comment.
4. **Deduplicate** across tools; one root cause is one SEC-n.
5. **Cite the tool** in the evidence line when it found the issue ("gitleaks: aws-access-token, config/prod.go:12"), never paste raw output.

## 5. Failures and Silence

- A scanner that errors, times out or cannot parse a file is a coverage limit — mention it in the summary line; it is neither a pass nor a finding.
- A scanner that finds nothing has only checked its own rules. Your source → sink reading still decides.

## Common Mistakes

- Pasting a scanner's 40 findings into the comment.
- Blocking on a scanner "HIGH" that is a test fixture, a constant or a pre-existing line.
- Running `pip-audit -r` without `--no-deps --disable-pip`, or `npx some-scanner` "just this once".
- Treating "semgrep: 0 findings" as approval.

## Red Flags

- A command in your plan that installs, builds, or fetches a tool to run it.
- Scanner output written into the repository checkout.
- A diff that adds a broad ignore rule to a scanner config next to new code the rule would have flagged.
- A CI scanner action referenced by tag instead of commit SHA (supply-chain-and-dependency-review).

## References (names and links only)

[Semgrep CLI](https://semgrep.dev/docs/cli-reference) · [gitleaks](https://github.com/gitleaks/gitleaks) · [TruffleHog](https://github.com/trufflesecurity/trufflehog) · [osv-scanner](https://google.github.io/osv-scanner/) · [govulncheck](https://go.dev/doc/security/vuln/) · [pip-audit](https://github.com/pypa/pip-audit) · [zizmor](https://docs.zizmor.sh/) · [Checkov](https://www.checkov.io/) · [Trivy](https://trivy.dev/) · CVE-2026-33634
