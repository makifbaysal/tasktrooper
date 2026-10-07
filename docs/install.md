---
title: Install
description: Installing TaskTrooper on macOS, Windows and Linux, and what first launch downloads.
---

TaskTrooper is a desktop app. There is no server to stand up and no account to
create — install it, open it, and the guided setup ([First run and
setup](first-run.md)) takes over from there.

## macOS

### Install script

```sh
curl -fsSL https://raw.githubusercontent.com/makifbaysal/tasktrooper/main/scripts/install.sh | bash
```

This downloads the latest release's universal `.dmg`, copies `TaskTrooper.app`
into `/Applications`, and prints the commands for anything else your Mac
needs (git, an agent CLI). It does not install anything on your behalf beyond
the app itself.

Options:

| Flag / variable | Effect |
|---|---|
| `-y`, `--yes` | Replace an existing install and answer every prompt yes |
| `TASKTROOPER_INSTALL_DIR` | Install somewhere other than `/Applications` |

If `/Applications` (or your chosen directory) is not writable by your user,
the copy step runs under `sudo`. If a copy is already running, the script
quits it before replacing it.

### Homebrew

```sh
brew tap makifbaysal/tasktrooper https://github.com/makifbaysal/tasktrooper
brew install --cask tasktrooper
```

The repository is its own Homebrew tap. The cask requires macOS Ventura or
later, and its `zap` step (`brew uninstall --zap`) removes
`~/Library/Application Support/TaskTrooper`, `~/Library/Logs/TaskTrooper` and
the app's preferences plist along with the app itself.

### Manual `.dmg`

Download the universal `.dmg` from
[Releases](https://github.com/makifbaysal/tasktrooper/releases) and drag
`TaskTrooper.app` to Applications.

### Signing and Gatekeeper

The app is **ad-hoc signed and not notarized yet**. Apple's Gatekeeper refuses
a quarantined copy on first launch with a dialog that offers no way past it,
so both installation paths clear the quarantine flag for you:

- the install script runs `xattr -dr com.apple.quarantine` on the copied app
  when Gatekeeper's own assessment fails;
- the Homebrew cask does the same in its `postflight` step.

If you copy the `.app` some other way (an unzipped download, a shared drive),
you may need to right-click → Open the first time instead.

## Windows

Download `TaskTrooper-<version>-setup.exe` from
[Releases](https://github.com/makifbaysal/tasktrooper/releases) and run it.
The installer is NSIS-based, lets you change the install directory, and is
**not code-signed yet** — Windows SmartScreen will ask you to confirm the
first run.

## Linux

Two package types, both x86_64 only for now:

- **AppImage** — `TaskTrooper-<version>-x86_64.AppImage`. Make it executable
  and run it:

  ```sh
  chmod +x TaskTrooper-*.AppImage
  ./TaskTrooper-*.AppImage
  ```

- **`.deb`** — install with your usual package manager (`dpkg -i` or your
  distribution's front end).

Neither package is code-signed yet.

The install script and the Homebrew tap are macOS-only; on Windows and Linux,
download from [Releases](https://github.com/makifbaysal/tasktrooper/releases)
directly.

## What first launch downloads

TaskTrooper's desktop shell starts two local processes on launch: an
embedding server and the Go backend (which starts an Appium hub only when a
mobile task needs one). Two things are pulled down the first time, into the
app's own data directory rather than bundled into the installer:

| Download | Size | Why it's not bundled |
|---|---|---|
| Postgres binaries | ~30 MB | The backend brings up its own embedded Postgres 17 when no external database is configured. On macOS these land in `~/Library/Application Support/TaskTrooper/postgres-bin`. |
| Embedding model (`nomic-embed-text-v1.5`) | ~140 MB | Used by the bundled embedder to index your repositories for semantic code search and agent memory. |

The window narrates these downloads rather than sitting on a blank screen —
you'll see the progress before the backend answers its first health check and
the UI appears.

## What TaskTrooper needs

| Requirement | Why |
|---|---|
| `git` | Every task clones a repository and commits to a branch. Required — the app refuses to start without it. |
| One agent runtime | Either an agent CLI (Claude Code, Cursor, Antigravity, or OpenCode) installed on this Mac, **or** an API key for a model provider (OpenAI, Anthropic, Google Gemini, Groq, or any OpenAI-compatible endpoint). Exactly one is enough to get started; you can add more later. |

On first launch (and before every backend start), TaskTrooper probes the
machine and tells you exactly what's missing and the command to fix it — see
[First run and setup](first-run.md) for the full checklist, including the
optional items (Chrome, Xcode command line tools, Appium, the Android SDK)
that unlock QA and mobile capabilities but never block the app from starting.

If `git` is missing on macOS, the fix is:

```sh
xcode-select --install
```

If no agent CLI is found, the install script prints install commands for
each:

```sh
# Claude Code
curl -fsSL https://claude.ai/install.sh | bash

# Cursor
curl https://cursor.com/install -fsS | bash

# OpenCode
npm install -g opencode-ai

# Antigravity — see antigravity.google/docs/cli
```

## Packaging notes

The macOS build is a **universal** app (arm64 + amd64) with the hardened
runtime enabled, because the bundled Go backend is itself built universal —
running the Electron shell under Rosetta while its backend runs natively (or
vice versa) is the kind of mismatch that is hard to support. Windows and
Linux builds are produced on their own runners, since the backend is a cgo
binary.

There is no auto-update feed configured for a plain build from this
repository (`publish: null` in the packaging config) — only the project's own
CI-built releases carry one. A build you package yourself with `make
package` will report its update status as unsupported, which is the honest
answer rather than a silently broken check.

## Running from source

```sh
make setup      # go mod download + npm ci (desktop, desktop/ui)
make desktop    # Electron app in dev mode
make dev        # or: backend + UI dev server, open http://localhost:3200
make package    # build the installer for this OS into desktop/release
```

See [Run from source](run-from-source.md) for the full walkthrough.
