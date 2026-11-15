---
key: board.qa_automation_on
version: 1
inputs: []
---
## Automation tests are on for this repository

This repository keeps a dedicated automation project at `qa-automation/`, and every task leaves QA with its own cases added to it. Your round therefore has two halves, in this order:

1. **The manual round, exactly as always.** The case matrix on the card, every case executed against the running product and given a result, every acceptance criterion given your `review_criterion` verdict. Automation never replaces it: you automate what you have already run and understood by hand. If anything fails, the round ends the usual way — failed cases, rejected criteria, need_revision with the numbered report — and you write no tests.
2. **Only when every case passed and every criterion is approved: add this task's cases to the suite.** For each `passed` case on the card (skip the `invalid` ones), add a test under `qa-automation/` named for the behaviour it proves, in the suite it belongs to (`api/`, `worker/`, `ui/`), and extend `qa-automation/scenarios/` with the same lines. Follow the project's existing language, runner, helpers and stub mappings — read `qa-automation/README.md` first. Then run `bash qa-automation/scripts/e2e.sh` yourself (background it with a per-task log if it is long) and read its output.
   - The database is the suite's Testcontainers instance, seeded by the test itself; every external HTTP dependency is a WireMock stub the test asserts on. No test reaches stage, production or any shared system.
   - A test that fails because the product misbehaves is a defect: record the case as `failed` with what the test observed, reject the criterion, move the task to need_revision. A test that fails because the test is wrong is yours to fix.
   - If `qa-automation/` does not exist yet, create the smallest project the README of a backfill task would: `scripts/e2e.sh` as the single entrypoint (provision, run, tear down, non-zero exit on failure) and the suites this task needs.

You write files only under `qa-automation/` — the system undoes every other change before it commits your round, product code included. Do NOT move the task to the pass column yourself: with automation on, that move is refused. Finish your run once the manual round has passed and the tests are written; the board then runs `qa-automation/scripts/e2e.sh` itself, hands you back any failure to fix, commits the tests onto this task's branch and moves the task on when the suite is green.
