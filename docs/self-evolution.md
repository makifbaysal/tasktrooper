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

- **Periodic** — a ticker checks every agent on a fixed interval
  (`evolution.tick_interval`, 10 minutes by default) and starts a reflection
  once at least `evolution.reflect_interval` (24 hours by default) has passed
  since its last one.
- **`need_revision`** — a task bouncing back to Need Revision schedules a
  smaller, debounced reflection (`evolution.revision_debounce`, 30 minutes
  minimum between these) so a string of related failures does not trigger a
  reflection per failure.
- **On demand** — the **Reflect Now** button on the agent's **Performance**
  tab starts one immediately.

Each reflection is **incremental**: it only reviews the window since the
previous reflection, and compares against that reflection's own performance
snapshot rather than re-reviewing everything the agent has ever done. What it
reads for that window: the agent's chat messages, its task runs, any
revision comments, score events, KPI attainment, and its current skills,
rules and memories — plus a regression report on any earlier change that
turned out to hurt (see [Impact tracking](#impact-tracking) below). With
`evolution.allow_web_research` on, the reflection itself can use web search
and page fetch while it reasons, the same as any other agentic run.

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

## The golden gate

With `evolution.golden_gate` on (the shipped default), a proposed skill or
rule change does not take effect unread. The agent's **golden tasks** — fixed
eval prompts with an expected-substring check, editable from
`GET/POST /admin/agents/:id/golden-tasks` — are replayed tool-less, once
against the skill/rule set as it stood *before* the change and once *after*
it. An independent **judge model** (`evolution.judge_model` /
`judge_provider_type`; the reflection's own model when neither is set) is
given the before/after pass rate, which golden task started failing, and the
list of changes, and returns a keep-or-revert verdict with a reason.

- If the pass rate simply dropped, the whole change set is reverted without
  even consulting the judge.
- If the judge is unreachable, the fallback is "keep unless it regressed" —
  the same rule, just without the judge's reasoning attached.
- A revert is **all-or-nothing**: every skill and rule change in that
  reflection's set is undone, in reverse order, as its own recorded event.

The outcome is written into the reflection's summary as a line like
`Golden gate: 80% → 60% | verdict: reverted`, and the before-rate is stored
on the reflection's performance snapshot too, so you can see the gate's
verdict without digging through raw events.

## Impact tracking

Even a change the golden gate let through keeps being watched. After
`evolution.impact_window` (7 days by default) has passed, an applied change
is classified by comparing the agent's scores before and after it:
**effective**, **regressed**, **neutral**, or **insufficient_data** if there
were fewer than `evolution.min_events_for_impact` (3) events to compare
after. A regressed change is not reverted automatically — the golden gate is
the only fully automatic revert — but it is surfaced to that agent's *next*
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
survivable: a reflection's change that slips past the golden gate and the
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

Each replay of a golden task — before or after a proposed change, on any
reflection — is recorded (`GET .../golden-results`) with a pass/fail and a
detail string, linked back to the reflection that triggered it when there is
one. This is the raw data the golden-gate summary line is computed from, if
you want to see exactly which golden task started failing rather than just
the pass rate that resulted.

## The Performance tab

Besides the KPI composite and per-KPI cards, an agent's **Performance** tab
shows a score trend, the **Reflect Now** button (disabled while a reflection
is already running), and a list of past reflections with their status
(running/completed/failed), trigger, and timestamp. This is where you go to
see what self-evolution has actually done to an agent, and to trigger a
fresh look at it on demand instead of waiting for the next scheduled pass.
