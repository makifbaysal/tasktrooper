---
name: automation-pipeline-integration
category: testing
description: Wire the automation test project into the repository's own pipeline so every task must pass the suite before it can leave QA
enabled: false
---
# Automation Pipeline Integration

## Overview

An automation suite that only runs when someone remembers it is decoration. The suite must run in the **repository's own pipeline** — the one this platform triggers automatically when a task moves to `ready_for_qa` — so a task cannot pass QA while the suite is red. Each repository runs its own tests in its own pipeline; there is no shared cross-project test job.

## How the platform runs it

When a task reaches `ready_for_qa`, the QA pipeline runs the repo's build and test stages: the repository's `test_command` override if set, otherwise language auto-detect. A red pipeline sends the task back to `need_revision` automatically with the failed stage's log; a green pipeline releases the task to QA dispatch. Check results with `get_pipeline_status`; re-trigger manually via the task's pipelines endpoint after a fix.

## Wiring the suite in

1. **One entrypoint command.** Give the automation project a single script that provisions everything and exits non-zero on any failure — e.g. `scripts/e2e.sh`: start disposable dependencies (Testcontainers/compose), boot the app from the current branch build, start WireMock stubs, run the suite, tear down.
2. **Make the pipeline call it.** Set the repository's `test_command` to run unit tests AND the automation entrypoint (`go test ./... && ./qa-automation/scripts/e2e.sh`, or the package.json script that does both). If the repo's CI (GitHub Actions etc.) is the source of truth for deploys, add the same entrypoint there too — same command locally, in the QA pipeline, and in CI, so green means the same thing everywhere.
3. **Keep it self-contained.** The pipeline environment provides no shared DB and no external network guarantees: the suite brings its own database container (test-database-seeding) and stubs (test-doubles-wiremock). If the environment has no container runtime, the entrypoint must fail loudly with a clear message — never silently skip tests and report green; raise the infra gap on the task instead.
4. **Keep it fast and deterministic.** Budget the suite (minutes, not an hour); parallel-safe, order-independent tests; no sleeps, no shared state. Flaky tests get fixed or quarantined with a task — a suite people rerun until green enforces nothing.

## Coverage reporters are mandatory

The platform parses the test job's log for a total coverage percentage and surfaces it on the pipeline and job — but only if the suite prints one in a format it recognizes. A suite with no coverage reporter enabled leaves that number blank, silently, and nobody is told the pipeline stopped proving anything about coverage. Every automation/unit suite wired into `test_command` must enable one:

- **Go:** `go test -cover ./...` (prints a `coverage: NN.N% of statements` line per package), or `go tool cover -func=coverage.out` after a `-coverprofile=coverage.out` run (prints the `total: (statements) NN.N%` summary line).
- **JS/TS (Jest/Vitest):** run with `--coverage` so the run ends with an istanbul text summary (`Lines : NN.N%` or the `All files | ... | NN.N |` table) in the job's stdout — not only an HTML/lcov file the log never shows.
- **Python (pytest):** `pytest --cov` with the default terminal report, which prints a `TOTAL ... NN%` line.

The report must land in the job's console output (stdout/stderr), because that is the only thing the platform's log-based parser can read — a coverage artifact written only to disk (`coverage.out`, `lcov.info`, `htmlcov/`) and never printed is invisible to it.

## The QA contract per task

- After the manual pass, this task's scenarios are added to the suite (e2e-automation-project).
- The suite — including the new tests — is green: run it yourself and/or confirm the pipeline (`get_pipeline_status`) after your changes land.
- Only then does the task move to `human_uat`, with the suite result quoted in the evidence comment.

New scenarios accumulate: every finished task widens the regression net that all future tasks must pass.

## Red Flags

- The automation project exists but no pipeline or CI job executes it.
- `test_command` runs unit tests only, and the e2e suite is "run manually sometimes".
- The entrypoint skips the suite when dependencies are missing and still exits 0.
- Tests pass locally but the pipeline lacks the runtime/config to run them — fix the pipeline, don't drop the tests.
- The suite runs but prints no coverage total (no `-cover`/`--coverage`/`--cov` flag), so the pipeline's coverage badge never appears even though tests genuinely ran.
