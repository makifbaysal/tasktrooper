---
key: repository.automation_backfill_criteria
version: 1
inputs: []
---
- `qa-automation/scripts/e2e.sh` provisions everything it needs, runs every suite and tears down, from a clean checkout, with one command and a non-zero exit on any failure
- Every user-facing feature the repository already ships has at least its happy path and its main negative case covered by a test that names the behaviour it proves
- The database is a disposable Testcontainers instance migrated and seeded per run, and every external HTTP dependency is a WireMock stub the tests assert on — no test reaches a shared, stage or production system
- The repository's own pipeline runs `qa-automation/scripts/e2e.sh`, so a red suite fails the pipeline
