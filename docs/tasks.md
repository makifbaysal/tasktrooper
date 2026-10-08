---
title: Tasks, criteria and documents
description: Task types, fields, acceptance criteria, documents, comments, attachments, test cases and task keys.
---

## Task types

Every task belongs to a repository and, optionally, a project (an initiative
that groups the repositories shipping together). It is also one of three
types:

| Type | Key prefix | What it is |
|---|---|---|
| `task` | `T-` | Ordinary product work |
| `bug` | `B-` | A defect |
| `analiz` | `A-` | An analysis |
| `design` | `D-` | A design: a design system or a screen, designed before it is built |

`task` and `bug` follow the same review chain (Code Review → QA → PM UAT).
An **`analiz`** task is different in kind, not just in name: its deliverable
is a set of **documents** attached to the task itself — a spec and an
implementation plan — never a commit to the repository, because an analysis
produces no diff. It's reviewed in the **Analiz Review** column, where a
human reads the plan and either approves it (the architect then opens the
implementation tasks the analysis called for) or rejects it (back to Need
Revision for the architect to rework). Each implementation task opened from
an approved analysis names that analysis with a `derived_from` relation — see
[Ordering and relations](ordering-and-relations.md) — which is how the
developer picking it up reaches the specification: `list_task_documents`
re-reads it at any point, and it's injected into every run on that task
automatically.

A **`design`** task works the same way, owned by the `ui-designer` agent: its
deliverable is an HTML design document (mockups of every state, or a design
system preview) and, when it changes the design system, a design system
proposal. You review it in **Analiz Review**; approving it approves the
design system versions it proposed, and the frontend and mobile tasks that
are `derived_from` it, or blocked by it, get the approved design in every run
and under the task. Work blocked by a `design` task starts only once that task
is **released**, after its designer has written the hand-off. See
[Design systems](design-systems.md).

## Fields

| Field | Holds |
|---|---|
| `title` | Short summary |
| `description` | Product-level story: the user story, context, out-of-scope notes (markdown) |
| `technical_description` | Technical detail: affected endpoints/files/schema, approach, constraints (markdown) |
| `acceptance_criteria` | The checklist QA executes and PM UAT verifies — see below |
| `priority` | `low` / `medium` / `high` / `critical` |
| `assignee` | The agent responsible; setting it is what makes a task in a working column dispatch automatically |
| `repository` | Which codebase the work touches |
| `project` | Which initiative (a group of repositories that ship together) the task belongs to |
| `before_deploy` | Pre-deploy checklist (markdown) — posted as a comment automatically when a release is dispatched |
| `after_deploy` | Post-deploy steps (markdown) — posted automatically once the production deploy succeeds |
| `rollback_plan` | How to undo the change if production breaks (markdown) — posted alongside the pre-deploy checklist |

Keep description, technical description and acceptance criteria in their own
fields. They're read for different purposes — a reviewer wants the technical
detail, QA and PM UAT read only the criteria — and content pasted into the
wrong one is content those readers never see.

`before_deploy` also carries a block TaskTrooper generates and keeps in sync
on its own, fenced between `<!-- tt:order -->` markers, naming what this task
ships after and what it's built after (from its relations — see [Ordering
and relations](ordering-and-relations.md)). Anything you write outside that
fence is yours and survives regeneration.

## Acceptance criteria and verdicts

Each criterion is one observable Given/When/Then statement. It has three
possible states:

- **Open** — not yet settled; this is what holds up a hand-off.
- **Completed** — the implementer's own claim that it's satisfied.
- **Canceled** — deliberately dropped from scope, with a reason recorded and
  posted as a comment. A cancelled criterion counts as settled but is never
  treated as met.

Completed is a claim, not a verdict. QA and PM UAT each record their own
independent **verdict** on every criterion — approve or reject, with a
mandatory note on a reject — and a criterion isn't truly accepted until every
role that reviews it has signed off. The role is derived from which column
the task is in when the verdict is recorded, never from what the agent calls
itself. See [Quality gates](quality-gates.md) for exactly when this is
enforced.

## Task documents

Free-form markdown documents attached to a task, in position order — the
only place an `analiz` task's spec and plan live. Any task can carry
documents, not just analyses; they're a place to keep material too long or
too structured for a comment.

## Comments

A comment is for something that needs attention: a rejection and its reason,
an error, a blocker, an unanswerable question, work that wasn't done.
Everything that went smoothly is already visible in the column, the
criteria, the pull request link and the run history, so a clean run doesn't
add a comment repeating what those already say.

## Attachments

Images or documents (up to 10 MB, common file types) linked to the task.
They're for reference — screenshots, specs, mockups — and are not fed into
an agent's context automatically the way task documents are.

## Test cases

QA's test round lives on the card as its own list, separate from the
acceptance criteria. Each case has a title, a category (happy path,
boundary, negative, auth, empty state, regression, visual, async, other), a
status (planned, passed, failed, skipped, invalid), expected/actual results,
evidence and notes, and optionally links back to the acceptance criterion it
covers. A task can't move forward out of Ready for QA / In QA with no cases
recorded, or with any case still `planned`.

## Editing fields

Every text field follows the same rule when you update a task: leaving a
field out of the update leaves its stored value alone, sending an explicit
empty value clears it. The one exception is the assignee, which reads three
ways — omitted leaves whoever is on the card, an explicit "unassign" clears
it, and a value assigns that agent — because a plain empty string and "don't
change this" needed to mean different things once the UI grew a button for
each.

## Runs history

Every headless agent run a task has had — which agent, when, what it did,
how it ended — is listed on the task, each with its own activity stream you
can open to see the tool calls that made it up. A run can be stopped or
re-run from there without moving the card.

## Task keys

Every task gets a short key — `T-1`, `B-1`, `A-1` — built from its type's
letter prefix and a number that counts up on its own for that type, so the
three sequences never interleave (the first bug is always `B-1`, regardless
of how many tasks or analyses came before it). The key is what you'll see
quoted in chat, in a commit trailer, or in a branch name (`feature/t-12`),
and it's stable for the task's whole life — reading a key back always tells
you the type without opening the task. Numbers are never reused, even if a
task is deleted, so a key always points at exactly one task for as long as
the board exists.

## Deleting a task

A task can be deleted permanently, along with its comments, criteria,
documents and relations — there's no soft-delete state to recover from
later. It's restricted to a task still in Backlog or Todo; once work has
started elsewhere, deleting it is refused unless you explicitly force it,
and a reason can be recorded as the last trace once the row itself is gone.

This restriction exists for the same reason moving a task backward is
always allowed but deleting one further along is not: work already done —
a branch, a pull request, review history — is worth keeping a record of even
when the task itself was a mistake. Moving it to Need Revision or closing it
out with a comment explaining why preserves that history; deleting it does
not.
