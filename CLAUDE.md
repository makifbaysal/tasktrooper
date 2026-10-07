# TaskTrooper — monorepo

Three parts, one product: the desktop app (macOS, Windows, Linux). Read the directory's own
`CLAUDE.md` before working in it.

| dir | what | verify |
|---|---|---|
| `server/` | Go backend (hexagonal), embedded Postgres, agent runtime | `go build ./... && go vet ./... && go test ./...` |
| `desktop/` | Electron shell: supervises the backend + embedder, serves the UI from `app://tasktrooper` | `npm run typecheck && npm run lint && npm test` |
| `desktop/ui/` | React UI, bundled into the app | `npx tsc --noEmit && npm run build && npm run check:locales` |

`make test` runs all three.

## Shape

- **Local only.** One user, no login. The UI authenticates to the backend with
  one bearer token: the desktop generates it and passes it to the server as
  `SERVER_API_KEY` and to the page as `window.__tasktrooperDesktop.apiToken`;
  `make dev` uses `VITE_API_KEY`.
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
- Nothing multi-tenant, no cloud, no control plane comes back: no Firebase, no
  tunnel, no `X-Internal-*` headers, no team/invite/billing-plan UI.
- Secrets never go on argv; children are `spawn`ed with `shell: false`.
- No LLM-facing prose in Go: prompts, guard wording and tool descriptions live
  in `catalog/system` (see `catalog/system/README.md`); code passes data.
