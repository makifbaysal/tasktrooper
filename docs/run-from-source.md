---
title: Run from source
description: Building and running TaskTrooper from the monorepo — setup, the dev loop, packaging, tests and the release process.
---

TaskTrooper is a monorepo: a Go backend, an Electron shell, and a React UI
bundled into it. This page is for running it from a checkout instead of the
packaged app — see [Install](install.md) if you just want to use it.

## Prerequisites

| Tool | Why |
|---|---|
| Go (version pinned in `server/go.mod`) | The backend |
| A C compiler | The backend links `smacker/go-tree-sitter`, a cgo package — builds need `CGO_ENABLED=1`. On Windows this is what the release workflow installs MSYS2/gcc for |
| Node.js 22 and npm | The desktop shell and the UI |
| git | Every task clones and commits; the backend itself needs it on `PATH` too |

Nothing else is required to build. `rg` (ripgrep) on `PATH` is worth having
for backend development — the `grep_code` tool shells out to it with no
fallback — but it's not a build dependency.

## Layout

```
server/       Go backend — API, board, agent loop, tools, embedded Postgres
desktop/      Electron shell — supervises the backend + embedder, serves the UI
desktop/ui/   React UI — bundled into the app; runs in a browser for development
docs/         user docs (rendered at tasktrooper.ai/docs)
scripts/      dev.sh, install.sh, update-cask.sh
```

Each directory has its own `README.md` and `CLAUDE.md` with more detail than
this page covers.

## Setup

```sh
make setup      # go mod download (server) + npm ci (desktop, desktop/ui)
```

## The dev loop

Two ways to run it, depending on whether you want the Electron chrome:

```sh
make dev        # backend (embedded Postgres) + UI dev server, in your terminal
make desktop    # the Electron app itself, in dev mode
```

**`make dev`** runs `scripts/dev.sh`, which starts the Go backend with
`go run ./cmd/agent-server` and the UI's Vite dev server side by side, and
stops both on Ctrl-C. On first run it writes `server/.env.local` (gitignored)
with a generated `SERVER_API_KEY` and `MCP_SECRETS_KEY`, `PORT=8085` and a
`DATA_DIR` under `server/`; edit that file directly for anything you want to
change, it won't be regenerated once it exists. The UI dev server comes up at
**http://localhost:3200**, proxying `/v1`, `/admin` and `/health` to the
backend and passing it `VITE_API_KEY` (the same key the backend was started
with) so the page authenticates without you typing anything in.

**`make desktop`** builds the backend binary and the UI, then launches
Electron with the chrome pointed at Vite — this is the path that exercises
the supervisor, the tray, and the desktop bridge (`desktop/src/ipc/host.ts` ↔
`desktop/ui/src/lib/desktop-bridge.ts`), which the plain `make dev` loop
skips entirely.

Iterating on the UI inside the Electron shell: run `npm --prefix desktop/ui
run build -- --watch` in a second terminal and reload from the title bar —
the window loads the built `app://tasktrooper` bundle, never a Vite dev
server.

## Packaging

```sh
make package    # build the installer for this OS into desktop/release
```

Dispatches to `electron-builder` for the current OS (`package:win` on
Windows, `package:linux` on Linux, the macOS build otherwise). A local build
is ad-hoc signed (`-c.mac.identity=-` on macOS) unless signing environment
variables are set — see `desktop/README.md` for what those are.

## Tests and verification

```sh
make test       # all three suites
make build      # compile everything without running
make lint        # vet + typecheck only, no tests
```

`make test` runs the same three checks each directory's own `CLAUDE.md`
names as that directory's verify step:

| Directory | Verify |
|---|---|
| `server/` | `go build ./... && go vet ./... && go test ./...` |
| `desktop/` | `npm run typecheck && npm run lint && npm test` |
| `desktop/ui/` | `npx tsc --noEmit && npm run build && npm run check:locales` |

Backend tests that need Postgres start their own embedded instance
(`platform/database.StartEmbedded`) — nothing external has to be running
first. `check:locales` compares the flattened key sets of `desktop/ui/src/locales/en`
and `tr`; a key present in one and not the other fails it.

## Release process

A release is a tag push, handled by `.github/workflows/release.yml`:

1. **The tag is the version.** Each platform job writes the tag (minus its
   `v`) into `desktop/package.json` before building, so cutting a release is
   only pushing an annotated `vX.Y.Z` tag — no version-bump commit first.
2. **One GitHub Release is created first**, then macOS, Windows and Linux
   build in parallel, each on its own runner (the backend's cgo dependency
   means each OS needs its own toolchain — MSYS2/gcc on Windows, for
   instance), and each uploads its installer plus updater feed
   (`latest-mac.yml`, `latest.yml`, `latest-linux.yml`) to that release.
3. Running the workflow by hand (`workflow_dispatch`, or a non-tag ref)
   builds all three platforms as workflow artifacts without publishing or
   signing — useful for checking the build itself without cutting a release.
4. **The Homebrew cask follows automatically.** Once the macOS job has
   uploaded the `.dmg`, the `cask` job downloads it, runs
   `scripts/update-cask.sh <version> <dmg>` to rewrite `version` and
   `sha256` in `Casks/tasktrooper.rb`, and commits that to `main`. This
   repository is the tap, so that commit is what publishes the update. It
   skips pre-release tags (`v1.2.3-beta`) and never moves the cask back to an
   older version. The script still works by hand for a one-off fix.

## Contributing rules

From the root and per-directory `CLAUDE.md` files, load-bearing enough to
repeat here:

- **No code comments that explain WHAT.** A comment is for a non-obvious
  invariant, a workaround, or a WHY that isn't clear from the code itself —
  well-named identifiers already say what the code does.
- **`domain` and `application` never import `adapter`** in `server/` — the
  hexagonal boundary is enforced by convention, not tooling, so it's on every
  change to hold it.
- **Secrets never go on argv.** Children are spawned with `shell: false`; a
  token or key that needs to reach a child process goes through its
  environment or a file, never a command-line argument that would be visible
  in `ps`.
- **Nothing multi-tenant, no cloud, no control plane comes back.** No
  Firebase, no tunnel, no `X-Internal-*` headers, no team/invite/billing-plan
  UI — this is a local, single-user app and stays one.
