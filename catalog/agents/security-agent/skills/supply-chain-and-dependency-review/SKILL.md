---
name: supply-chain-and-dependency-review
category: security
description: Use when the diff adds, upgrades, re-sources or re-pins a dependency - manifests, lockfiles, go.mod replace, Docker base images, GitHub Actions uses, Terraform modules, submodules, vendored code or curl-pipe-sh installs
tech_stack: Supply chain & CI/IaC
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP Top 10:2025 A03, OSV and deps.dev API documentation cited, not reproduced
---
# Supply Chain and Dependency Review

## Overview

A dependency is code you run without reviewing it. A diff that adds or changes one asks the reviewer to vouch for code that is not in the diff. You cannot read it all; you can check whether it is known-bad, misspelled, newly hijacked, or able to run code at install time. OWASP Top 10:2025 lists this as A03 Software Supply Chain Failures.

**Core principle:** judge the dependency change by evidence — advisories, identity, provenance. Unavailable data is never evidence of risk, and an absent measurement is never a clean verdict.

## 1. Find Every Dependency Change

```sh
git diff --name-only "$base" | grep -E 'package(-lock)?\.json|pnpm-lock|yarn\.lock|bun\.lock|go\.(mod|sum)|pom\.xml|\.gradle(\.kts)?|libs\.versions\.toml|requirements.*\.txt|pyproject\.toml|poetry\.lock|uv\.lock|Pipfile|Cargo\.(toml|lock)|Gemfile|Podfile|Package\.resolved|pubspec|\.csproj|packages\.lock\.json|Dockerfile|\.github/workflows|\.terraform\.lock\.hcl|\.gitmodules'
git diff "$base" -- package-lock.json | grep -E '^\+\s+"(node_modules/[^"]+|version|resolved|integrity|hasInstallScript)"'
```

Also: `curl … | sh` and `wget … | bash` in scripts and Dockerfiles, new `uses:` lines, `FROM` lines, `replace` directives, git URLs in manifests, vendored directories.

## 2. Advisories — Version-Matched

Query OSV for each new or changed `name@version`. `fetch_url` is GET-only, so the query goes through `run_terminal` (a network read, not project code):

```sh
curl -s https://api.osv.dev/v1/query \
  -d '{"package":{"name":"lodash","ecosystem":"npm"},"version":"4.17.20"}'
```

Ecosystem names: `npm`, `PyPI`, `Go`, `Maven`, `crates.io`, `RubyGems`, `NuGet`, `Packagist`, `Pub`. Details of one advisory: `fetch_url https://api.osv.dev/v1/vulns/<id>`. A cross-check with publish dates and advisory keys: `fetch_url https://api.deps.dev/v3/systems/npm/packages/<url-encoded name>/versions/<version>`.

Then judge it:

- **`MAL-…` id** (OpenSSF malicious packages) → the package is malware: block.
- **Critical/high advisory with a fixed version available**, on a dependency this diff added or changed → block, naming the fixed version.
- Critical/high with no fix yet, or an advisory whose vulnerable function or config is clearly not used → a non-blocking note.
- A pre-existing vulnerable dependency the diff did not touch → not this review.
- For Go, `govulncheck` (if installed — read-only-security-scanners) reports whether the vulnerable symbol is actually reachable.

## 3. Identity — Typosquats, Slopsquats, Confusion

- **Typosquat:** one edit from a popular name (`reqeusts`, `lodahs`, `crossenv`), a swapped scope (`@type/node` for `@types/node`), a separator change (`python-dateutil` vs `dateutil`).
- **Slopsquat:** a name an AI assistant invented that someone then registered. Tells: the package was first published days ago, has one version, no repository link, few or no dependents, and the diff's code uses it in a way that matches its name too neatly. Check the registry page or `deps.dev` package history.
- **Dependency confusion:** an internal package name that the build can also resolve from the public registry — `pip --extra-index-url` (both indexes searched), unscoped npm names with no registry pin in `.npmrc`, Maven/Gradle repository order putting a public repo before the private one.

A likely typosquat or confusion vector on a new dependency → block.

## 4. Install-Time Code Execution

- npm: `"hasInstallScript": true` in `package-lock.json` for a new package; `preinstall`/`install`/`postinstall` in its manifest.
- Python: an sdist-only release (no wheel) runs `setup.py` / build backends at install.
- Gradle/Maven plugins, Cargo `build.rs`, CocoaPods `prepare_command`.

Not a block by itself — many legitimate packages compile native code — but it turns any identity doubt into a block, and it is why you never install the dependency to look at it.

## 5. Provenance, Pinning and Drift

- **Lockfile drift:** a manifest change without a lockfile change (CI resolves something nobody reviewed); a lockfile change with no manifest change; `resolved` URLs pointing off the registry; an `integrity` field removed; ranges widened to `*`, `latest` or `>=`.
- **Source swaps:** a dependency moved to a fork, a git branch or a personal registry; a `replace` in `go.mod` pointing at a fork; an `http://` Maven repository or `allowInsecureProtocol`.
- **Pin by digest:** third-party GitHub Actions by full commit SHA, base images by `@sha256:`. Tags are mutable: `tj-actions/changed-files` tags were repointed to a secret-dumping commit in March 2025 (CVE-2025-30066), and in March 2026 76 of 77 `aquasecurity/trivy-action` tags and all `setup-trivy` tags were force-pushed to credential-stealing code alongside a malicious Trivy v0.69.4 release (CVE-2026-33634). An unpinned third-party action is a note; one pinned to a tag known to be compromised is a block.
- **Maintainer health:** archived upstream, single maintainer with a recent ownership transfer, a new major published by a new account. Signals for a note or for deeper checking — never a block alone.

## Severity Guide

Malicious package, likely typosquat, or dependency-confusion vector → CRITICAL. Critical/high advisory with a fix on a new or changed dependency → HIGH (block). Unpinned action, missing lockfile update, abandoned upstream → MEDIUM note.

## Common Mistakes

- Running `npm install`, `pip install` or `go get` to inspect a package — that executes it.
- Reporting "outdated version" with no advisory.
- Blocking on an advisory for a dev-only tool that never ships, or for a vulnerable function the code cannot reach — say why it does not apply instead.
- Treating "deps.dev returned nothing" as "the package is fine" or as "the package is suspicious".
- Re-reporting pre-existing vulnerable dependencies on every review.

## Red Flags

- A new package whose name is a near-miss of one already in the manifest.
- `resolved` pointing at a non-registry host, or `git+https` / tarball URLs in a lockfile.
- `--extra-index-url` added to a pip config, `requirements.txt` or Dockerfile.
- `curl | sh` of an unpinned URL in a Dockerfile or CI step.
- `uses: some-org/some-action@main` (or `@v1`) in a workflow that has secrets or write permissions.

## References (names and links only)

[OWASP Top 10:2025 A03 Software Supply Chain Failures](https://owasp.org/Top10/2025/) · [OSV API](https://google.github.io/osv.dev/api/) · [deps.dev API](https://docs.deps.dev/api/v3/) · [OpenSSF malicious-packages](https://github.com/ossf/malicious-packages) · [OpenSSF Scorecard](https://scorecard.dev/) · CVE-2025-30066, CVE-2026-33634
