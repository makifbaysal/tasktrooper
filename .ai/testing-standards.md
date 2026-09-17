# Testing standards

How each package is verified, and the invariants specific to its test setup.
See [coding-standards.md](coding-standards.md) for everything else.

| Package | Command | Notes |
|---|---|---|
| `server/` | `go build ./... && go vet ./... && go test ./...` | Tests needing Postgres start their own embedded instance (`platform/database.StartEmbedded`) — nothing external has to be running. One embedded cluster per test binary, a database per suite. `CGO_ENABLED=1` required (tree-sitter). |
| `desktop/` | `npm run typecheck && npm run lint && npm test` | Vitest, Node environment, Electron mocked per suite. `detect.test.ts` spawns real fake CLIs rather than stubbing `execFile`. |
| `desktop/ui/` | `npx tsc --noEmit && npm run build && npm run check:locales` | `npm test` (Vitest) also exists and should stay green; `check:locales` fails the build if `en.ts`/`tr.ts` key sets diverge. |

`make test` from the repo root runs all three.

## Rules

- A test that needs a real database is a `*_test.go` file using the embedded
  Postgres helper, guarded with `if testing.Short() { t.Skip(...) }` — see
  `server/internal/adapter/store/postgres/*_test.go` for the pattern (a
  `suite.Suite` with `SetupSuite`/`TearDownSuite` starting/stopping one
  embedded cluster).
- A store method whose correctness depends on a CTE's snapshot semantics (a
  claim-and-clear statement where a later part of the same query must still
  see the pre-update row) can only be verified against the real engine — a
  fake/mock store test would pass either way. Write that one test against
  Postgres and keep the rest at the Go level.
- No code comments explaining WHAT a test does — the test name and assertions
  already say that; a comment is for a non-obvious fixture choice or ordering
  requirement.
