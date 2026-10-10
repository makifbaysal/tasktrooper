---
title: Data directory and security
description: Everything TaskTrooper keeps runs on this machine — what is in the data directory, how the app talks to its own backend, and what actually leaves.
---

# Data directory and security

TaskTrooper is local-first in the literal sense: one machine, one user, no
login, no control plane. The desktop app, the Go backend and the embedded
Postgres are three processes on your Mac, and nothing about how they talk to
each other assumes a second machine exists.

## What is in the data directory

The backend keeps everything that has to survive a restart under one
directory (`DATA_DIR`):

- **macOS app**: `~/Library/Application Support/TaskTrooper/data`
- **`make dev`**: `server/data`, or wherever `DATA_DIR` points

```
postgres/       the embedded Postgres cluster (pgdata)
postgres-bin/   downloaded Postgres binaries (cache; separate from data/)
workspaces/     git checkouts — one per repository mirror, one per task
files/          uploaded files, when RAG is enabled
```

The Postgres binaries cache (`postgres-bin/` under `userData`, or
`EMBEDDED_POSTGRES_CACHE_DIR`) is deliberately kept outside the data
directory: it is a ~30 MB download that can be deleted and re-fetched, so
deleting it and deleting your actual data are two different actions.

Two more files live in the app's `userData` folder, next to (not inside) the
data directory:

| File | Holds |
|---|---|
| `local.bin` | the encrypted bearer token and secrets key (below) |
| `settings.json` | workspace folder, launch-at-login, auto-start — no secrets |

Deleting the data directory starts the install over from empty; see
[Troubleshooting](troubleshooting.md) for when that is the right move.

## The one bearer token

There is no login screen and no account. On first run the desktop app
generates a single 32-byte token, hands it to the backend as
`SERVER_API_KEY` and to the web page as
`window.__tasktrooperDesktop.apiToken`, and every `/v1` request the UI makes
carries it as a bearer. The listener binds `127.0.0.1` only, and the backend
starts with `PORT=0` — it picks a free loopback port itself and prints
`LISTENING http://127.0.0.1:<port>`, which is how the desktop app learns
where to point the page. A restart binds a different port; nothing caches
the old one.

This token is the entire authorization model. There is no per-user identity,
no session and no role system layered on top of it — anything that clears
the bearer check can do everything the API exposes. That is a deliberate
simplification for a single-user, single-machine product: an earlier,
multi-tenant version of this schema (row-level security, a `tenants` table,
per-row tenant columns) was removed outright rather than kept dormant.

## Secrets at rest

A second secret, `mcp_secrets_key`, is generated alongside the bearer token
and handed to the backend as `MCP_SECRETS_KEY`. It encrypts everything the
backend stores that should not sit in the database in the clear: connected
provider API keys, MCP server credentials and OAuth sign-in tokens, the GitHub
token, Vercel and store credentials, and mobile signing assets.

**`mcp_secrets_key` must never be regenerated on an install that already has
encrypted rows.** It is meant to stay stable for the life of the install —
losing it makes every credential encrypted under it permanently unreadable,
with no way to recover them short of reconnecting each one by hand.

Both secrets are generated once, encrypted with the OS's own facility
(`safeStorage`, backed by the macOS Keychain — or, on a Linux session with no
keyring at all, a weaker built-in fallback rather than refusing to start),
and written to `local.bin` at `0600`. If that file exists but fails to
decrypt — the Keychain entry was removed, or the app was restored onto a
different machine — TaskTrooper regenerates both secrets rather than
refusing to start; the cost is that credentials encrypted under the old key
become unreadable and have to be reconnected. Nothing here is ever passed on
a command line: values move from the Keychain into a child process's
environment directly, with no shell in between, so a secret containing a
`$` or a backtick is never expanded.

## What leaves the machine

Everything else stays local. The things that do reach the network are:

- **Model API calls** — to whichever LLM provider an agent is configured
  against (OpenAI, Anthropic, Google, Groq, an OpenAI-compatible endpoint,
  or a local one you run yourself). See
  [Agent CLIs and API providers](runtimes.md).
- **GitHub** — cloning, pushing, opening and merging pull requests, and the
  webhook GitHub calls back on. See [Git and pull requests](git-and-pull-requests.md).
- **Store APIs** — App Store Connect and Google Play, once connected. See
  [Mobile devices and store releases](mobile-releases.md).
- **Any MCP server you add** — a server you configure under Settings → MCP
  Servers reaches whatever host you pointed it at, on your authority. See
  [MCP servers](mcp-servers.md).

- **An anonymous active-install count** — official desktop builds only. See
  [Anonymous usage statistics](#anonymous-usage-statistics) below.

Nothing else calls out on its own. There is no license check and no other
home-phoning built into the product.

## Anonymous usage statistics

Official TaskTrooper desktop builds send a small, anonymous count so the
project can see how many installs are in use. It is on by default and you can
turn it off.

- **What is sent:** a random ID made on this computer (not tied to you, your
  account or your hardware), the app version, the operating system, the CPU
  architecture, and whether you are using TaskTrooper locally or with an
  account. One event when the app opens and one at most every 30 minutes while
  its window is in use. Google Analytics also sees the connection's IP address,
  as any web request does, and derives a rough region from it.
- **What is never sent:** tasks, prompts, code, file paths, repository or
  project names, API keys, your email, or anything from your account.
- **Turn it off:** Settings → General → "Anonymous usage statistics", or on the
  menu-bar/tray icon (the only place in account mode). Turning it off stops
  sending at once and deletes the random ID. Setting the environment variable
  `TASKTROOPER_TELEMETRY=0` or `DO_NOT_TRACK=1` keeps it off regardless.
- **Builds from source and forks send nothing.** The measurement settings are
  compiled in by the official release build only; without them there is no
  network call and no ID file.

## The outbound URL guard

Because an agent's context is full of text it read from the open internet
(and sometimes from a prompt-injected source), every outbound request whose
destination an agent, a stored config row, or an API caller can influence —
`fetch_url`, `web_search`, browser tools, MCP HTTP endpoints, a deploy
target's health/logs URL, a job callback — is checked by an internal guard
before it is ever dialled.

The guard blocks loopback addresses, link-local and RFC1918 ranges, IPv6 ULA,
CGNAT, multicast, and the unusual textual forms of an IPv4 address a naive
parser would miss — because unrestricted, a URL an agent chooses could reach
this pod's own unauthenticated internal endpoints, or a cloud metadata
service, or another device on your own network. The check runs again on
every redirect hop a request follows, and the connection is pinned to the
exact address that was checked (rather than trusting a second DNS lookup at
dial time), which closes the usual DNS-rebinding trick of answering a public
address to the first lookup and a private one to the second.

Loopback access is off by default and is an explicit opt-in: setting the
process environment variable `ALLOW_LOOPBACK_TOOL_URLS=true` (never a
database setting an agent could reach) re-enables it for tools that dial an
agent-chosen destination — useful on a self-hosted install that legitimately
points a tool at another service on the same machine. Browser tools are the
one exception with the opposite default: loopback is allowed for them out of
the box, because a developer role routinely boots its own dev server on
`127.0.0.1` and opens it to look at what it just built.

## The pre-upgrade Postgres backup

An earlier version of TaskTrooper's database schema supported multiple
tenants (row-level security, a `tenants` table, `tenant_id` columns
throughout). That schema was dropped for good — a change with no way back —
so the very first time the embedded Postgres starts under a build that
includes the migration that drops it, the stopped cluster is copied whole to
`$DATA_DIR/postgres-backup-pre-133` before anything touches it. A cluster
created by a build that already has the migration gets no such copy — there
is nothing in it to lose.

To go back to that state, stop TaskTrooper, and put the backed-up copy in
place of `$DATA_DIR/postgres`. There is no in-app "restore" button for this;
it is a manual file operation, and it only ever matters if you deliberately
need the pre-upgrade database back.

## Backing up and moving the data directory

Since everything lives under one directory, backing up or moving your
install is copying that directory:

1. Quit TaskTrooper (or stop the backend from the tray) so Postgres shuts
   down cleanly rather than being copied mid-write.
2. Copy the data directory (see the path above) to wherever you are backing
   up to, or to the new machine.
3. On a new machine, install TaskTrooper, then point it at the copied
   directory before first launch — for a developer run, set `DATA_DIR`; the
   packaged app does not currently expose a picker for this, so relocating a
   packaged install's data directory means moving the folder and, if needed,
   symlinking `userData/data` to it.

`local.bin` (the bearer token and `mcp_secrets_key`) is generated per install
and is not part of the data directory — moving only `data/` to a fresh
install gives you a new token pair, and every credential encrypted under the
old `mcp_secrets_key` will need to be reconnected. Copying `local.bin`
alongside `data/` avoids that.

## Resetting

Quit TaskTrooper, then delete the data directory (and, if you want a
completely clean slate including every connected credential, `local.bin`
too). The next launch treats the install as brand new: a fresh Postgres
cluster, a fresh default board, and — if `local.bin` was deleted — a fresh
bearer token and secrets key.

## See also

- [Configuration](configuration.md) — the environment variables and config
  keys mentioned above
- [Troubleshooting](troubleshooting.md) — when a reset or a restart is the
  right next step
- [Git and pull requests](git-and-pull-requests.md) — what the GitHub token
  is used for
