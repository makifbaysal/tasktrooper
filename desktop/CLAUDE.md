# desktop/ — working notes

The Electron desktop app (macOS, Windows, Linux), in one of two modes chosen at
runtime (`src/main/account/mode.ts`, persisted as `mode` in `settings.json`):

- **Local** (the default): it runs the whole product on this machine — the Go
  backend from `../server`, an embedded Postgres the backend starts, the
  bundled embedding engine, and Claude Code. Single user, no cloud, no sign-in.
- **Account**: the local backend and its Postgres are stopped; the window
  shows the account's web app from its origin (`https://app.tasktrooper.ai` by
  default) in a partition of its own, and the runner (`runner/`) plus the
  executor it starts are what run here, with the embedder beside them. Sign-in
  happens on that web page with its cookie; this process holds no session.
  The member's API keys are entered in this app's own API-key window
  (`src/main/keys/`), never in that page. Its only ways in are the account's
  Settings → LLM Connection and MCP Servers pages (`account.openKeys`); no
  tray or menu item, and local mode keeps keys on its own LLM Connection page.

## Invariants

- **In local mode the window waits for the backend.** `Shell.create()` makes the
  chrome only. The `WebContentsView` that renders the SPA is attached by
  `Shell.serve()`, called from `main/index.ts` when the supervisor emits
  `server`. The page reads `apiBase`/`apiToken` synchronously in its preload,
  so a view attached earlier is a page pointed at nothing, permanently. In
  account mode there is nothing local to wait for: `serve("/home")` runs at
  launch, `serve("/login")` on sign-in.
- **A mode switch replaces the view, it never navigates it.** `Shell.retire()`
  closes the old page first; the next `serve()` builds a view in the new
  mode's partition (`persist:account:<origin>`, or the default session for
  local) on the new mode's origin. Entering account mode: notifications and
  the local backend stop (`supervisor.disconnect()`; the embedder stays), the
  mode is persisted, `${origin}/login` loads. Leaving it: the account's view
  goes first, then the runner and executor stop and the pairing is forgotten,
  the mode is persisted, the partition is cleared, the backend starts, and
  `app://tasktrooper` loads on `server`. Local data is never touched. The
  order is `mode.test.ts`'s to assert.
- **"Use without an account for now" is not a sign-out.** When the account's
  page cannot load, the chrome's failure screen offers Retry and that
  (`shell:account:use-local-for-now`, guarded on the chrome as the sender):
  the account's view goes, the runner stops WITH its pairing kept, and the
  local backend starts. The persisted mode stays `account`, the partition is
  not cleared, and nothing is written — the next launch tries the account
  again. While it lasts `ModeController.mode` reads `local` (so the local
  page is the trusted one) and `account.state().temporaryLocal` is true; the
  local UI shows a banner and the tray a line, both with "Back to …"
  (`signIn()`, which reopens `${origin}/home` and starts the runner). Only an
  explicit Sign out unpairs.
- **One trusted origin at a time.** The sender guard (`sender-guard.ts`) and the
  navigation policy (`window.ts`) trust `app://tasktrooper` in local mode and
  the account's origin in account mode, never both. Off-origin navigation goes
  to the real browser; popups are denied. https only — plain http to loopback
  for the account origin only when not packaged (`TASKTROOPER_ACCOUNT_ORIGIN`
  is the dev override).
- **The account's page gets no local secret.** In account mode `apiBase` and
  `apiToken` answer "", `overridesSet` is refused (an override names a program
  the runner execs), and a pairing bundle is refused unless its `tm_base_url`
  is the account's origin (`ModeController.checkPairing`).
- **`PORT=0`, so the base URL is not stable.** The backend prints
  `LISTENING http://127.0.0.1:<port>`; a restart binds a different port. The
  `server` event carries the new one and `index.ts` reloads the view. Do not
  cache a base anywhere else.
- **A Disconnect stops the backend only; the embedder only on quit.** The
  embedder survives a Disconnect so a restart does not pay for a model
  reload. `drain()` is the only thing that stops it. It runs in both modes
  (`ACCOUNT_MODE_RUNS_EMBEDDER` in `account/mode.ts`): the process starts at
  launch, the model loads on the first request and unloads after ten idle
  minutes. In account mode its URL is the runner's `embeddings_base_url`
  (with `embedding_model` pinned, `runner/env.ts`), which the runner hands to
  the executor; a restart's new port reaches a running runner as an
  `embeddings-base-url` control line (`Supervisor`'s `embedder` event).
- **This app runs no Appium hub.** When Appium is installed the backend gets
  `APPIUM_BIN` + `MOBILE_APPIUM_HUB_URL` and starts the hub itself when a
  mobile tool needs it, stops it after 10 idle minutes and on its own shutdown
  (`server/internal/adapter/local/appiumhub`). The preflight still detects
  Appium and its drivers; do not bring back an appium child or a "does anything
  use mobile" poll — only the backend sees the calls that need the hub.
- **`mcp_secrets_key` must never be regenerated** on an install that has stored
  provider credentials — they are encrypted with it.
- **`src/shared/` may not import Electron or Node.** Enforced by eslint.
- **No secret crosses the bridge except `apiToken`**, and that is the bearer for
  a loopback server this process started — local mode only. Account mode's
  session lives in the web page's own cookie on its own origin; this process
  holds none, and no channel returns a credential: the pairing bundle goes in
  (`runner.pair`) and comes back without its token (`runner.pairing`). Do not
  add one.
- **API keys are typed into this app's own window, and only there.** The
  API-key window (`src/main/keys/`) is `src/renderer/keys.html` in a
  BrowserWindow of its own: in-memory session, no navigation, no popups, its
  own preload with `shell:keys:list|set|remove` (and the three `mcp-*` ones below) and nothing else. Those
  answer only that window's top frame on that page (`sender-guard.ts#isOwnPage`)
  — never the web app's view, on either mode's origin. `set` takes a key in;
  every answer is `{id, type, base_url?, models?, hasKey}`, every log line an
  id. The bridge has `account.openKeys(prefill?)` for it: a provider's or an
  MCP server's name to focus on, never a key or a value, and no answer. A
  change restarts a running runner (stop + connect, the pairing kept), which
  is what hands the new list to the executor. Ids: a built-in type's id is
  the type (`anthropic`, `openai`, `gemini`, `groq`, `local`); a custom
  OpenAI-compatible endpoint's is a UUID.
  **The window's second section is the member's own MCP servers**
  (`shell:keys:mcp-list|mcp-set|mcp-remove`, the same sender guard): a stdio
  `{name, command, args, env}` or an http `{name, url, headers}`, stored in
  `mcp-servers.bin` (`config/mcp-servers.ts`, `safeStorage`, 0600). Every answer
  is `{name, transport, command?, args?, url?, secretNames, hasSecret}` — a
  name and a flag, never an env or header value, and an address is shown
  without its query. In `set`, an empty `env` or `headers` value keeps the
  stored one under that name and a name left out is dropped. The list rides the
  runner's stdin as `mcp_servers` and from there the executor's, which serves
  the tools as `mcp_<name>_<tool>` to runs whose cloud tool policy names the
  server; a change restarts the runner like a key does. A stored
  `disabled: true` keeps a server here but out of the runner's list.
  **No cloud keeps MCP servers: this app adds the defaults itself**
  (`config/mcp-defaults.ts`, the backend's templates less `postgres`, no
  secret or placeholder, only `browser` switched on). `SeededMcpServerStore`
  adds them on the first read of the list in a process — the runner's spawn,
  the key window or the account's page — and records the version in
  `mcp-defaults.json`; a computer gets only defaults newer than its version,
  so one the member removed stays removed, and a list the key store cannot
  read is never written over.
- **The account's MCP Servers page manages the same list, without a secret.**
  `account.mcp.list|set|remove` (`cloud:account:mcp-*`, bridge 6, account
  mode only, the usual trusted-origin guard) answers the key window's
  summaries and takes the key window's `set`, plus `disabled`, with one
  difference: a variable or header the page's request does not name is kept,
  not dropped (`keepUnsent`; the page only knows the names from its last
  read). A server is a program the runner starts as this user, so a `set` that
  would run something new — a new server, another command, arguments or
  address, or a new variable or header value (`keys.ts#mcpServerConsent`) —
  waits for the person's yes in a native dialog (`keys/consent.ts`, Cancel the
  default and the escape), exactly as the toolchain's installs do; the page
  gets `declined` or `busy`. The dialog shows the command line with each
  argument quoted, a changed address in full and every new value in full,
  with control and bidi characters written out, and names the saved values
  that would go with a changed command or address. Keeping, dropping or
  switching what is stored asks nothing. The page never sends a secret: it
  opens the key window with `{mcp, secretNames}` for the values. Listed
  arguments have a URL's password masked (`redactArg`); an argument sent back
  masked keeps the stored one.
- **Secrets for the runner go on its stdin, nowhere else.** The pairing token
  (`pairing.bin`), the member's provider keys (`providers.bin`) and their own
  MCP servers' env and headers (`mcp-servers.bin`), all
  `safeStorage`-encrypted at 0600, are written into the runner's one-line
  config (`runner/env.ts`), which hands the keys to the executor on ITS stdin.
  Never argv, never an environment block, never the bridge, never the cloud.
- **Claude Code is not required in account mode.** A member may work with
  their own API keys alone: the account preflight requires `git`, and its
  `api-keys` row (`detect.ts#agentAccessItem`) is required only when there is
  no other way to run an agent — no stored key with the executor, no Claude
  Code signed in to a plan that has it, no OpenCode, no Cursor. `claude_bin` is
  sent only when detected; the runner answers `claude.run` with `not_ready`
  without it.

## Toolchain setup

Detecting and installing the developer tools agents use on this computer
(Git, Node.js, the JDK and Android SDK, Appium and its drivers, Docker, the
.NET SDK, Rust, …), in the background, for onboarding, the header's progress
chip and Settings (`src/main/toolchain/`, bridge `toolchain.*`, both modes).

- **The page sends ids, never commands.** `TOOLCHAIN_IDS` is the whole
  vocabulary; every formula, winget id, package list, script URL and pinned
  download lives in `toolchain/registry.ts`, per OS. An OS missing from a
  tool's `recipes` is one it does not apply to; a recipe whose channel is not
  on the machine (no winget, no pkexec, an arch with no build) degrades to
  `manual` and links its page. `openPage` opens only the table's own https
  (or `macappstore:`) URL.
- **Account mode asks first, natively.** `install` from the account's remote
  page plans (leaving out what the running job already has), shows "Install
  developer tools?" (`dialog.showMessageBox` on the shell window, labels +
  methods, Cancel the default) and enqueues exactly that list — no second
  plan. Cancel rejects with `declined`; a second install while the dialog is
  open rejects with `busy`. Local mode's page is our own bundle and is not
  asked twice (`main/ipc.ts`). The remote page also gets no paths: report
  items lose `path`, and the home directory reads `~` in every detail, error
  and log line (`toolchain/remote.ts`).
- **Detection decides, not exit codes.** One job at a time, steps in order
  (brew/winget/apt hold locks). A step already installed is `skipped`; one
  whose dependency did not install is `skipped` naming it; an install that
  exited 0 and is still not found is `failed` ("Installed, but not found
  afterwards"). A step that ended without installing can be queued again in
  the same job (a new step at the end). Detection takes the first copy of a
  tool that answers, so a version manager's shim with no version selected
  does not hide a working one.
- **How things are installed.** Strategies per method in
  `toolchain/recipes/`; all spawn with `shell: false` in their own process
  group, nothing secret on argv. Root only through the OS's own prompt:
  `pkexec apt-get|dnf|snap …` (so polkit names the package manager),
  `SUDO_ASKPASS` for Homebrew casks only (a native dialog naming the cask;
  Cancel stops sudo), UAC for winget. Never a downloaded package file
  installed as root (Linux Chrome is the Chromium snap). Downloads go through
  `net.fetch` (the system proxy), give up after 60 s without bytes, and are
  pinned by digest where the vendor publishes one (Android command-line
  tools, OSV-Scanner, uv and DuckDB on Linux). The rest are TLS-trusted
  vendor installers from fixed https URLs — Claude Code's
  `install.sh`/`install.ps1`, rustup's `sh.rustup.rs` (`-y`), Microsoft's
  `dotnet-install.sh` on Linux and Homebrew's `install.sh` — always saved to
  a file and then run with the table's fixed flags (Homebrew's in a Terminal
  window), never piped.
- **Installed means reachable.** The backend (`supervisor/env.ts`) and the
  runner with its executor (`runner/env.ts`) get `toolchain.runtime()`
  appended to PATH — the directories of the tools the last report found plus
  the install locations that exist (`toolchain/locations.ts`: a keg-only
  JDK, `%LOCALAPPDATA%\Programs\osv-scanner`, `~/.duckdb/cli/latest`,
  `~/.cargo/bin`, `~/.dotnet/tools`, …) — and JAVA_HOME when none is set, and
  on Linux DOTNET_ROOT for an SDK in `~/.dotnet` when none is set.
- **State** is `userData/toolchain-state.json` (atomic, pushed to the page at
  most 4/s on `cloud:toolchain:state-changed`). Cancel kills the running
  step's tree; one running as administrator cannot be killed, so after a
  grace period the step reads "Still finishing in the background" and the
  job moves on. Quit kills the running step (`QuitSteps.stopToolchain`,
  before the drain) and waits for nothing; a job found `running` at launch
  becomes `done` with its unfinished steps failed as "Interrupted when the
  app quit." When a job ends, a fresh report and then `runPreflight(true)`
  run so the setup screen, the caches and the runner see the new tools.

## Anonymous usage statistics

The main process sends GA4 Measurement Protocol events so the owner can count
active installs, local-mode ones included (`src/main/analytics/`). No SDK, no
renderer involvement.

- **Sent:** `POST https://www.google-analytics.com/mp/collect` with a random
  client id and `app_open` (once per launch) and `app_active` (a heartbeat at
  most every 30 minutes while a window is focused, with `engagement_time_msec`),
  batched. Params: `session_id`, `app_version`, `platform`, `arch`, `mode`
  (`local` | `account` | `temporary_local`). Nothing else: no origin, email,
  path or task content. Network errors are swallowed; the one logged line has the
  secret scrubbed.
- **Config is build-time only.** `TT_GA_MEASUREMENT_ID` and `TT_GA_API_SECRET`
  are inlined by `scripts/build-main.mjs` (esbuild `define`); the release
  workflow takes them from the `GA_MEASUREMENT_ID` and `GA_API_SECRET` secrets.
  Either empty (dev builds, forks, tests, a hand-started workflow run) means no
  network and no id file; the switch is then not shown. Neither value is read from the
  environment or a file at run time. The inlined secret is visible to anyone who
  unpacks the app, which is all a Measurement Protocol secret protects.
- **Consent.** `analytics` in `settings.json`; `ANALYTICS_DEFAULT_ON`
  (`analytics/config.ts`) is the one place the default lives.
  `TASKTROOPER_TELEMETRY=0` or `DO_NOT_TRACK=1` forces it off. Turning it off
  stops sending and deletes `analytics-id` (a UUID, 0600, in userData, created
  only while enabled).
- **Where the switch is.** Settings → General only, in both modes: the local
  bundle's, or in account mode the account's own page, through
  `analytics.get()/set()` (`cloud:analytics:*`, the usual trusted-origin
  guard); plus a line on the first-run screen. The tray carries no switch.

## Where things are

| Concern | File |
|---|---|
| Start/stop/restart, readiness, state machine | `src/main/supervisor/supervisor.ts` |
| One child process, backoff, SIGTERM → SIGKILL (`TERM_GRACE_MS` 30s) | `src/main/supervisor/child.ts` |
| The backend's environment | `src/main/supervisor/env.ts` |
| `LISTENING` + zerolog parsing | `src/main/supervisor/server-log.ts` |
| `/health` polling | `src/main/services/health.ts` |
| Machine probing, `binDir()`, `dataDir()`, `postgresCacheDir()` | `src/main/services/detect.ts` |
| One shared sweep at a time; login PATH + `--version` cache in `preflight-cache.json` | `src/main/services/preflight-sweep.ts`, `preflight-cache.ts` |
| `app://tasktrooper`, `webRoot()` | `src/main/services/app-scheme.ts` |
| Generated secrets in `local.bin` | `src/main/config/secrets.ts` |
| Local ↔ account switch, pairing origin check | `src/main/account/mode.ts` |
| Account origin rules, default, partition name | `src/main/account/origin.ts` |
| Anonymous active-install count (GA4 Measurement Protocol) | `src/main/analytics/` |
| Toolchain setup: the install table, detectors, recipe strategies, the job | `src/main/toolchain/` |
| Who may call the cloud bridge | `src/main/sender-guard.ts` |
| Account mode's supervisor, the runner child, its stdin config, its log | `src/main/runner/` |
| Runner pairing (`pairing.bin`), provider keys (`providers.bin`), MCP servers (`mcp-servers.bin`) | `src/main/config/pairing.ts`, `providers.ts`, `mcp-servers.ts` |
| The API-key window, its rules, its page | `src/main/keys/`, `src/preload/keys.ts`, `src/renderer/keys/` |
| The Go runner and its executor supervision | `runner/` (its own `CLAUDE.md`) |
| Channels, host contract, validators | `src/ipc/` |
| Wiring, boot order, sender guards | `src/main/index.ts` |

## The bridge contract

`src/ipc/host.ts` is one half; `ui/src/lib/desktop-bridge.ts` is the other. They
are separate declarations and must be changed together.

```ts
window.__tasktrooperDesktop = {
  info(), apiBase?, apiToken?, bridgeVersion: 6, mode: "local" | "account",
  account: { signIn(origin?), signOut(), state(), openKeys(prefill?), mcp: { list(), set(server), remove(name) } },
  runner: { snapshot, subscribe, connect, ..., pair, unpair, pairing, restart },
  updates: { status, subscribe, check, restart },
  analytics: { get(), set(on) },
  toolchain: { report(force?), plan(ids), state(), subscribe, install(ids), cancel(), dismiss(), openPage(id) }
}
```

`ChildId` is `"embedder" | "agent-server" | "runner"` (`CHILD_IDS` keeps the
local two). `HostRunnerSnapshot.tunnel` is set in account mode only.
`PreflightId` is the seventeen ids in `src/ipc/types.ts` (`runner`,
`executor` and `api-keys` are account mode's); the SPA's own union must match
exactly. `account.state()` answers
`{mode, origin, switching, paired, temporaryLocal, error?}`, a superset of
the page half's `{mode, origin?, temporaryLocal?}`. `bridgeVersion` 2 added
`account.openKeys()` and `temporaryLocal`, 3 `openKeys(prefill)`, 4 `analytics`,
5 `toolchain` (required here, optional `toolchain?` on the page's half), 6
`account.mcp` and `openKeys({mcp})` (optional `mcp?` on the page's half).
`ToolchainId` is the twenty-four ids in `TOOLCHAIN_IDS`, in the same order on
both halves.

## Boot order (`whenReady`)

Local mode as below. In account mode step 2 also calls `shellWindow.serve("/home")`,
step 7 is `runnerSupervisor.detect()` and step 8 starts the runner when this
computer is paired and `autoConnect` is on. The embedder (step 5) starts in
both. A launch never starts "locally for now": that lasts one run of the app.

1. `serveAppScheme()` — before any window, or the first load is a blank frame.
2. `registerIpc`, then `shellWindow.create()` — the chrome and its starting
   screen, before anything slow (the keychain, child processes).
3. `supervisor.warmUp()` — the last launch's login PATH is used at once; this
   launch's login shell runs in the background.
4. `secretStore.ensure()` (via `loadSecrets()`) — generates `local.bin` on
   first run.
5. `supervisor.reapStale()` and `supervisor.startEmbedder()` — not awaited.
6. tray, updater (electron-updater is loaded only when the build has a feed),
   supervisor event wiring.
7. `supervisor.detect()` — so the setup screen has answers.
8. `startBackend()` when `autoConnect` (default true) — it joins the sweep
   step 7 started and spawns once that sweep's gating half and the reaper are
   both done; otherwise the chrome shows its offline screen with the reason.

## Gotchas

- `build:server` needs **`CGO_ENABLED=1`**: the backend links tree-sitter, whose
  grammars are C. With cgo off every grammar fails with "build constraints
  exclude all Go files", which reads like a toolchain problem and is not one.
  Cross-compiling darwin/amd64 from arm64 still works — clang takes `-arch`
  from Go.
- In local mode the window loads `app://tasktrooper` from `ui/dist`, never a
  Vite dev server: the bridge's sender checks are written against that origin.
  In account mode it loads the account's origin; point a dev build at a local
  control plane with `TASKTROOPER_ACCOUNT_ORIGIN=http://127.0.0.1:<port>`.
- `build:runner` is pure Go (`CGO_ENABLED=0`); `build:executor` builds
  `../server/cmd/executor` with cgo, like `build:server`. `npm run package`
  builds and signs all three.
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
