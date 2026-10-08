---
title: MCP servers
description: Connect your own MCP servers to your agents, and how that differs from the board tools a Claude Code session already gets.
---

**Settings → MCP Servers** connects external MCP servers to your agents —
your own tools, reached over stdio or HTTP, alongside the built-in terminal,
file, web and board tools every agent already has.

This is the opposite direction from the MCP endpoint a Claude Code session
calls back on for the board's own tools. See [the difference](#the-other-direction-tasktroopers-own-mcp-endpoint)
below, and [Agent CLIs and API providers](runtimes.md#how-a-run-is-started)
for how that endpoint works.

## Adding a server

Open **Settings → MCP Servers** and choose **Add Server**. You either pick a
ready-made template or start from a custom configuration; every field is
editable before you save either way.

Every server needs:

| Field | Meaning |
|---|---|
| ID | A short name with no spaces or slashes. This becomes the namespace every one of the server's tools is served under, and it cannot be changed later without recreating the server. |
| Transport | `stdio` (a local process) or `http` (an existing MCP endpoint) |
| Enabled | Whether the server connects at all. A disabled server is kept configured but not dialled. |
| Available to | **Only agents that list it** (the default for a server you add) or **All agents** — see [Who can use a server](#who-can-use-a-server). |

**stdio** servers need a `command` and its `args` — the same shape as a
`.mcp.json` entry, run as a local child process (`npx -y
@zereight/mcp-gitlab`, for example). **http** servers need a
`url` and, optionally, request `headers`.

An `allowed_tools` list on the server narrows which of its tools are actually
registered; leave it empty to take every tool the server advertises.

An HTTP server's URL is checked against the same outbound guard every other
agent-reachable URL goes through (loopback, link-local and private-network
addresses are refused) before the server is ever saved — see [Data directory
and security](data-and-security.md) for what that guard covers.

## Templates

Eight templates ship with the app, each pre-filling the transport, command or
URL and naming which fields are secrets:

| Template | Transport | What it needs |
|---|---|---|
| Filesystem | stdio | A root directory to expose |
| Git | stdio | A repository path — runs through `uvx`, so [uv](https://docs.astral.sh/uv/) has to be on your PATH |
| GitHub | http | A personal access token, sent as the `Authorization` header to GitHub's own hosted server |
| GitLab | stdio | `GITLAB_PERSONAL_ACCESS_TOKEN`, and `GITLAB_API_URL` if you are on a self-hosted instance |
| PostgreSQL | stdio | A connection URL |
| Slack | stdio | `SLACK_BOT_TOKEN`, `SLACK_TEAM_ID` |
| Hugging Face | http | A bearer token, sent as the `Authorization` header |
| Browser | stdio | Nothing — enabled by default |

Picking a template fills the form with its defaults; you still choose the
server's ID, fill in whatever it asks for (a token, a path, a connection
string), and can edit anything else — command, args, extra environment
variables — before saving. A template already added to your list of servers
is not offered again. **Custom** starts from a blank stdio server instead of
a template.

## How the tools are named

Every tool a connected server advertises is registered as
**`mcp_<server_id>_<tool_name>`** — the server's ID, then the tool's own
name, exactly as the MCP server defined it. An agent the server is
[available to](#who-can-use-a-server) sees it under that name and calls it
like any other tool; its parameters and description come straight from the
server's own tool definition, unchanged. Which server a tool belongs to is
recorded when it is registered, so an ID that itself contains `_`
(`figma_team`) is never confused with a shorter one (`figma`).

This is a flat namespace on purpose: a GitHub server with ID `github` and a
tool called `create_issue` is served as `mcp_github_create_issue`, and
nothing about the name changes based on which agent is calling it or which
column the run is in. [Tool policies](tool-policies.md) control access the
same way they control every built-in tool — by name or by server ID.

## Who can use a server

Each server has an **Available to** setting:

| Setting | Which agents get the server's tools |
|---|---|
| **Only agents that list it** | Agents whose tool policy names the server under **MCP servers**. This is the default for every server you add, so connecting a design tool or an issue tracker does not hand its tools to every agent at once. |
| **All agents** | Every agent whose tool policy names no MCP servers, plus any agent that names this one. |

An agent's own list is exact: an agent whose tool policy names one or more MCP
servers gets exactly those, so a server set to *All agents* that it does not
name is not served to it either. Give an agent a server with the **MCP
servers** picker under **Settings → Tool policy** on the agent's page; the MCP
Servers table shows which agents list each server.

Servers that existed before this setting, and the templates the app seeds on a
fresh install, are set to *All agents*, so an upgrade changes no agent's
tools.

The same rule applies everywhere a tool reaches an agent: the tool list the
model sees, a call it makes to a tool it was not shown, and the `/mcp`
endpoint a Claude Code session calls back on.

## Tool results with images

When a tool returns an image — a screenshot of a design frame, a rendered
chart — the model receives it as a picture, the way it receives
`browser_screenshot`'s, not as a block of base64 text. Text blocks stay text.
PNG, JPEG, GIF and WebP up to 5 MB are passed through; any other image, or a
larger one, is reduced to its type and size. A Claude Code session calling
the tool through TaskTrooper's `/mcp` endpoint gets the same image as MCP
image content.

## Signing in with OAuth

Hosted `http` servers that follow the MCP authorization spec do not take a
pasted token: they sign you in with OAuth. TaskTrooper runs that sign-in
itself, on this machine:

1. A server that answers `401` shows **Sign-in needed** and a **Connect**
   button.
2. **Connect** finds the server's authorization server (from the
   `WWW-Authenticate` challenge, or `/.well-known/oauth-protected-resource`,
   then the authorization server's own metadata), registers TaskTrooper as a
   client when the authorization server supports dynamic client registration,
   and opens the sign-in page in your browser.
3. Once you approve, the browser comes back to
   `http://127.0.0.1:<port>/oauth/mcp/callback` — TaskTrooper itself, on the
   loopback address — and shows **Connected — you can close this tab**. The
   row switches to **Signed in** and the server reconnects with its token.

What to know:

- Every sign-in uses PKCE (S256), and the `resource` parameter names this MCP
  server, so the token is only good there. An authorization server without
  S256 support is refused.
- Tokens are encrypted with the same key as the other MCP secrets and never
  sent back to the UI. An access token is refreshed a minute before it
  expires, and again whenever the server rejects it. When the refresh token
  itself is refused, the row shows **Sign-in expired** and **Reconnect**.
- A sign-in link works once and for ten minutes; starting again cancels the
  earlier one.
- When the authorization server cannot register clients by itself, **Connect**
  asks for the client ID (and, if it has one, the secret) of an OAuth app you
  register with the provider, and shows the redirect URI to register. Its port
  changes when TaskTrooper restarts; providers that follow the loopback
  redirect rules accept any port.
- **Disconnect** forgets the tokens. A client ID you entered is kept for the
  next sign-in.
- A sign-in replaces any `Authorization` header configured on the server.
  Changing the server's URL or transport forgets the sign-in, because the
  token was granted for the old address.
- Discovery, registration and token requests go through the same outbound
  guard as the MCP connection itself, and none of the requests that carry a
  credential follow a redirect.

## Secrets

A field a template marks as secret — a token, a key, a password — is
encrypted at rest and never sent back to the browser in the clear: the
server list shows a masked placeholder for it instead of the stored value,
and saving the form again without touching that field leaves the stored
secret alone. A custom server infers which of its own `env`/`headers` values
look like secrets the same way, rather than requiring every field to be
hand-flagged.

Non-secret fields — a repository path, a team ID, a Postgres host with no
credentials in it — are stored as plain configuration next to the server, not
encrypted, since masking them would only make the form harder to read.

Deleting a server removes its stored secrets along with it; there's nothing
left over to clean up by hand.

## Connection status

Each row in the MCP Servers table shows:

| Column | What it means |
|---|---|
| Transport | `stdio` or `http` |
| Available to | **All agents** or **Listed agents**, and the agents that list the server |
| Status | **Connected**, **Inactive** (disabled), **Needs setup**, **Sign-in needed**, **Sign-in expired**, or **Error**; a signed-in `http` server also shows **Signed in** with **Disconnect** |
| Tools | How many tools the server is currently serving, expandable to the list of names |
| Enabled | The toggle that connects or disconnects the server without deleting it |

**Needs setup** means a required field of the template — a token, a path, a
connection URL — is still empty, so there is nothing to dial yet. The row names
the fields, and so does the edit form, rather than leaving you to read it out of
a connection error.

An enabled server that failed to connect — a bad command, an unreachable
URL, a stdio process that exited — shows **Error** with the failure reason;
toggling it off and back on, or editing and saving it, retries the
connection. The table refreshes in the background every few seconds while
the page is open.

## The other direction: TaskTrooper's own MCP endpoint

MCP servers you connect here are tools your agents call *out* to. TaskTrooper
also runs the reverse: a `/mcp` endpoint the server mounts on its own port,
which is how a Claude Code session reaches the board itself — moving cards,
ticking acceptance criteria, reading a task's pull request, and so on. That
endpoint:

- serves tools named `mcp__tasktrooper__<name>`, a different namespace from
  `mcp_<server_id>_<tool_name>`;
- is bound to `127.0.0.1` and guarded by a token minted for that one run (or,
  in chat, that one turn) and revoked when it ends — nothing you configure
  here;
- is the *only* MCP configuration a board or chat session ever loads. The
  session is started with `--strict-mcp-config`, which means a repository's
  own checked-in `.mcp.json` and your personal Claude Code settings under
  `~/.claude` are never read into a TaskTrooper run — servers connected here
  are what stand in for them.

In other words: the servers on this page are what a TaskTrooper agent can
call; the `/mcp` endpoint is what a TaskTrooper agent already answers to
Claude Code. See [Agent CLIs and API providers](runtimes.md#how-a-run-is-started)
for the run-side detail on that endpoint.

## Where this is used

A connected, enabled server's tools reach the agents it is
[available to](#who-can-use-a-server) — board runs and chats alike, on any
provider. There is no per-repository or per-task MCP configuration: which
agents get a server is decided once, by its **Available to** setting and the
agents' tool policies.
