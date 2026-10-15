---
title: Skills and rules
description: What a skill and a rule are, how they differ, the budgets that apply, and how to write a good one.
---

Every agent carries two kinds of instructions beyond its system prompt:
**skills**, which it loads on demand, and **rules**, which are always in its
prompt. Both live on the agent's own **Skills** and **Rules** tabs — there is
no shared, global catalog. A skill or a rule belongs to exactly one agent.

## Skills: loaded on demand

A skill is a markdown document — name, description, category, optional tags,
and content — that is **not** injected into the prompt in full. Instead, the
system prompt carries an index of every enabled skill (just its name and
description), and the agent fetches the full content only when it decides it
needs it, by calling the `load_skill` tool. This is why a role agent can ship
with two dozen skills without bloating every single run's prompt: the index
costs a line per skill, and the content is paid for only on the runs that use
it.

Skills are also embedded and searchable: the planner and the semantic skill
search can surface a relevant skill by meaning, not just by name, when
building a plan or dispatching a subtask.

## Rules: always in the prompt

A rule is name, content, a **priority**, and an enabled flag. Every enabled
rule an agent has is injected into its prompt on every run, in priority
order (higher first) — there is no on-demand loading, because a rule is
meant to be a standing constraint the agent should never have to go looking
for. This is also why the rule budget (below) is much tighter than the skill
budget: unlike a skill, every rule you add is a permanent line in every
future prompt for that agent.

## Agent-scoped, not global

Both skills and rules belong to one agent via a required `agent_id`; there is
no `/admin/skills` or `/admin/orchestrator-rules` endpoint any more; both live
under `/admin/agents/:id/skills` and `/admin/agents/:id/rules`. The upside is
that two agents can define a rule with the same name and completely different
content — a `manual-only-testing` rule means something specific to QA and
nothing to a backend developer — without either colliding with the other.

The [seeded role agents](role-agents.md) each ship with a curated set (15 to
33 skills, 11 to 22 rules depending on the role) built for that role's job;
custom agents start with whatever a template gave them, or nothing at all
from scratch.

## Tech stacks

A skill can optionally be filed under a **tech stack** — a technology this
agent's own skills target, like "Go", "Java" or "React" — via the skill's
`tech_stack_id`. A skill with no stack is **general**: it holds regardless of
which language or framework the surrounding code is written in. On the
Skills tab this shows up as sections: a "General" section plus one section
per stack you have added with "Add Tech Stack". Deleting a stack does not
delete the skills filed under it — they fall back to General, since a
skill's usefulness does not disappear just because you removed the label for
it.

Tech stacks are created per agent, the same as skills and rules; a stack
belonging to one agent cannot be assigned to another agent's skill.

## Budgets

Self-evolution (see [Self-evolution and KPIs](self-evolution.md)) can propose
new skills and rules on its own, so both are capped per agent to keep the set
from growing without bound:

| | Default budget | What happens at the cap |
|---|---|---|
| Skills | 100 per agent | A `create` is rejected; the reflection is nudged to merge/update an existing skill instead, and a `create` naming an existing skill's name is silently converted into an `update` of it |
| Rules | 15 per agent | Same behavior |

The cap applies equally to an agent's own `create_skill` tool call, not just
to self-evolution — an agent cannot create its way around the budget from
inside a run either. Both numbers are configurable
(`evolution.max_skills_per_agent`, `evolution.max_rules_per_agent`) if you
want a different ceiling.

## Versions and restore

Every write to a skill or a rule — create, update, delete, or restore — is
appended to a version history, tagged with its **source**: `user` (you, from
the UI or the API), `evolution` (a reflection changed it, with a link back to
that reflection), or `seed` (the built-in content shipped with TaskTrooper).
Deleted content is stored too, so nothing here is ever actually gone.

`GET /admin/agents/:id/skills/:skillId/versions` (and the equivalent for
rules) lists that history; `POST .../restore` with a version number brings
that version back as a **new** version — restoring never rewrites history,
it just adds to it. This is what makes it safe to let self-evolution touch
skills and rules at all: if a change turns out to be wrong, or the
[golden gate](self-evolution.md#golden-gate) didn't already catch it, you can
always get back what was there before.

## Disabled seeded skills

A skill or rule can be shipped but **disabled** — the content and its seed
entry exist, but the enabled flag is off, so it is never injected into the
prompt and never appears in the skill index. The QA agent's seeded
automation skills (`e2e-automation-project`, `automation-pipeline-integration`,
`test-doubles-wiremock`, `test-database-seeding`) and two matching rules are
shipped this way today: QA's current flow is manual-only, and the automation
phase is deferred rather than deleted. Turning one back on is one toggle on
the Skills or Rules tab (the `Enabled` switch on that skill's or rule's edit
form) — no migration, no re-seeding, nothing to write by hand.

## How to write a good skill

A skill earns its place in the index by being findable and worth the extra
tool call to load. In practice that means:

- **A description that says when to use it**, not just what it covers — the
  description is the only part of the skill the agent sees before deciding
  whether to load it, since the content itself is not in the prompt until
  `load_skill` is called.
- **One concern per skill.** A skill mixing "how to write a migration" with
  "how to write a REST handler" is two skills wearing one name; splitting them
  lets the agent load only the one it actually needs for a given task, and
  lets you tag one under a tech stack while leaving the other general.
- **Concrete enough to act on.** A skill that repeats what a well-named
  function already says is a skill that will never change the run's outcome.
  The seeded skills lean toward specific patterns, conventions and gotchas
  for this codebase — the shape a review comment takes, which test library to
  reach for, how a particular workflow is supposed to look — rather than
  general advice a model already knows.
- **Tag it under a tech stack when it only applies to one.** A skill about
  Mockery's typed `EXPECT()` API is Go-specific; leaving it general would
  have it surface (uselessly) for a Java subtask on the same agent.

## How to write a good rule

Because a rule is always in the prompt, the bar is higher than for a skill:

- **Keep it short.** Every rule you add is a permanent line in every future
  prompt for that agent — a rule that reads like a skill's worth of prose is
  a skill that should not have been made a rule.
- **Give it a priority that reflects how absolute it is.** The seeded rules
  use priority as a real ordering signal: something like "never modify
  production code" or "write a failing test first" sits at priority 90-100,
  while a stylistic preference like comment format sits lower. When rules
  would pull in different directions, the higher-priority one is meant to
  win.
- **State a constraint, not a workflow.** "Run the affected tests before
  marking a task complete" is a rule; a multi-step procedure for how to set up
  a test database belongs in a skill instead, where it can be loaded once and
  followed rather than repeated on every single prompt.

## Where the content actually comes from

For the seeded role agents, skill and rule content is authored as
markdown files in the server's source tree and synced into the database from
the agent catalog; upgrading TaskTrooper reconciles that content into your
existing installation the same way (see
[Seeding and what survives an upgrade](role-agents.md#seeding-and-what-survives-an-upgrade)).
For any agent you create yourself, everything is authored directly through
the Skills and Rules tabs — there is no file to edit.

