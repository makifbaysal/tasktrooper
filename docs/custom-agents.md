---
title: Your own agents
description: Creating an agent from a template or from scratch, every field on it, and what happens to its tasks when you delete it.
---

Besides the eleven seeded [role agents](role-agents.md), you can create as many
of your own agents as you like — a second QA agent for a different
repository, a technical writer, an agent that runs on a different CLI or a
different model entirely.

> **Currently hidden in the shipping build.** Creating hand-made agents — along
> with editing the board, roles and workflows — is turned off in the current
> release and re-enabled only for advanced setups; the feature is planned to
> return, and the pages and dialogs described below stay in place for when it
> does. The seeded role agents are unaffected.

## Creating an agent

Click the **+** next to **Agent Chats** in the sidebar. The dialog offers:

- **From scratch** — an empty agent; you fill in everything below.
- **From template** — a list of templates, built-in ones (the eleven roles) and
  any you have saved yourself, each showing its name, description, and how
  many skills and rules it carries. Picking one copies those fields, skills
  and rules onto a new agent; editing the new agent never changes the
  template it came from.

Either path lands you on the new agent's **Settings** tab to finish setting
it up, then **Save**.

## Fields on the Settings tab

| Field | What it controls |
|---|---|
| Name | Shown everywhere the agent appears — sidebar, board cards, chat |
| Description | Short summary, shown next to the name in a few places |
| Provider | Which runtime this agent's sessions run on — see [Runtimes](runtimes.md) |
| Model | The model for ordinary turns and subtasks. On Claude Code (and the other CLIs) this can be left empty to use the CLI's own default; on an HTTP provider it is required |
| Model (heavy) | An optional stronger model the executor escalates to for subtasks the planner rates "hard", and for self-reflection and the golden-gate judge. Independent of Model — leave it empty to never escalate |
| Subagent type | A label carried on the agent record (`generalPurpose`, `backend-engineer`, `system-architect`, and so on); it does not change what the agent can do, only how it is categorized |
| System prompt | The agent's core instructions, on top of its skills and rules |
| Tool policy | The allow-list of tools and MCP servers this agent may use — see [Tool policies](tool-policies.md) |
| Enabled | An agent that is off is never dispatched, but keeps its skills, rules, memories and history |

A model you type in that the selected provider does not list is flagged, not
rejected outright — on the CLI providers the curated list is a shortlist, not
a hard contract, since the CLI itself accepts any `--model` value. On an HTTP
provider a name outside its catalog is a real mismatch and is marked as an
error.

The Provider dropdown lists every provider that is actually usable right now:
the four local-process CLIs (Claude Code, Cursor, Antigravity, OpenCode) that
are available whenever their binary is on PATH, plus any OpenAI-compatible
endpoint you have connected on the **LLM Connection** settings page (OpenAI,
Anthropic, Google Gemini, Groq, or a custom endpoint such as LM Studio,
Ollama or vLLM). A provider that TaskTrooper knows about but cannot execute
on this build is never offered here, even though it may still be listed
elsewhere as "coming soon".

If the agent's tool policy allows any of the browser or mobile tools (or has
no restriction at all, which allows everything), the form warns when the
chosen model has no vision — those tools return screenshots, and a
text-only model cannot judge a screenshot it cannot see.

Two fields exist on every agent but are not yet exposed on this screen, so
they are set through the admin API rather than a form control:

- **Effort** (`low`/`medium`/`high`/`xhigh`/`max`) — the CLI effort level a
  session runs at. Empty uses the CLI's own default.
- **Max turns** — a per-agent turn ceiling for one Claude Code session. `0`
  uses the executor's own default.

## Columns

The **Columns** tab lists every non-backlog board column with a checkbox.
Checking one subscribes the agent to it, so a task landing in that column is
dispatched to the agent even when it is not the task's assignee — this is how
the seeded QA agent gets woken by "Ready for QA" without being anyone's
assignee. Leave a custom agent unsubscribed if it should only ever work a
task it is explicitly assigned.

## Skills, rules, and tech stacks

The **Skills** tab lists the agent's skills grouped into sections: a
**General** section for skills that apply regardless of language or
framework, plus one section per **tech stack** you add with "Add Tech Stack"
(name + description, e.g. "Go" or "React"). Filing a skill under a stack is
one field on the skill's own form (or "General" to leave it unscoped);
deleting a stack does not delete its skills, it just files them back under
General.

The **Rules** tab lists the agent's rules with name, priority, status and
created date; a rule's content and priority (higher runs first when rules
would conflict) are edited the same way skills are — see
[Skills and rules](skills-and-rules.md) for what goes in each and the budgets
that apply.

## Memory

The **Memory** tab (per agent) and the sidebar's "Team Memory" link (shared
across every agent) both open the same memory manager: a scope filter, a list
of saved memories with their scope badge, and forms to add, edit or delete
one. See [Memory](memory.md) for the four scopes and what content is refused.

## Saving an agent as a template

`POST /admin/agents/:id/template` snapshots an agent's fields plus its
current skills and rules into a template with the same name (calling it again
on the same agent updates that template rather than creating a second one).
This is available through the admin API today; use it to turn a
hand-tuned agent — your own QA setup for a particular stack, say — into
something you or a teammate can spin up again with "From template" without
retyping the prompt, skills and rules by hand.

## Mixing runtimes on one board

Nothing requires every agent on a board to run the same way. One agent's
Provider can be Claude Code, another's Cursor, another's a plain API key on
Anthropic or OpenAI, and they all work the same board: each task is dispatched
to whichever agent is assigned or subscribed, and that agent's own Provider
and Model decide how its session actually runs. See
[Agent CLIs and API providers](runtimes.md) for what each provider needs.

This is a reasonable way to, for example, keep your subscription-backed
Claude Code sessions for the roles that write the most code, and put a
lighter, cheaper API model on an agent that only triages incoming bugs.

## Deleting an agent

The delete button lives on the agent's own layout, next to its name (not on
the seeded role agents alone — any agent can be deleted). Deleting it:

- Removes the agent's skills, rules, memories, reflections and chat sessions
  with it — they belong to the agent and do not outlive it.
- **Does not delete its tasks.** Every board task the agent was assigned to
  has its assignee cleared instead, so the task stays exactly where it is on
  the board, unassigned, for you to hand to someone else.
- Does not affect any template saved from it, or any other agent created from
  that template.

There is no undo — re-creating an agent with the same name does not restore
its skills, rules or memory history.

## A worked example

Say you want a second QA agent dedicated to a mobile app repository, running
on a cheaper API model instead of your Claude Code subscription:

1. Open the **+** next to Agent Chats, pick **From template**, and choose the
   built-in `qa-agent` template. This copies its manual-testing skills and
   rules onto a new agent.
2. Rename it (e.g. "mobile-qa") and switch Provider to an API endpoint you
   have already connected on the LLM Connection settings page; pick a model
   that supports vision, since QA relies on screenshots.
3. On **Columns**, subscribe it to Ready for QA for the mobile repository's
   board (it moves a task into In QA itself once it claims it; Done belongs to
   the release engineer), and leave the original `qa-agent` subscribed
   only to the repositories it should still cover — subscriptions are
   per-agent, not per-repository, so this only works cleanly when each
   repository's board is otherwise handled by a different agent for that
   column.
4. On **Skills**, add a "Flutter" tech stack if this repository's QA notes are
   framework-specific, and file any new skill you write under it.
5. Save. The new agent now works exactly like a role agent from the outside —
   it appears on cards, can be reassigned tasks, and builds up its own memory
   and performance history independently of the one you copied it from.
