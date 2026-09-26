# TaskTrooper Desktop

The macOS app. It starts the backend, the database and the embedding engine on
this machine, serves the web UI from inside the bundle, and runs the agent CLI sessions
(Claude Code, Cursor, Antigravity or OpenCode) here. One user, one machine, no cloud.

## What runs when you open it

| # | Process | Started by | Gates the window? |
|---|---|---|---|
| 1 | `embedder` | app init, always | no |
| 2 | `agent-server` (from `../server`) | the supervisor | **yes** — the UI appears when `/health` answers 200 |
| 3 | `appium` | beside the backend, if installed and nothing is on `127.0.0.1:4723` | no |

The backend brings up its own Postgres. The first launch downloads ~30 MB of
Postgres binaries into `~/Library/Application Support/TaskTrooper/postgres-bin`;
the window narrates that rather than spinning silently.

The window itself is a title bar plus a `WebContentsView` that loads
`app://tasktrooper` — the SPA in `ui/`, served out of the bundle by
`src/main/services/app-scheme.ts`. It is created only after the backend answers,
because the page reads its API base synchronously from the preload.

## Layout

| Path | What |
|---|---|
| `src/main/` | supervisor, config, detection, updater, window, tray |
| `src/preload/` | the two bridges: the chrome's, and the SPA's |
| `src/ipc/` | channels, payload types, runtime validators |
| `src/renderer/` | the title bar, the starting screen and the failure screen |
| `src/shared/` | small UI pieces the renderer uses. No Electron, no Node |
| `embedder/` | the bundled ONNX embedding server (its own esbuild bundle) |
| `ui/` | the SPA. Built by `npm run build:ui`, staged as `Resources/web` |
| `bin/` | `agent-server`, built from `../server`. Not in git |
| `scripts/` | the build scripts |

## Commands

| Command | What it does |
|---|---|
| `npm ci` | install |
| `npm run dev` | builds the backend, the embedder and (if missing) `ui/dist`, then runs Electron with the chrome on Vite |
| `npm run build` | typecheck, then bundle the chrome and the main process |
| `npm run build:server` | `go build` in `../server` into `bin/agent-server` |
| `npm run build:server:universal` | arm64 + amd64, joined with `lipo` |
| `npm run build:ui` | `npm --prefix ui run build` |
| `npm run build:embedder` | the embedder bundle into `dist/embedder` |
| `npm run typecheck` / `lint` / `test` | three tsconfigs, eslint 9, vitest |
| `npm run package` | all of the above plus `electron-builder --mac` |

Iterating on the SPA: `npm --prefix ui run build -- --watch` in a second
terminal, then Reload in the title bar. The window does not load a Vite dev
server — the bridge's powers are attached to the `app://tasktrooper` origin, and
`http://localhost:5173` is a different one.

## Secrets and data

Generated on first run, encrypted with `safeStorage`, stored in
`userData/local.bin`:

| Field | Used as |
|---|---|
| `api_token` | the backend's `SERVER_API_KEY`, and the bearer the UI sends |
| `mcp_secrets_key` | the backend's `MCP_SECRETS_KEY`. Must stay stable, or stored provider credentials stop decrypting |

Everything else lives in `userData`: `settings.json` (workspace folder, launch at
login, auto-start), `data/` (the database, RAG files, session workspaces),
`postgres-bin/` (the downloaded Postgres).

There is no sign-in, no account and no env file. `apiToken` is a bearer for a
loopback server this same process started.

## What the backend is started with

Environment, never a config file. `PORT=0` — the backend picks a free port and
prints `LISTENING http://127.0.0.1:<port>`, which is how this process learns the
API base.

| Var | Value |
|---|---|
| `PORT` | `0` |
| `DATA_DIR` | `userData/data` |
| `EMBEDDED_POSTGRES_CACHE_DIR` | `userData/postgres-bin` |
| `SERVER_API_KEY` | `api_token` |
| `MCP_SECRETS_KEY` | `mcp_secrets_key` |
| `CLAUDE_CODE_BIN` | the `claude` detection found |
| `EMBEDDINGS_BASE_URL` | the embedder's loopback URL, when it bound one |
| `CHROME_BIN`, `MOBILE_APPIUM_HUB_URL` | only when found |

`DATABASE_URL` is deliberately never set: an empty DSN is what selects the
embedded Postgres.

## Preflight

`src/main/services/detect.ts` probes the machine at launch and before every
start. Required items refuse the start and the refusal names the item.

| Item | Required | Note |
|---|---|---|
| `agent-server` | yes | bundled; missing means a broken build |
| `postgres` | no | always `ok`; says the download is coming |
| `git` | yes | every task clones and commits |
| `claude`, `claude-account`, `cursor-agent`, `agy`, `opencode` | no | agent runtimes; any one of them, or an API provider, is enough. `claude-account` is separate because a free plan passes every "is it there" check and fails inside the first run |
| `chrome`, `xcode-clt`, `appium*`, `android-sdk` | no | capabilities; never block |

## Packaging and signing

`npm run package` produces a universal, hardened-runtime `.app`, ad-hoc signable
with `-c.mac.identity=-`. Signing material comes from the environment
(`CSC_LINK` / `CSC_KEY_PASSWORD`, and `APPLE_API_KEY` / `APPLE_API_KEY_ID` /
`APPLE_API_ISSUER` for notarization); with none of it set, electron-builder says
so and carries on. Never set `mac.identity: null` — that skips signing entirely
and produces "TaskTrooper is damaged and can't be opened".

`bin/agent-server` is listed under `mac.binaries` because the `.app`'s signature
does not cover it and notarization rejects an unsigned Mach-O.

## Updates

`publish: null`, so electron-builder cannot infer a feed from the git remote. A
build updates itself only if it was packaged with a `publish` configuration
passed on the command line — which is what `.github/workflows/release.yml`
does. `services/updater.ts` reads the bundled `app-update.yml` and accepts two
shapes: `provider: generic` with a trustworthy URL, and `provider: github` for
`makifbaysal/tasktrooper` and no other repository. A dev run and a plain
`npm run package` build report `unsupported`, which is the honest state.

The GitHub provider reads the releases feed unauthenticated, so it answers 404
for every install while this repository is private. That is expected rather than
broken: it is logged only under `TASKTROOPER_UPDATE_DEBUG` and never drawn as a
failed check.
