---
key: repository.automation_backfill_brief
version: 1
inputs: [Kind]
---
This repository just switched automation tests on (kind: {{.Kind}}). From now on every task leaving QA adds its own cases to a dedicated automation project here and must leave that suite green. This task builds the project and writes the tests for what the repository already does, so later tasks extend a suite instead of starting one.

**Where.** One project at the repository root, `qa-automation/`, separate from the product's unit tests, with its own dependencies. If one already exists (here or under another name the README points to), extend it — never fork a second.

```
qa-automation/
  scenarios/        one human-readable scenario list per feature
  api/              HTTP suite against the running product
  worker/           async jobs: trigger through the product path, poll, assert side effects
  ui/               Playwright against the built app (headless Chromium from CHROME_BIN, no browser download)
  fixtures/         seed data
  support/          helpers, request builders, page objects, WireMock mappings
  scripts/e2e.sh    the single entrypoint: provision, run every suite, tear down
```

Use the language and test runner the product already uses (Go: testing + testcontainers-go; JVM: JUnit + Testcontainers + RestAssured; Node/TS: Vitest or Jest + testcontainers + supertest; Python: pytest + testcontainers-python; .NET: xUnit + Testcontainers for .NET). Mobile automation is out of scope.

**Determinism.**
- The product under test boots from this branch inside the run; the suite never targets stage, production or any shared environment.
- Databases, queues and caches are Testcontainers instances, migrated with the product's own migrations and seeded per run; each test seeds and cleans its own rows and runs in any order.
- Every external HTTP dependency (payment, auth, partner APIs, mail) is a WireMock container (or the language's WireMock port) the product is pointed at through its own configuration; tests assert on the requests the stub received, and failure modes (timeouts, 5xx, malformed bodies) are stubbed on purpose.
- Docker is the only thing the entrypoint may assume. If Docker is not available on this machine, say so in one `ask_user` question instead of faking containers.

**What to cover.** Work out what the repository does from its README, routes/handlers, screens, workers and existing docs — this is the one task where you read the code to find the features, not to decide verdicts. For each feature write the scenario list first, then one test per scenario: the happy path, the main negative case (invalid input, missing auth, not found), and the boundaries that matter. A scenario with no test, or a test with no scenario, is a gap.

**Done means.** `bash qa-automation/scripts/e2e.sh` is green from a clean checkout, the repository's CI workflow runs the same entrypoint, and a short `qa-automation/README.md` says how to run one suite and how to add a test for a new task.
