---
title: First run and setup
description: The guided setup that checks your Mac, connects an agent runtime and GitHub, and imports your first project.
---

The first time you open TaskTrooper, it takes you through a guided sequence
at `/setup` before showing you the board. It has four steps, each unlocked
only once the previous one has actually succeeded — not just been visited —
so quitting mid-setup, restarting the app, or disconnecting something next
week all land you back on the right screen with nothing stale to work
around.

## Two choices before the sequence

The first run asks two one-time questions, answered only once each and
remembered in `localStorage` (`tt.firstRun.*`): **local or an account**, and
**which team to start from**. Each is skipped once it has been answered.

| Question | What it asks |
|---|---|
| Local or an account? | Run everything on this computer with no sign-in, or join your board and team from any device with an account. The account option hands the window over to the account's own web app (and is offered only when the desktop shell exposes `account.signIn()`); the local choice goes straight into the sequence. You can change it later in Settings, and signing out of an account returns this first-run question nothing — the choice is saved before the hand-over, so the app goes back to your chosen setup, not to "local or account?" again. |
| What kind of team? | Pick a starting team: **Web application**, **Mobile application**, **Game**, **Data & analytics**, or **Custom**. A template preselects the catalog agents that team starts with; every agent is shown with a toggle and can be turned on or off before you confirm. The **product manager is always on** — the board has no lead without one — so its toggle is locked. The rest, including the **security agent**, start from the template's choice and are toggled through each agent's `enabled` flag (`PUT /admin/agents/:id` on Confirm). |

The team's agent list is not set in stone: after setup, open **Settings →
Agents** to toggle any agent on or off, or edit an agent's role, runtime and
tools, and add your own agents — see [Role agents](role-agents.md) and
[Your own agents](custom-agents.md).

| Step | What it does |
|---|---|
| Check this Mac | Probes git, agent CLIs, and optional capabilities (Chrome, Xcode, Appium, the Android SDK) |
| Connect an agent runtime | Connect Claude Code, Cursor, Antigravity or OpenCode, or add an API provider with your own key |
| Connect GitHub | Sign in through GitHub's own OAuth screen so agents can clone, branch and open pull requests |
| Your first project | Create a project and import a repository into it |

You can revisit an already-completed step at any time — to re-run the
preflight, swap the connected agent CLI, or add another project — without
losing your place in the sequence.

## Check this Mac

This step runs entirely inside the desktop app: it probes your machine
directly (installed binaries, signed-in accounts) through the Electron
shell, so it cannot run in a browser tab. If you're looking at TaskTrooper's
web UI without the desktop app behind it, this step tells you to download the
Mac app instead of offering a button that could never work.

The checklist, in the order you should fix things:

| Item | Required | Notes |
|---|---|---|
| `agent-server` | yes | The bundled backend binary. Missing means a broken install, not something to fix by hand. |
| Postgres | no | Always reports `ok` — it names the ~30 MB download that happens on first backend start rather than a real failure. |
| `git` | yes | Every task clones a repository and commits to a branch. |
| Claude Code | no | One agent CLI (this or one of the next three) is enough. |
| Claude account | no | Reported separately from the `claude` binary: a Claude account without Claude Code access can pass every "is it installed" check and only fail once a real task tries to run, minutes later, deep in a log you weren't reading. |
| Chrome | no | Needed for QA's headless-browser testing. Never blocks. |
| Xcode command line tools | no | Needed for iOS work. Never blocks. |
| Appium, Appium XCUITest driver, Appium UiAutomator2 driver | no | Needed to drive iOS simulators / Android emulators for mobile QA. Never blocks. |
| Android SDK | no | Needed for Android work. Never blocks. |
| `agy` (Antigravity), `cursor-agent` (Cursor), `opencode` (OpenCode) | no | The other agent CLIs a task can run on — which one is chosen per agent, not per machine. |

Only `agent-server` and `git` are required; everything else is a capability
that unlocks more of the product without ever stopping you from starting.
Once every required item is green, **Connect** unlocks in the next step.

## Connect an agent runtime

Agents do their work through a coding CLI running as a local process on this
Mac, or through a model API. You need exactly one to get started:

- **An agent CLI** — Claude Code, Cursor, Antigravity or OpenCode. Pressing
  Connect verifies the binary is installed and signed in, then writes every
  enabled agent's role, rules and skills to disk in the layout that CLI
  reads. Only one local CLI can be connected at a time; connecting a
  different one disconnects the first.
- **An API provider** — OpenAI, Anthropic, Google Gemini, Groq, or any
  OpenAI-compatible endpoint (LM Studio, Ollama, vLLM, OpenRouter, or your own
  IP) with a base URL and key. These ask for no local binary at all.

Both live under **Settings → LLM Connection** if you want to add more later,
or switch which one an individual agent uses — see [Agent CLIs and API
providers](runtimes.md). That page has three sections:

| Section | What's in it |
|---|---|
| Local Agent CLIs | Claude Code, Cursor, Antigravity, OpenCode — connect asks for no API key and no address, only that the binary is installed and signed in |
| Native Providers | Google Gemini and Anthropic's own API, connected with a key |
| OpenAI-compatible Endpoints | Any number of named endpoints — OpenAI, Groq, OpenRouter, LM Studio, Ollama, vLLM, or your own IP |

Connecting a local CLI also writes every enabled agent's role, rules and
skills to disk in the layout that CLI reads, so the moment it's connected
your agents are ready to run through it.

## Connect GitHub

Agents clone, branch, push and open pull requests as your GitHub account. You
authorize this through GitHub's own permission screen — nothing is typed in
by hand, and no personal access token ever passes through this step. Once
connected, you can import private repositories the same way the setup
sequence does.

GitHub also lives permanently under **Settings → Integrations**, alongside
Vercel, Google Cloud and the mobile app stores, if you need to reconnect or
switch accounts later.

## Your first project

A project groups the repositories that ship together. This step:

1. Creates a project (a name is enough to start).
2. Imports a repository into it. TaskTrooper asks what kind of repository it
   is (backend, frontend, mobile, worker, monorepo — detected automatically
   from the working copy when it can tell), and profiles how it deploys and
   how it's built and tested.

You can add more repositories to the same project, or create more projects,
at any time from **Projects** once setup is done.

## What the app starts underneath

Opening TaskTrooper starts two local processes, in this order:

| # | Process | Started by | Gates the window? |
|---|---|---|---|
| 1 | Embedder | App init, always | No |
| 2 | Backend (`agent-server`, from `server/`) | The supervisor | **Yes** — the window appears only once `/health` answers 200 |

The backend brings up its own embedded Postgres (downloading the binaries on
the very first start, as noted above). The window itself doesn't appear until
the backend is answering, because the page reads its API address from the
same process that started it — there's nothing useful to show before then.

If Appium is installed, the backend starts an Appium hub on port 4723 itself,
but only when a mobile task needs one, and stops it again after 10 idle
minutes; a hub already listening there is used instead.

Stopping the app stops the backend (which stops its Postgres and any Appium
hub it started); the embedder is left running so a quick restart doesn't have
to reload the embedding model.

### The tray and the offline screen

TaskTrooper keeps running in the menu bar even when its window is closed —
closing the window is not the same as quitting the app. Whether it starts the
backend automatically on launch is controlled by an **auto-connect** setting
(on by default). With it off, or if a required preflight item is missing, the
window shows an offline screen naming the reason instead of starting
anything, and you connect manually from there once it's fixed.

If the backend crashes or falls behind on its health check while running, the
window reflects that as a degraded state rather than pretending everything is
fine — the difference between "stopped" and "restarting, but not there yet"
is exactly what tells you whether to wait or to go looking at logs.

### Other integrations

GitHub is the only account setup asks you to connect, because it's the only
one every task needs. **Settings → Integrations** also holds the accounts
individual features need once you reach for them: Vercel (linking hosted
projects for deploys), Google Cloud (reading Cloud Run services and GKE
clusters), and the App Store Connect / Google Play credentials that sign
and upload mobile releases. None of these block setup, and you can add
them at any point later.

## Resuming or redoing setup

Because every step's status is derived from the real thing it checks —
whether a CLI is actually signed in, whether GitHub is actually connected —
rather than from a "seen this screen" flag, there's nothing to reset by
hand. Disconnect an agent CLI or a GitHub account from its own settings page
and the corresponding step reports itself as not done again; reconnecting
picks the sequence back up in the same place.

Once setup is complete, you land on the board — see [Your first
task](first-task.md) to put it to work.
