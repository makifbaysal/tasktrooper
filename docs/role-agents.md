---
title: Role agents
description: The eleven role agents a board is seeded with, what each one does, and what "seeding" and reconciliation mean when you upgrade.
---

When you set up a repository, TaskTrooper creates eleven role agents for you,
covering backlog, architecture, backend, frontend, mobile, data, games,
security, QA, release and design. Five of them — the product manager, the
system architect, the QA agent, the security agent and the release engineer —
are the **core** set every board keeps on; the other six run the work their
area calls for. They show up in the sidebar under **Agent Chats**, and each has
its own **Settings**, **Skills**, **Rules**, **Columns**, **Memory** and
**Performance** tabs.

You do not have to use them as given — everything here can be edited from
those tabs or replaced with [your own agents](custom-agents.md) — but they are
built to run a board end to end without any setup beyond registering a
repository.

## The agents

| Agent | Runs as | Effort | What it does |
|---|---|---|---|
| `product-manager` | `generalPurpose` | medium | Backlog, requirements, stakeholder questions, board organization |
| `system-architect` | `system-architect` | high | Analysis tasks, task decomposition, code review |
| `backend-developer` | `backend-engineer` | high | Go/Fiber, Java/Quarkus, C#/ASP.NET Core, TypeScript/Node.js and Rust/axum APIs, database migrations, backend tests |
| `frontend-developer` | `frontend-engineer` | high | React/Vite/Tailwind UI |
| `mobile-developer` | `mobile-dev-engineer` | high | Flutter, SwiftUI, Jetpack Compose apps; store deploys |
| `data-scientist` | `data-scientist` | high | Data pipelines, SQL/dbt analytics, model training and evaluation, notebooks turned into tested modules |
| `game-developer` | `game-developer` | high | Unity, Godot, Unreal Engine, web (Phaser, PixiJS, three.js, Babylon.js) and Bevy games |
| `security-agent` | `security-reviewer` | high | Security-only review of every pull request in Code Review; blocks the ones that introduce an exploitable vulnerability |
| `qa-agent` | `generalPurpose` | medium | Manual test rounds against a running build |
| `release-engineer` | `generalPurpose` | medium | Merge, ship, verify and roll back — owns Done and Released |
| `ui-designer` | `generalPurpose` | high | The project's design system, screen designs before they are built, design reviews — see [Design systems](design-systems.md) |

"Runs as" is the **subagent type** shown on the agent's Settings tab — it is
metadata the CLI session carries, not a separate program. "Effort" is the CLI
effort level (`low`/`medium`/`high`/`xhigh`/`max`) the agent's sessions run
at; a checklist pass and a multi-file refactor do not want the same depth of
thinking, so the roles that build or judge code (system-architect, the
developers, data-scientist, game-developer, security-agent and ui-designer)
are seeded at `high` while the roles that mostly read, route and ship
(product-manager, qa-agent, release-engineer) run at `medium`.

Every seeded agent's provider is Claude Code when that CLI is available on this
machine. On Claude Code — and the other host CLIs (Cursor, Antigravity,
OpenCode) — the model is left **empty**, so the CLI picks its own account
default rather than TaskTrooper pinning a model name. A `sonnet`/`opus` pair
(ordinary work / the subtasks the planner rates "hard", plus self-reflection
and the golden-gate judge, see [Self-evolution](self-evolution.md)) is the
default of the separate Anthropic **HTTP** provider instead, inherited only
when an agent is put on that provider. If no CLI is available, the agents are
still created but left without a provider until one is configured on the
Settings tab.

## Which columns each agent works

Agents are dispatched to a board column either because they are the task's
**assignee** or because they **subscribe** to that column. Every role agent
carries at least one subscription out of the box, so tasks flow to the right
agent automatically instead of bouncing back to whoever implemented the task:

| Agent | Subscribed columns | Why |
|---|---|---|
| `backend-developer`, `frontend-developer`, `mobile-developer`, `data-scientist`, `game-developer` | Todo, Need Revision | Picks up new work and returned work in its area |
| `ui-designer` | Todo, Need Revision | Picks up `design` tasks and design revisions |
| `system-architect` | Code Review | Reviews the diff once a developer hands a task there |
| `security-agent` | Code Review | Reviews the same diff for security only |
| `qa-agent` | Ready for QA | Picks the task up, then moves it into In QA and runs its rounds there |
| `product-manager` | PM UAT | Reviews the finished task against its acceptance criteria |
| `release-engineer` | Done, Released | Merges the pull request, watches the deploy, and rolls back if it fails |

Code Review is the one column where every subscriber is a **required
reviewer**: each records its own verdict, and the task leaves only when all of
them have decided — to Ready for QA when every reviewer approved, to Need
Revision when any of them asked for changes. The task's detail panel lists each
reviewer's verdict per review round, pending ones included. Unsubscribe the
`security-agent` from Code Review (its Columns tab) to go back to a single
reviewer.

The developer agents are routed by the repository's area: `data` repositories
(Python data/ML code, dbt projects, notebooks) go to `data-scientist`, `game`
repositories (Unity, Godot, Unreal, web-engine and Bevy projects) go to
`game-developer`. With neither agent assigned, both areas fall back to the
backend developer.

QA subscribes to Ready for QA only: once it claims a task it moves the task
into In QA itself and runs its rounds there as the assignee, so In QA needs no
subscription of its own. The merge and the deploy belong to the
`release-engineer`, which owns Done and Released — QA's own work ends at its
verdict (see [Tool policies](tool-policies.md) and
[Git and pull requests](git-and-pull-requests.md)).

Analysis Review and Human UAT have no agent subscriber on purpose: those are
the columns where a human approves or rejects, not an agent.

A task's **assignee** is dispatched when the task sits in a column nobody
subscribes to. With the developers now subscribed to Todo and Need Revision,
the only working column left to the assignee alone is **In Progress**, where
the implementation itself happens; the assignment is made by the product
manager or picked up by the agent claiming the task. You can see and change
which columns an agent is subscribed to on its **Columns** tab.

## Skills, rules, and tool access

Each role agent ships with a curated set of markdown skills and orchestrator
rules — see [Skills and rules](skills-and-rules.md) for what those are. Counts
as seeded today:

| Agent | Skills | Rules |
|---|---|---|
| `product-manager` | 19 | 22 |
| `system-architect` | 15 | 11 |
| `backend-developer` | 33 | 15 |
| `frontend-developer` | 27 | 14 |
| `mobile-developer` | 30 | 16 |
| `qa-agent` | 24 (4 shipped disabled) | 18 (2 shipped disabled) |
| `ui-designer` | 9 | 10 |

The developer roles share a common set of general skills (board comment style,
performance awareness, TDD workflow, incremental commits, root-cause
debugging, CI/CD authoring, deploy templates, incident response, and a few
more) on top of their own language- and platform-specific ones. Because every
skill and rule belongs to exactly one agent — there is no global catalog —
those shared skills are **physically copied into each agent's own Skills tab**,
which is why a developer's total above already includes them. You can add, edit
or delete any of them from that agent's **Skills** and **Rules** tabs.

What each role's tools actually allow is covered in full on
[Tool policies](tool-policies.md); in short: the developer roles and QA get the
terminal and (for developers) the file editors, the product manager can create,
move and delete board tasks, the release engineer holds the merge and the
deploy tools, and QA is deliberately kept away from the code-reading tools — it
tests the running product, not the source.

### QA runs manual tests only, for now

The QA agent's skill and rule set is built for a two-phase flow — a manual
pass now, and a later automation phase that writes the same scenarios into a
dedicated test project. The automation half exists as skills and rules today
(`e2e-automation-project`, `automation-pipeline-integration`,
`test-doubles-wiremock`, `test-database-seeding`, and two matching rules) but
they are seeded **disabled**: the content is there, but nothing is injected
into QA's prompt until you turn them back on from the Skills/Rules tabs. QA's
active flow today is: move the task into In QA, boot the app or the API in
the task's own workspace, run real requests or drive the UI with the browser
tools, capture evidence, then move to PM UAT or back to Need Revision.

## Agent templates

The eleven role agents also exist as built-in **agent templates** — read-only
snapshots of an agent's fields, skills and rules that you can copy from when
creating a new agent. Opening **New agent** in the sidebar offers "From
scratch" or a list of templates (built-in ones are marked accordingly); picking
one copies the role's prompt, skills and rules onto a brand-new agent that you
can then rename and re-tune without touching the original. See
[Your own agents](custom-agents.md) for the full creation flow, including
saving any agent — including one you have customized — back out as a
template of your own.

> Creating agents by hand — along with editing the board, roles and workflows —
> is **hidden in the current shipping build** and re-enabled only for advanced
> setups; the feature is planned to return. The templates themselves still back
> the seeded role agents.

## Seeding and what survives an upgrade

Role agents do not come from a one-time boot seed; they arrive through the
**catalog sync**. At boot — and again on a fixed interval, and whenever you
press the button on the **Catalog** page — the server reads the role agents
from the bundled agent catalog and creates or merges each one into your board
**by name**. (In development the catalog is the repo-root `catalog/`; in a
packaged build it is a copy shipped inside the app, which `AGENT_CATALOG_REPO`
points at.) This is also how an upgrade reaches an install that already has
these agents, and it draws a firm line between what it may overwrite and what
it must not:

- **Adoption is name-based, and your edits park the sync.** While an agent's
  content still matches the catalog's definition it is stamped with the catalog
  slug and an **etag** and left on `auto_pull_agent_updates`, so the next sync
  keeps it current on its own. The moment you hand-edit that agent,
  `auto_pull_agent_updates` is switched off and the etag cleared: the next sync
  no longer overwrites it — it surfaces the upstream change as a **parked
  update** for you to review, rather than silently diverging.
- **Skills, tech stacks and KPIs reconcile either way**, without touching a
  byte of hand-written content. A shipped skill or rule whose text, category or
  enabled flag changed between versions is picked up on the next sync; one
  renamed or retired upstream is removed.
- **Tool policy and effort are applied by the sync from the catalog**, and
  afterwards belong to the agent row like any other field — so once an agent is
  parked (hand-edited), your tool-policy and effort changes are never quietly
  reset.
- **The system prompt, description and subagent type reconcile** the same way,
  so a rewritten role prompt in a newer release reaches an agent you never
  touched.
- **Models are filled in, never overwritten.** An agent's model and heavy model
  come from its provider's defaults — empty on the host CLIs (the CLI picks its
  own), the `sonnet`/`opus` pair on the Anthropic HTTP provider — and only
  while both are still empty; once you set either field yourself, the sync
  leaves it alone.
- **Column subscriptions are seeded once** and never re-applied if the agent
  already has any subscription, so re-pointing an agent at a different column is
  never undone.

In short: whatever you can see and change in the UI (tool policy, effort,
subscriptions, models once set) is yours to keep; the skill, rule and prompt
content is kept in sync with what TaskTrooper ships — unless you have edited
that agent, in which case the change waits for you as a parked update.
