---
title: Your first task
description: A walkthrough of creating a task, watching an agent work it, and moving it through the board.
---

This walks through the whole loop once: create a task, drop it on the board,
watch an agent take it, and follow it to done.

## Create a task

From the board, open **New task**. The dialog asks for:

| Field | What it's for |
|---|---|
| Title | Short summary shown on the card |
| Type | `task`, `analiz` (analysis) or `bug` — see [Tasks, criteria and documents](tasks.md) for what each means |
| Priority | `low` / `medium` / `high` / `critical` |
| Column | Where it starts (defaults to Backlog) |
| Project | Which initiative this belongs to |
| Assignee | The agent responsible. Setting this is what makes the task dispatch automatically |
| Description | The product-level story, context and out-of-scope notes |
| Technical description | Affected endpoints/files/schema, approach, constraints |
| Acceptance criteria | One observable Given/When/Then per item — the checklist QA runs and verifies |
| Documents | Free-form attached documents (this is where an `analiz` task's spec and plan live) |
| Attachments | Images or files |
| Deploy (collapsed by default) | Deploys after (which tasks must ship first), pre-deploy checklist, post-deploy steps, rollback plan |

Keep description, technical description and acceptance criteria in their own
fields rather than folding one into another — QA reads the
acceptance criteria specifically, and criteria pasted into the description
never become a checklist anyone verifies.

## Put it in Todo

A task sitting in **Backlog** is not yet on the board — nothing dispatches
work there. The moment a task lands in **Todo** (or any working column), the
column's agent is dispatched to it automatically. You don't press a run
button; the board does that the instant the card is somewhere an agent
watches.

If the task names other tasks in `blocked_by`, it won't start yet — it parks
on **Blocked** with a note naming what it's waiting on, and picks up on its
own once those finish. See [Ordering and relations](ordering-and-relations.md).

## Watch the run

Open the task (click the card) to see its **Agent Runs** section and the live
activity stream for the run in progress. This shows the agent's tool calls
as they happen — reading files, running commands, editing code — not just a
spinner. If the run stalls or you want to try again, the run can be stopped
or re-run from there:

- **Stop** ends a running or pending run and parks the task on Blocked with
  the reason; dragging the card onto a column afterward clears that and lets
  normal dispatch pick it up again.
- **Re-run** queues a fresh run of the same agent against the task's current
  state without moving the column — useful when a run failed for a reason
  that's now fixed and you don't want to touch where the card sits.

Both act on that one run and leave the task's history intact — cancelling or
re-running is recorded as its own event, not rewritten over what already
happened.

The activity stream itself reflects the run's real status rather than just
whether the stream is still receiving events — a run that was stopped or
crashed shows as interrupted even if its last recorded step looks like it
was still in progress, instead of appearing to hang forever.

## What the agent does

For a task with a repository attached, a board run:

1. Clones (or reuses) the repository into its own workspace and checks out a
   branch named after the task's key, e.g. `feature/t-12`.
2. Does the work — reads and edits code, runs the repository's own build/test
   commands, fixes what it broke.
3. Commits and pushes the branch, and opens a pull request (ready for review,
   not a draft) once the task reaches Human UAT or Done.
4. Ticks off acceptance criteria as it satisfies them.
5. Moves the card on to the next column when it's done with its part.

Entering **Ready for QA** triggers an automatic build/test pipeline against
the workspace before QA is dispatched; entering **Code Review** does the
same before the reviewing agent is woken. See [Quality
gates](quality-gates.md) for exactly what that pipeline checks and what
happens when a repository has none configured.

## How it moves through the columns

A typical `task`/`bug` flows:

```
Backlog → Todo → In Progress → Code Review → Ready for QA → In QA
   → Need Revision (on a reject) → Human UAT (optional) → Done → Released
```

An `analiz` (analysis) task instead goes:

```
Backlog → Todo → In Progress → Analiz Review → Done → Released
```

— its deliverable is documents, not code, and a human approves the plan in
Analiz Review rather than a QA chain. See [Board and
columns](board-and-columns.md) for the full column list and who owns each
one, and [Tasks, criteria and documents](tasks.md) for what an `analiz`
task's approval actually produces.

## Where to find things on the task

Opening a task's detail view shows, in order:

- **Acceptance criteria**, with a count, and each criterion's QA verdict
  once reviewed.
- **Deploy** — the deploys-after list, pre-deploy checklist, post-deploy
  steps and rollback plan, if any are set.
- **Documents**, with a count — an `analiz` task's spec and plan live here,
  never as a file committed to the repository.
- **Agent Runs**, with a count — every run this task has had, and their
  outcomes.
- **Pull Request** — once one is open, its number, state and merge status.
- **Comments**, with a count — where an agent explains a rejection, a
  blocker, or an error; the column and the criteria already say everything
  that went smoothly, so successful runs don't add a comment.

## Editing a task after creation

Every field from the create dialog stays editable from the task's detail
view — title, type, priority, description, technical description, and the
deploy runbook fields. Acceptance criteria can be added, ticked, or cancelled
with a reason at any point; a cancelled criterion is dropped from scope
rather than treated as met, so the checklist never ends up asking QA to
verify work that was deliberately never built.

## Sending a task back to Need Revision

If QA rejects a criterion, the task moves to **Need Revision**
itself as part of recording that verdict — you don't need to do it by hand.
You can also drag the card there yourself, or use the column selector inside
the task's detail view, if you've reviewed something outside the normal
chain and want a developer to look again.

## Chatting with the agent about a task

Every task has its own **Discuss** button, which opens (or reopens) a chat
thread scoped to that one task. Unlike an ordinary agent chat, this thread
works directly in the task's own branch checkout — so a change you ask for
there can be committed and pushed straight to its pull request — and every
message carries the task's key, title, column, description and acceptance
criteria as context, plus tools to read the PR, commit changes and reply to
review comments. It's the same thread an agent's own question lands in when
it asks for [clarification](quality-gates.md#clarification), so answering
there also unblocks a parked run.
