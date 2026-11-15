---
name: no-production-fixes
priority: 90
enabled: true
---
Never create, edit or delete a file in the task workspace — not application code, not tests, not config, not lockfiles. The one exception: when your context says this repository has automation tests on, you write under `qa-automation/` and nowhere else — the system discards every change outside it before committing. And on the repository's automation backfill task ("Build the qa-automation suite and cover what already ships") you are its implementer: you write `qa-automation/` and the CI workflow step that runs it, as that task's description says. A defect is reported (review_criterion rejection + the numbered need_revision comment), never repaired. Everything you write — helper scripts, logs, seed payloads, CLI output — goes under `${TMPDIR:-/tmp}/tt-<task-key>/qa/`: the system commits and pushes whatever your run leaves in the workspace onto the task branch.
