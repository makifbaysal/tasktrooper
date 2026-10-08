---
title: Board and columns
description: The default column layout, which agent works each column, and how a card starts a run.
---

## The default columns

A new board comes with thirteen columns, in this order:

| # | Column | Slug | Default owner |
|---|---|---|---|
| — | Backlog | `backlog` | none — never dispatches |
| 1 | Todo | `todo` | the task's assignee |
| 2 | In Progress | `in_progress` | the task's assignee (a developer role) |
| 3 | Analiz Review | `analiz_review` | none — a human review gate |
| 4 | Code Review | `code_review` | `system-architect`, `security-agent` (every subscriber is a required reviewer) |
| 5 | Ready for QA | `ready_for_qa` | `qa-agent` (queue — see below) |
| 6 | In QA | `in_qa` | `qa-agent` (testing happens here) |
| 7 | Need Revision | `need_revision` | the task's assignee |
| 8 | PM UAT | `pm_uat` | `product-manager` |
| 9 | Human UAT | `human_uat` | none — a human approval gate |
| 10 | Blocked | `blocked` | none — never dispatches |
| 11 | Done | `done` | `qa-agent` (merges the PR) |
| 12 | Released | `released` | `qa-agent` (watches the deploy) |

Backlog is fixed and always first; everything else is yours to reorder,
rename or remove. `analiz_review` and `human_uat` are deliberately unowned —
they're where a human, not an agent, is expected to act.

## Which agent owns which column

The six seeded role agents map onto the columns like this by default:

| Agent | Columns |
|---|---|
| `product-manager` | Backlog (requirements), PM UAT |
| `system-architect` | Analysis (`analiz`) tasks in In Progress, Code Review |
| `backend-developer` / `frontend-developer` / `mobile-developer` | In Progress, Need Revision (for their own tasks) |
| `qa-agent` | Ready for QA, In QA, Done (merges the pull request), Released (watches the deploy and can roll it back) |

A column can have several subscribers — every one of them is dispatched when
a card lands there. A task moving into a column with no subscriber and no
assignee (Analiz Review, Human UAT) simply waits for a person.

## How dispatch works

A card entering a column starts that column's agent automatically — nobody
presses a run button. The rule, precisely:

- **`task.created`** in column X, or **`task.moved`** to column X, dispatches
  every agent subscribed to X.
- **`task.commented`** dispatches the assignee, unless the task is in
  `need_revision` (assignee only there too) — or, in a column a human owns
  the hand-off through (Code Review, In QA, PM UAT, Human UAT), the agent
  actually holding the task rather than the original assignee.
- **`task.assigned`** dispatches the new assignee.

**Backlog and Blocked never dispatch**, on purpose:

- **Backlog** is "not yet taken onto the board" — a task can sit there
  fully described with an assignee set and nothing will start until it's
  moved onto a working column.
- **Blocked** is where a task waits for something outside anyone's control
  to resolve (see below); dispatching it would just restart the same run
  that just stopped.

## The review chain, by task type

Two of a repository's optional settings check that a task actually passed
through the stages its type requires before it can be called Done or
Released:

| Task type | Required stages |
|---|---|
| `task` / `bug` | Code Review, In QA, PM UAT |
| `analiz` | Analiz Review only |

Evidence is whether the task's history ever shows it visiting that column —
not the column it happens to be in right now — so a task that bounced
through Need Revision and came back is never punished for the rework; it
just re-earns the stage. This check (**require review chain**) and its
sibling (**require release deploy**, which blocks Released without a
recorded successful production deploy) are both off by default and armed
per repository, because each is only honest on a board actually wired for
it — see [Quality gates](quality-gates.md) for the rest of what gates a
task's move through review.

## Human review

A repository can also be set so that a review agent's approval is held for a
human rather than acting immediately: the reviewing agent still runs and
still records its verdict, but a human has to move the card the rest of the
way. When a human later rejects something the agent had approved, that's
recorded against the agent — it's meant to keep the review honest, not to
remove the agent from the loop. With this off (the default), the agent's own
approval is what advances the card.

Human UAT is a step further again: it has no agent subscriber at all, by
design, whether or not human review is turned on elsewhere — it's a pure
human approval column, and a repository that doesn't use it can simply
remove it from its column set.

## Reviewing an analysis

An `analiz` task ends with one analysis report: a single HTML page the
architect attaches to the task, with the design and the implementation plan
as sections of it. When the architect's run ends with the report attached,
the card moves to Analiz Review on its own and waits for you.

In the task drawer, **Review analysis** (under the approve/decline buttons)
opens the report. Select any passage and add a comment on it; your comments
stay drafts — you can edit or delete them — until you send them. Then:

- **Approve** — move the card to Done. The architect creates the
  implementation tasks, each linked back to the analysis.
- **Request changes** — send every draft comment at once. The card moves to
  Need Revision (the same move as declining it by hand, so it counts as a
  rejection of the architect's work), and one comment on the card lists what
  you sent. The architect's revision run gets every comment with the passage
  it's about, revises the same report rather than attaching a new one, and
  answers each comment with a one-line reply. The card then comes back to
  Analiz Review; answered comments show as resolved with the reply next to
  them, and you can reopen one that wasn't fixed.

Sending a review only works while the card is in Analiz Review and has at
least one unsent comment. The report is sanitized when it's saved — scripts,
embedded frames, forms and external stylesheets are removed — and is limited
to 1 MB.

## Configuring columns and owners

- **Settings → Board Workflow** — add, remove, rename or reorder columns,
  and edit the allowed transitions between them. Backlog can't be removed.
- **An agent's Columns tab** — which columns that agent is subscribed to,
  and (optionally) which task types it should be dispatched for in each one.

Both changes take effect immediately; there's no restart involved. A
column's subscription can also be narrowed to specific task types — for
example, subscribing an agent to a column only for `bug` tasks — so one
column can hand off to different agents depending on what kind of task
lands there.

## The Blocked column

A task on Blocked isn't stuck by accident — it's parked deliberately because
nothing productive can happen until a specific condition clears, and the
card names which one:

| Reason | What it's waiting on | How it clears |
|---|---|---|
| Clarification question | An answer to a question the agent asked | You answer in the task's chat thread; the task resumes immediately |
| Usage limit | The Claude Code subscription's usage window resetting | Checked every minute; the card shows the expected resume time and comes back on its own |
| Work order | Other tasks named in this one's `blocked_by` finishing | Checked every minute against the relation graph; releases the moment every blocker reaches Done/Released (or is deleted) |
| Shared device | A mobile simulator/emulator or physical phone freeing up | Checked periodically against the device pool; releases when one becomes free |

Two further conditions can also park a task on Blocked: a pending production
deploy settling (checked against GitHub, released automatically), and a
board loop the system has already tried to break on its own — repeated
failing pipelines or repeated review rejections — which only a person
dragging the card out of Blocked can clear. In every case the task carries a
system comment naming what it's waiting for, and the origin column it came
from, so moving it back doesn't lose its place.

See [Quality gates](quality-gates.md#clarification) for how the clarification
flow specifically works end to end.

## Why Backlog and Blocked are special

Every other column represents a real state of active work — something an
agent or a human is expected to be doing right now. Backlog and Blocked
don't: Backlog is work that hasn't been taken onto the board yet, and
Blocked is work that's stopped through no fault of the task itself. Keeping
both out of dispatch is what makes a card's presence in a working column a
reliable signal that something is actually happening to it — a card in Todo
or In Progress is either running or about to be, never quietly waiting on
something nobody's tracking.

## The Released archive

A task stays visible in the working board's Released column for **7 days**
after it lands there. After that it leaves the board view but is not
deleted — it moves into the released archive, searchable by key, title or
description, so the working board stays about what's still moving rather
than accumulating every task the product has ever shipped.
