---
title: Self-evolution and KPIs
description: How agents revise their own skills and rules from how work actually went, the safety net around it, and the targets that drive it.
---

Self-evolution lets an agent rewrite its own skills, rules and memories based
on how its work has actually gone — not on a fixed schedule of manual
tuning, but on real runs, real revisions, and real scores. It is off by
default per agent; turn it on from the **Enabled** switch on the agent's
Settings tab (`self_evolution_enabled`).

## Reflection: what triggers it, what it reads

A **reflection** is one pass where an agent reviews itself and proposes
changes. Three things can trigger one:

- **Periodic** — a ticker checks every enabled agent on a fixed interval
  (`evolution.tick_interval`, 10 minutes by default) and starts a reflection
  once at least `evolution.reflect_interval` (24 hours by default) has passed
  since its last one and the agent has done something since.
- **`need_revision`** — a task bouncing back to Need Revision schedules a
  smaller, debounced reflection (`evolution.revision_debounce`, 30 minutes
  minimum between these) so a string of related failures does not trigger a
  reflection per failure.
- **On demand** — the **Analyze now** button on the agent's **Performance**
  tab starts one immediately.

`evolution.enabled: false` turns all three off, the button included.

Each reflection is **incremental**: it only reviews the window since the
previous reflection, and compares against that reflection's own performance
snapshot rather than re-reviewing everything the agent has ever done. What it
reads for that window: the agent's chat messages, its task runs, any
revision comments, score events, KPI attainment, and its current skills
(with an excerpt of each one's text), rules and memories — plus a regression
report on any earlier change that turned out to hurt (see
[Impact tracking](#impact-tracking) below), a digest of how its recent runs
went step by step (the tool errors it hit and the last thing it said, runs
that were revised or failed first), and the [lessons](#lessons) earlier
tested changes taught.

With `evolution.allow_web_research` on, the reflection itself can use web
search and page fetch while it reasons, the same as any other agentic run,
and it gets the full evidence as files it can open: the report uncut, every
skill and rule in full, and each recent run step by step with the request and
the feedback it got.

## What it proposes

The reflection call is a strict JSON contract: a self-assessment, and lists
of proposed skill changes, rule changes, memory changes, and reverts of
earlier changes. **Skill and rule changes are only actually applied for
agents with self-evolution turned on** — an agent with the toggle off still
runs periodic reflections and records what it would have proposed (visible
in its reflection history), but nothing is written. Memory changes follow the
same per-reflection cap (`evolution.max_memory_changes`, 5 by default) and, as
covered in [Memory](memory.md), a reflection can only propose **global**
memories, never project-scoped ones.

Each skill or rule change is tagged with a **dimension**: *procedure* (how
the agent works — tool use, the order of steps, checks, how it finishes) or
*domain* (what a practitioner knows — requirements, conventions, the shape of
the deliverable). The two are tested separately, below.

Before anything is tried, the proposal is checked for the ways a change fails
at run time without anyone noticing:

- a skill's description must say **when** to load it — until a run loads a
  skill, that one line in its skill index is all it sees;
- a rule or skill that names another skill (`` the `db-migrations` skill ``)
  must name one that exists after the change, and deleting a skill may not
  leave a rule pointing at it;
- rules are in every run's context, so together they may not grow past
  `evolution.max_always_on_chars` (8000 characters by default) — procedure
  with a recognisable trigger belongs in a skill.

Whatever fails goes back to the reflection once, with the problems spelled
out; anything still wrong after that is dropped and shown as **rejected**.

## The probe gate

With `evolution.golden_gate` on (the shipped default), a skill or rule
change is **rehearsed before it is written** — nothing reaches the catalog
until it has done better than what the agent has now. This is the
Propose–Probe–Compose step from Google Research's
[Harness²](https://github.com/google-research/harness2), sized for a product.

- **Cases.** The agent's **golden tasks** (fixed prompts with expected
  phrases, edited on its Performance tab) plus `evolution.probe_cases` (2 by
  default) recent real requests: board tasks it worked on, revised ones
  first, then chat messages.
- **Variants.** When the proposal has both procedure and domain changes, each
  half is tried on its own, and all of it together; otherwise just the whole.
  Each variant is the agent's skills and rules as they *would* stand, built
  in memory.
- **Rehearsal.** The current setup and every variant answer every case on the
  agent's own engine — its HTTP model, or a short session on its CLI (Claude
  Code, OpenCode, …) in an empty directory with no TaskTrooper tools.
- **Scoring.** A golden task passes when the answer contains every expected
  phrase. The other cases are scored by an independent **judge model**
  (`evolution.judge_model` / `judge_provider_type`; the agent's heavy model
  when neither is set) that sees the answers under shuffled letters and never
  knows which one is the current setup.
- **Composition.** A variant is accepted when it does better on more cases
  than it does worse, and never makes a golden task that passed now fail. The
  whole set is adopted when it beats the best half (or ties it with both
  halves holding up on their own); otherwise the best half that held up;
  otherwise nothing.

Only the adopted variant is written. Every other change is shown as **not
adopted**, and a change set nothing could measure — no golden tasks and no
recent work, or an engine that could not run the rehearsal — writes nothing.
Each change also gets a verdict: **helped**, **hurt**, **no effect**, or
**confounded** when it was only ever tried together with other changes, so
its own effect is unknown.

The reflection's detail view shows the cases, how each variant did against
the current setup, and which one was adopted; the golden pass rate before and
after lands on its performance snapshot.

## Lessons

After a measured rehearsal, one more call turns the record into at most one
**lesson**: the shape of situation it applies to, the change to make to a
skill or rule, the evidence behind it and a confidence. It declines when the
record shows nothing reusable. The agent's next reflections read its five
most recent lessons, so an edit that already failed is not proposed again.

## Impact tracking

Even a change the probe adopted keeps being watched. After
`evolution.impact_window` (7 days by default) has passed, an applied change
is classified by comparing the agent's scores before and after it:
**effective**, **regressed**, **neutral**, or **insufficient_data** if there
were fewer than `evolution.min_events_for_impact` (3) events to compare
after. A regressed change is not reverted automatically, but it is surfaced to that agent's *next*
reflection, which decides for itself whether to revert it (restoring the
pre-change version and recording that as a `revert` event too).

## Budgets and version history

Skills and rules are capped per agent — 100 skills, 15 rules by default — so a
reflection cannot pile up indefinitely; once at budget, a `create` is
rejected in favor of merging into or updating something that already exists.
See [Skills and rules](skills-and-rules.md#budgets) for the full mechanics,
which apply identically whether the change came from you or from a
reflection.

Every write to a skill or rule — by you, by a reflection, or from the
built-in seed — is appended to a version history with its source recorded,
and any version can be restored. This is what makes the whole mechanism
survivable: a reflection's change that slips past the probe and the
impact check can still be rolled back by hand from the skill's or rule's own
version history.

## KPIs

Each agent can carry **KPIs** — targets measured per day, week, or month,
each with a **full** target, a **half** target, and a weight. A measured
value at or past the full target scores 1.0 attainment; at or past half,
0.5; otherwise 0. A weighted mean of every enabled KPI's attainment, times
100, is the agent's **KPI composite score**, shown at the top of its
**Performance** tab.

Only metrics in a fixed registry can be attached to a KPI (`GET
/v1/kpi-metrics` lists them): `tasks_completed`, `revisions_received`,
`uat_failures`, `failed_runs`, `gate_rejected_runs`, `bugs_assigned`,
`first_pass_rate`, `tool_error_rate`, plus a family of column-time metrics
(median hours spent in a particular column — In Progress, Code Review, In QA,
PM UAT — and a review-escape count). `gate_rejected_runs` counts runs the host
rejected for skipping required grounding — reading the repo, or running and
checking the product — and `tool_error_rate` is the share of an agent's tool
calls that came back as errors.

The seeded role agents each get a starting set matched to their job: the
developer roles get tasks completed, revisions received, first-pass rate, UAT
rejections, bugs assigned, clean cycle time in In Progress, and tool error
rate; QA gets tasks completed, UAT escapes, clean time in QA, gate-rejected
runs and tool error rate; the product manager gets tasks completed, clean time
in PM UAT, gate-rejected runs and tool error rate; the architect gets tasks
completed, revisions, first-pass rate, review time, analysis time, review
escapes, gate-rejected runs and tool error rate.

Two rules keep a KPI from being gamed by rushing: only tasks that reached
Done or Released **without ever bouncing through Need Revision** count
toward a cycle-time measurement, and a period with fewer than 3 qualifying
tasks is left unmeasured rather than scored as if it were instantaneous.

KPI targets are injected into the agent's own prompt as "your objective: meet
these KPIs" — so the agent is working toward the same numbers you see on its
Performance tab, not a private notion of doing well. Manage the list of KPIs
for an agent through `/admin/agents/:id/kpis`; each one shows on the
Performance tab as a card with its current attainment, measured value, and a
progress bar against its two thresholds.

## Golden task results

Each rehearsal of a golden task is recorded (`GET .../golden-results`) with a
pass/fail and a detail string, linked back to the reflection: once for the
current setup (`before`) and once for what was adopted (`after`, the current
setup again when nothing was). This is where to look to see exactly which
golden task a change broke or fixed.

## The Performance tab

Besides the KPI composite and per-KPI cards, an agent's **Performance** tab
shows a score trend, the **Analyze now** button (disabled while a reflection
is already running), the agent's **golden tasks** (add, edit, switch off),
and a list of past reflections with their status (running/completed/failed),
trigger, and timestamp. Opening one shows each change with its dimension,
verdict and outcome, the rehearsal results and the lesson. This is where you go to
see what self-evolution has actually done to an agent, and to trigger a
fresh look at it on demand instead of waiting for the next scheduled pass.
