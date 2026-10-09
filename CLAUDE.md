# TaskTrooper — monorepo

Three parts, one product: the desktop app (macOS, Windows, Linux). Read the directory's own
`CLAUDE.md` before working in it.

| dir | what | verify |
|---|---|---|
| `server/` | Go backend (hexagonal), embedded Postgres, agent runtime | `go build ./... && go vet ./... && go test ./...` |
| `desktop/` | Electron shell: supervises the backend + embedder and serves the UI from `app://tasktrooper`; in account mode, the runner and the account's web app instead | `npm run typecheck && npm run lint && npm test` |
| `desktop/runner/` | Go runner for account mode (its own module): the tunnel, CLI runs, the executor | `go build ./... && go vet ./... && go test ./...` |
| `desktop/ui/` | React UI, bundled into the app | `npx tsc --noEmit && npm run build && npm run check:locales` |

`make test` runs all of them.

## Shape

- **Local by default.** One user, no login. The UI authenticates to the backend
  with one bearer token: the desktop generates it and passes it to the server
  as `SERVER_API_KEY` and to the page as `window.__tasktrooperDesktop.apiToken`;
  `make dev` uses `VITE_API_KEY`.
- **Account mode is the desktop shell's, and optional.** Signing in stops the
  local backend and shows the account's own web app from its origin; the shell
  then runs `desktop/runner`, which tunnels to the control plane and starts
  `server/cmd/executor` with the member's own keys. Leaving it brings local
  mode back unchanged. See `desktop/CLAUDE.md`.
- **The desktop app is the backend's supervisor.** It spawns `bin/agent-server`
  with `PORT=0`, reads the `LISTENING http://127.0.0.1:<port>` line, polls
  `/health`, then opens the window. The server starts its own Postgres from
  the zonky binaries under the data directory when `DATABASE_URL` is empty.
- **Two contracts cross directories and move together:**
  `desktop/src/ipc/host.ts` ↔ `desktop/ui/src/lib/desktop-bridge.ts` (the
  bridge), and the server's env/stdout contract in `server/README.md` ↔
  `desktop/src/main/supervisor/`.

## Rules

- No code comments that explain WHAT. Only a non-obvious invariant, a
  workaround, or a WHY.
- `domain`/`application` never import `adapter` in `server/`.
- The local edition stays single-user and cloud-free: no Firebase and no
  team/invite/billing-plan UI in `desktop/ui`; no tenants, no control plane and
  no `X-Internal-*` headers in `server/`. Outside it, and only there: the
  desktop shell's optional account mode (the account's web app on its own
  origin, `desktop/runner` and its tunnel), and `server/cmd/executor` taking a
  per-run coordination MCP URL and token from its parent. None of that reaches
  a local-mode code path.
- Secrets never go on argv; children are `spawn`ed with `shell: false`.
- No LLM-facing prose in Go: prompts, guard wording and tool descriptions live
  in `catalog/system` (see `catalog/system/README.md`); code passes data.
