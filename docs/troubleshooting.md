---
title: Troubleshooting
description: Symptom, cause and fix for the problems you're most likely to hit — from a backend that won't start to a card stuck with a spinner.
---

## The app shows the offline screen, or the backend doesn't start

The window shows an offline screen instead of the board whenever auto-connect
is off, or a required [preflight](first-run.md#check-this-mac) item is
missing — the screen names the reason. If a required item looks fine and the
backend still won't come up, check these in order.

**Something else is already using the port.** The backend is started with
`PORT=0` inside the desktop app, so it always binds a free port on its own;
this only bites when running from source with a fixed `PORT` in
`server/.env.local` (`make dev` defaults to `8085`). Change the port in that
file, or free the one it wants.

**Postgres failed to start.** With no `DATABASE_URL` set, the backend starts
its own embedded Postgres 17 under `$DATA_DIR/postgres`, downloading the
binaries (~30 MB) into `$DATA_DIR/postgres-bin` on the very first start. A
stale `postmaster.pid` left behind by a crash is cleared automatically on the
next start; if the download itself failed (no network, a blocked host), the
window narrates that rather than sitting silent, and retrying the start
retries the download.

**The data directory isn't writable.** The backend needs to create and write
inside `DATA_DIR` — inside the app that's `~/Library/Application
Support/TaskTrooper/data` — for the database, workspaces and RAG files. A
permissions problem here (a directory owned by another user, a read-only
volume) surfaces as the backend failing to start rather than a clear
permissions error; check that this directory is writable by your user.

**The pre-133 backup exists and something looks wrong after an update.**
Migration 133 permanently dropped the old multi-tenant schema, and because
that can't be undone, the *first* start on a build that carries it copies
your existing Postgres cluster to `$DATA_DIR/postgres-backup-pre-133` before
touching anything. If you need to go back, stop the app, move that backup
back into place of `$DATA_DIR/postgres`, and start an older build against it.
A fresh install with no prior data never creates this backup — there's
nothing to protect yet.

## A CLI is installed but not detected

The preflight checklist ([Check this Mac](first-run.md#check-this-mac))
probes `claude`, `cursor-agent`, `agy` and `opencode` directly on this
machine, and a CLI that works fine from your terminal can still show as
missing here for one specific reason: **a GUI-launched app inherits
launchd's `PATH`, not your shell's.** If a CLI was installed somewhere only
your shell profile puts on `PATH` — a manual install under `~/.nvm`, a
non-standard prefix — the app genuinely cannot see it even though `which
claude` finds it in Terminal. The app widens its own search past `PATH` with
the common install locations (Homebrew's `/opt/homebrew/bin` and
`/usr/local/bin`, npm's global prefixes, `~/.nvm/versions`), which covers
most cases; anything installed somewhere else needs the binary's path set as
an override in Diagnostics, or moved somewhere the search reaches.

A CLI found but reported **unusable** rather than **missing** means the
binary ran but something about it failed: it didn't answer `--version` at
all, or its version is older than this app can drive (`2.0.0` is the floor
for Claude Code). The detail line names which.

`claude-account` is checked separately from `claude` on purpose: a Claude
account with the CLI installed and signed in can pass every "is it there"
check and only fail once a real task tries to run, deep inside a session log
you weren't reading — this split is what says so up front instead.

## Runs fail immediately

**No runner on this host.** An agent on a host-executed provider (Claude
Code, Cursor, Antigravity, OpenCode) with no matching binary on `PATH` fails
every run — board or chat — with one sentence naming where it actually can
run, rather than falling back to some other provider silently. There is no
"enabled" flag for these providers: the binary being on `PATH` (or named by
its own environment variable — `CLAUDE_CODE_BIN`, `CURSOR_AGENT_BIN`,
`ANTIGRAVITY_BIN`, `OPENCODE_BIN`) is the only switch. See [Agent CLIs and
API providers](runtimes.md#the-four-local-clis).

**Model not valid for the provider.** None of the four local CLIs validate
the `--model` value you give them, so a typo in an agent's **Model** field
doesn't fail fast on save — it fails (or silently picks something else) the
next time that agent runs, and costs the run. On an HTTP API provider, an
unrecognized model name is rejected by the provider itself, quickly, as a
400. Either way, check the agent's Settings tab for the exact model string
against what its provider actually offers.

## A card sits with a spinner

Moving into **Code Review** normally waits for the build/test pipeline to
report before the reviewer is dispatched — see [Quality gates → the
code-review gate](quality-gates.md#the-code-review-gate) for the full
mechanism. The wait is bounded to 45 minutes and the card records *why* it
opened without a real result, shown as a warning instead of a spinner once it
does:

| Reason | What happened |
|---|---|
| `no_ci_configured` | The repository has no CI wired up — there was never anything to wait for |
| `ci_unavailable` | GitHub reports billing, quota, or an outage — an answer that can't come |
| `timeout` | Nothing reported within 45 minutes |

If you'd rather Code Review never wait on CI at all, turn off **require
pipeline for review** on the repository's settings (on by default); with it
off, the reviewer is dispatched immediately and the card just says the gate
was disabled. If a card is spinning with none of these reasons showing yet,
it likely just hasn't hit the timeout — give it a few minutes before
assuming something is stuck.

## A QA run is marked failed as ungrounded

A QA run in **In QA** or **Ready for QA** can't be marked complete without
actually running something — a real request, a headless-browser check, a
mobile simulator/emulator interaction. Writing a review comment, moving the
card, or reading the pipeline status doesn't count as evidence, and neither
does reading code, because QA's job is black-box. A run that finishes without
ever successfully calling one of QA's grounding tools is marked **failed**
with the reason posted on the task, and a fresh QA attempt is dispatched
automatically — bounded at three consecutive failures before it's left for a
person. See [Quality gates → QA must execute](quality-gates.md#qa-must-execute).
`analiz` tasks, and a run that ends by asking a clarifying question, are
exempt.

## A task is parked on Blocked

A blocked card is waiting on something outside any agent's control, not
failing — it resumes on its own once the thing it's waiting for clears.
What's on the card tells you which:

| Reason shown | Waiting on | Resumes when |
|---|---|---|
| "Claude usage limit reached" | The Claude subscription's usage limit | The recorded reset time passes; a sweeper checks every minute. See [Usage limits and concurrency](usage-limits.md) |
| "Waiting for blocking tasks" | This task's own `blocked_by` relations | Every task it's waiting on reaches Done or Released, or is deleted |
| A clarifying question | You, answering in the task's **Discuss** chat thread | You answer — no manual drag back onto a working column needed. See [Quality gates → Clarification](quality-gates.md#clarification) |
| "Waiting for a test device" | A shared iOS simulator, Android emulator or physical phone | Any device in the pool frees up |

A usage-limit park keeps the CLI session it had — it resumes with `--resume`
rather than starting over, so an agent picks up exactly where it stopped
instead of re-reading the repository. A device park is a queue: any device
freeing up can resume the oldest waiting task, not necessarily the one that
just released it.

## "build constraints exclude all Go files" when building from source

This means the backend was built with `CGO_ENABLED=0`. `smacker/go-tree-sitter`
is a cgo package, so every tree-sitter grammar fails to build under that
constraint — the error reads like a toolchain problem and isn't one. Build
with `CGO_ENABLED=1` (the desktop app's own build script always sets it,
including when cross-compiling darwin/amd64 from arm64 — clang takes `-arch`
from Go). See [Run from source](run-from-source.md#prerequisites).

## Where the logs are

**The backend logs to stderr.** The only thing it ever writes to stdout is
the single `LISTENING http://127.0.0.1:<port>` line the desktop app reads to
learn its address; every log line, from every level, goes to stderr. Running
from source with `go run ./cmd/agent-server`, that's just your terminal.

**Inside the desktop app**, each child process's stdout/stderr streams live
into the app itself rather than a separate log file — the Status view shows
it per-process as it happens. The tray's **Reveal** action (and the
equivalent in Diagnostics) opens the app's own data folder
(`~/Library/Application Support/TaskTrooper`) in Finder, which holds
`settings.json`, `data/` (the database, RAG files, workspaces) and
`postgres-bin/` — useful alongside the live log view rather than in place of
it.

## Restarting the backend from the tray

The tray menu has **Start the local server** and **Stop the local server** —
whichever applies to the current state is enabled, the other greyed out.
There's no separate "restart": stop it, then start it again. Quitting the
app the normal way also stops the backend (and with it any Appium hub it
started for a mobile task), and leaves the embedder running so a quick
relaunch doesn't pay to reload the embedding model again.

## Agents still "seeding" at boot

Right after a fresh install, or after adding a repository, `/admin/agents`
can report `seeding: true` for a short while. At boot the server creates any
missing default role agents and backfills skills or rules for a partially
seeded one; the seed itself stores skills without their embeddings so it
never waits on the embedder, and a background job fills those embeddings in
over the following minutes (retried for up to 30 minutes if the embedder
isn't ready yet). Agents work before this finishes — semantic skill lookup
is what's still catching up, not the ability to run tasks — and the flag
clears itself once the backfill completes. There's nothing to do but wait it
out; it doesn't recur once an install's agents are fully seeded.
