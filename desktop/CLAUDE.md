# desktop/ — working notes

The macOS Electron app. It runs the whole product on this machine: the Go
backend from `../server`, an embedded Postgres the backend starts, the bundled
embedding engine, and Claude Code. Single user, single machine, no cloud, no
sign-in.

## Invariants

- **The window waits for the backend.** `Shell.create()` makes the chrome only.
  The `WebContentsView` that renders the SPA is attached by `Shell.serve()`,
  called from `main/index.ts` when the supervisor emits `server`. The page reads
  `apiBase`/`apiToken` synchronously in its preload, so a view attached earlier
  is a page pointed at nothing, permanently.
- **`PORT=0`, so the base URL is not stable.** The backend prints
  `LISTENING http://127.0.0.1:<port>`; a restart binds a different port. The
  `server` event carries the new one and `index.ts` reloads the view. Do not
  cache a base anywhere else.
- **Stop order is agent-server, then appium. The embedder only on quit.** The
  backend holds the Claude Code sessions that may still be calling the hub, and
  the embedder survives a Disconnect so a restart does not pay for a model
  reload. `drain()` is the only thing that stops it.
- **`mcp_secrets_key` must never be regenerated** on an install that has stored
  provider credentials — they are encrypted with it.
- **`src/shared/` may not import Electron or Node.** Enforced by eslint.
- **No secret crosses the bridge except `apiToken`**, and that is the bearer for
  a loopback server this process started. There is no session, no OAuth and no
  credential-returning channel; do not add one.

## Where things are

| Concern | File |
|---|---|
| Start/stop/restart, readiness, state machine | `src/main/supervisor/supervisor.ts` |
| One child process, backoff, SIGTERM → SIGKILL (`TERM_GRACE_MS` 30s) | `src/main/supervisor/child.ts` |
| The backend's environment | `src/main/supervisor/env.ts` |
| `LISTENING` + zerolog parsing | `src/main/supervisor/server-log.ts` |
| `/health` polling | `src/main/services/health.ts` |
| Machine probing, `binDir()`, `dataDir()`, `postgresCacheDir()` | `src/main/services/detect.ts` |
| `app://tasktrooper`, `webRoot()` | `src/main/services/app-scheme.ts` |
| Generated secrets in `local.bin` | `src/main/config/secrets.ts` |
| Channels, host contract, validators | `src/ipc/` |
| Wiring, boot order, sender guards | `src/main/index.ts` |

## The bridge contract

`src/ipc/host.ts` is one half; `ui/src/lib/desktop-bridge.ts` is the other. They
are separate declarations and must be changed together.

```ts
window.__tasktrooperDesktop = {
  info(), apiBase?, apiToken?, runner: { snapshot, subscribe, connect, ... }
}
```

`ChildId` is `"embedder" | "agent-server" | "appium"`. `HostRunnerSnapshot` has
no tunnel field. `PreflightId` is the fourteen ids in `src/ipc/types.ts`; the
SPA's own union must match exactly.

## Boot order (`whenReady`)

1. `serveAppScheme()` — before any window, or the first load is a blank frame.
2. `secretStore.ensure()` — generates `local.bin` on first run.
3. `supervisor.startEmbedder()` — not awaited.
4. updater, tray, supervisor event wiring, `registerIpc`.
5. `shellWindow.create()` — the chrome and its starting screen.
6. `supervisor.detect()` — so the setup screen has answers.
7. `startBackend()` when `autoConnect` (default true); otherwise the chrome
   shows its offline screen with the reason.

## Gotchas

- `build:server` needs **`CGO_ENABLED=1`**: the backend links tree-sitter, whose
  grammars are C. With cgo off every grammar fails with "build constraints
  exclude all Go files", which reads like a toolchain problem and is not one.
  Cross-compiling darwin/amd64 from arm64 still works — clang takes `-arch`
  from Go.
- The window loads `app://tasktrooper` from `ui/dist`, never a Vite dev server:
  the bridge's sender checks are written against that origin.
- Everything under `bin/`, `dist/`, `ui/dist/` is build output. `ui/` has its
  own package and its own lint config; this package's eslint ignores it.
- `webRoot()`'s packaged path (`Resources/web`) must agree with
  `electron-builder.yml`'s `to: web`.
- Tests are vitest, Node environment, Electron mocked per suite. `detect.test.ts`
  spawns real fake CLIs rather than stubbing `execFile`.
- `npm run dev` opens and immediately closes on a machine that already has
  TaskTrooper.app running (the common case — it's this project's own daily
  driver): both builds are named "TaskTrooper", so Electron's single-instance
  lock treats the dev launch as a second instance and just focuses the running
  app. `npm run dev:isolated` points Electron at its own `--user-data-dir` (via
  `TASKTROOPER_DEV_USER_DATA_DIR`) so the two never collide — use it for any
  local check (including UAT) while the packaged app might be open.

## Verify

```
npm run typecheck && npm run lint && npm test && npm run build
npm run build:server && npm run build:embedder && npm run build:ui
npx electron .
```

A good launch logs, in order: `EMBEDDER_LISTENING <port>`, `embedded postgres
started`, `LISTENING http://127.0.0.1:<port>`, then the window appears. `curl
http://127.0.0.1:<port>/health` answers 200, and the same path with no
`Authorization` header on a `/v1` route answers 401.
