---
title: Role agents
description: The six agents every board is seeded with, what each one does, and what "seeding" and reconciliation mean when you upgrade.
---

When you set up a repository, TaskTrooper creates six agents for you: a
product manager, a system architect, a backend developer, a frontend
developer, a mobile developer and a QA agent. They show up in the sidebar
under **Agent Chats**, and each has its own **Settings**, **Skills**,
**Rules**, **Columns**, **Memory** and **Performance** tabs.

You do not have to use them as given — everything here can be edited from
those tabs or replaced with [your own agents](custom-agents.md) — but they are
built to run a board end to end without any setup beyond registering a
repository.

## The agents

| Agent | Runs as | Effort | What it does |
|---|---|---|---|
| `product-manager` | `generalPurpose` | medium | Backlog, requirements, stakeholder questions, board organization |
| `system-architect` | `system-architect` | high | Analysis tasks, task decomposition, code review |
| `backend-developer` | `backend-engineer` | high | Go/Fiber and Java/Quarkus APIs, database migrations, backend tests |
| `frontend-developer` | `frontend-engineer` | high | React/Vite/Tailwind UI |
| `mobile-developer` | `mobile-dev-engineer` | high | Flutter, SwiftUI, Jetpack Compose apps; store deploys |
| `data-scientist` | `data-scientist` | high | Data pipelines, SQL/dbt analytics, model training and evaluation, notebooks turned into tested modules |
| `game-developer` | `game-developer` | high | Unity, Godot, Unreal Engine, web (Phaser, PixiJS, three.js, Babylon.js) and Bevy games |
| `security-agent` | `security-reviewer` | high | Security-only review of every pull request in Code Review; blocks the ones that introduce an exploitable vulnerability |
| `qa-agent` | `generalPurpose` | medium | Manual test rounds against a running build |
| `ui-designer` | `generalPurpose` | high | The project's design system, screen designs before they are built, design reviews — see [Design systems](design-systems.md) |

"Runs as" is the **subagent type** shown on the agent's Settings tab — it is
metadata the CLI session carries, not a separate program. "Effort" is the CLI
effort level (`low`/`medium`/`high`/`xhigh`/`max`) the agent's sessions run
at; a checklist pass and a multi-file refactor do not want the same depth of
thinking, so the two engineering-heavy roles (system-architect and the three
developers) are seeded at `high` while the two roles that mostly read, write
and route (product-manager, qa-agent) run at `medium`.

Every seeded agent's provider is Claude Code and its models are `sonnet` for
ordinary work and `opus` for subtasks the planner rates "hard" (plus
self-reflection and the golden-gate judge, see [Self-evolution](self-evolution.md)).
That pairing only lands where the `claude` CLI is actually available on this
machine; if it isn't, the agents are still created but left without a
provider until one is configured on the Settings tab.

## Which columns each agent works

Agents are dispatched to a board column either because they are the task's
**assignee** or because they **subscribe** to that column. Three of the six
carry a subscription out of the box, so tasks flow to the right reviewer
automatically instead of bouncing back to whoever implemented the task:

| Agent | Subscribed columns | Why |
|---|---|---|
| `system-architect` | Code Review | Reviews the diff once a developer hands a task there |
| `security-agent` | Code Review | Reviews the same diff for security only |
| `qa-agent` | Ready for QA, In QA, Done | Picks the task up, tests it, and — once it is signed off — merges its pull request |
| `product-manager` | PM UAT | Reviews the finished task against its acceptance criteria |

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

Analysis Review and Human UAT have no subscriber on purpose: those are the
two columns where a human approves or rejects, not an agent. The
`ui-designer` subscribes to nothing either: every `design` task is assigned
to it, so it is dispatched as the assignee.

A task's **assignee** (a developer, typically) is dispatched when the task
sits in a column nobody subscribes to — Todo, In Progress, Need Revision — and
the assignment itself is made by the product manager or picked up by the
agent claiming the task. You can see and change which columns an agent is
subscribed to on its **Columns** tab.

## Skills, rules, and tool access

Each role agent ships with a curated set of markdown skills and orchestrator
rules — see [Skills and rules](skills-and-rules.md) for what those are. Counts
as seeded today:

| Agent | Skills | Rules |
|---|---|---|
| `product-manager` | 22 | 20 |
| `system-architect` | 13 | 9 |
| `backend-developer` | 27 (12 shared + 15 of its own) | 10 |
| `frontend-developer` | 21 (12 shared + 9 of its own) | 7 |
| `mobile-developer` | 25 (12 shared + 13 of its own) | 8 |
| `qa-agent` | 20 enabled + 4 disabled (3 shared + 21 of its own) | 16 enabled + 2 disabled |
| `ui-designer` | 9 (3 shared + 6 of its own) | 10 |

The three developer roles share a dozen skills (board comment style,
performance awareness, TDD workflow, incremental commits, root-cause
debugging, CI/CD authoring, deploy templates, incident response, and a few
more) on top of their own language- and platform-specific skills. Every skill
and rule belongs to exactly one agent — there is no global catalog — and you
can add, edit or delete any of them from that agent's **Skills** and **Rules**
tabs.

What each role's tools actually allow is covered in full on
[Tool policies](tool-policies.md); in short: the three developer roles and QA
get the terminal and file editors, the product manager and QA can create,
move and delete board tasks, and QA is deliberately kept away from the
code-reading tools — it tests the running product, not the source.

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

The six role agents also exist as built-in **agent templates** — read-only
snapshots of an agent's fields, skills and rules that you can copy from when
creating a new agent. Opening **New agent** in the sidebar offers "From
scratch" or a list of templates (built-in ones are marked accordingly); picking
one copies the role's prompt, skills and rules onto a brand-new agent that you
can then rename and re-tune without touching the original. See
[Your own agents](custom-agents.md) for the full creation flow, including
saving any agent — including one you have customized — back out as a
template of your own.

## Seeding and what survives an upgrade

"Seeding" is what happens the first time a repository's board comes up: the
server creates any of the six role agents that do not already exist by name,
and fills in any skills or rules a partially-created agent is still missing.
This also runs again on every server start (`EnsureRoleAgents`), which is how
upgrades reach installs that already have these agents — but it draws a firm
line between what is safe to overwrite and what is not:

- **Content is reconciled.** If a shipped skill's or rule's text, category, or
  enabled flag changes between versions, your existing installation picks up
  that change automatically on the next restart. A skill or rule renamed or
  retired between versions is deleted from existing installs too (it is
  listed as deprecated internally so the reconciler knows to remove it).
- **Tool policy and effort are set once, on creation, and never again.** If
  you have opened an agent's tool policy and added or removed a tool, or
  changed how it maps to your own workflow, that customization survives every
  restart and every upgrade. The one exception is additive: a tool introduced
  in a later version that pairs with a capability you already granted (for
  example, `cancel_criterion` once you already have `set_criterion_completed`)
  is added automatically — narrowing you already did on purpose is never
  undone, but a tool that simply didn't exist yet when you customized the
  policy still reaches you.
- **The agent's system prompt, description and subagent type are reconciled**
  the same way skill content is — so a rewritten role prompt in a newer
  release reaches an agent you never touched.
- **Models are filled in, never overwritten.** If an agent's model and heavy
  model are both still empty, the seed fills them with `sonnet`/`opus` on
  Claude Code — but only where that CLI can actually run on this host, and
  only if you never set either field yourself.
- **Column subscriptions are seeded once and never re-applied** if you already
  have any subscription on that agent — so re-pointing an agent at a different
  column is never quietly reset.

In short: whatever you can see and change in the UI (tool policy, effort,
subscriptions, models once set) is yours to keep; the underlying skill and
rule content is kept in sync with what TaskTrooper ships.
