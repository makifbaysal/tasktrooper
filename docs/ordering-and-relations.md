---
title: Ordering and relations
description: How blocked_by, deploy_depends_on, derived_from and discovered_from order and connect tasks.
---

Tasks can point at each other in four ways. Two of them are enforced order;
two are provenance only — a record of where a task came from, which nothing
gates on.

| Relation | Direction (as you set it) | Enforces | Set by |
|---|---|---|---|
| `blocked_by` | this task waits for the ones you list | Work order — parks the task until they're finished | You, or an agent, at creation or update |
| `deploy_depends_on` | this task ships after the ones you list | Shipping order — refuses the release until they're live | You, or an agent, at creation or update |
| `derived_from` | this task was opened out of that analysis | Nothing — feeds the analysis's documents into the run | An agent, when opening implementation work from an approved analysis |
| `discovered_from` | this task was found while working that one | Nothing at all | Automatically, whenever a task is created from inside a run |

All four point the same way — "this task comes after / came from the ones
you name" — so nobody has to reason about which end of a relation they're
holding.

## `blocked_by` — work order

Set `blocked_by` on a task to name other tasks that must be **finished**
(Done or Released) before this one may start. It's enforced in two places:

- Dragging a card into **Todo** or **In Progress** by hand is refused while
  an open blocker remains.
- If the task reaches a working column some other way — created straight
  into Todo, handed back by a sweeper, assigned — the dispatcher parks it on
  **Blocked** the moment it would otherwise start a run, with a comment
  naming every unfinished blocker.

A background sweep checks once a minute whether every blocker for a parked
task has finished (or disappeared — deleting a task removes its relations
with it) and releases the task the moment they have. Two parked tasks
waiting on different blockers are independent: whichever one's blockers
finish first is released first.

`blocked_by` **adds** to the existing set rather than replacing it — a
blocker one planner learned about is never silently dropped by a second
planner adding another.

Internally this is stored as a `blocks` relation with the blocker as the
source and the waiting task as the target — the opposite direction from
`deploy_depends_on` — but no tool or field ever asks you to think in that
direction; you always name blockers through `blocked_by`.

## `deploy_depends_on` — shipping order

Set `deploy_depends_on` to name tasks that must be **live in production**
before this one may be released — for a real shipping-order dependency, like
an API that has to exist in production before the client calling it can go
out. Two tasks can be developed fully in parallel (neither blocks the
other's work) and still have a hard deploy order.

A task does not ship while any of its deploy dependencies has not reached the
Released column (a dependency shipping in the same release counts as
satisfied). What "does not ship" means follows the component's delivery mode:
an `on_merge` task is not merged (the merge would deploy it), a `dispatch`
release is not deployed, and a `batch` release cannot be cut. The task gets
one comment naming the blocking keys; when the last dependency is released,
the release engineer is woken on it and carries on by itself. A dependency
may live in another repository.

Unlike `blocked_by`, setting `deploy_depends_on` **replaces** the whole set:
the release path always reads it as one complete statement, so a caller
editing it is expected to know the whole set it's declaring.

## `derived_from` — the analysis a task came from

Set on an implementation task to name the `analiz` task it was opened out
of. This is provenance, not order — the human already approved the plan in
Analiz Review before the implementation tasks were even created, so gating
on it again would be redundant. What it actually does: the named analysis's
documents (its spec and plan) are read into every run of this task
automatically, and `list_task_documents` can re-read them at any time. Since
an analysis's deliverable exists only as documents on that task — never a
file on any branch — this relation is the only route an implementer has back
to the specification they're building against.

## `discovered_from` — found along the way

You never set this yourself. When an agent opens a new task while working on
an existing one — something it ran into but wasn't asked to do — the new
task is linked back to the one the run was working on automatically, with no
tool argument involved. It's weaker than `derived_from`: "found while
working on," not "specified by." It orders nothing and feeds no documents
into anything; it's a trail to follow later, nothing more.

## Cycles are refused where they're written

Both ordering relations refuse a cycle the moment the edge that would close
it is written — at creation or update, not later at release time — and the
error names the chain that closes it, for example:

```
deploy-order cycle refused: T-2 already ships after T-1 (T-2 → T-1)
```

A task also can't depend on itself.

## Seeing both directions

A task's own detail view shows the relations it is the *source* of (what it
ships after, what it blocks, what it was derived from) and, separately, the
`blocked_by` edges pointing *at* it (who is waiting on this one, with their
key and title) — a single-task view carries both; bulk board lists carry
neither.

## The generated ordering block

`before_deploy` keeps a block TaskTrooper generates and regenerates on its
own whenever the relations change, fenced so your own notes around it
survive:

```
<!-- tt:order -->
**Release order (generated from this task's relations — do not edit by hand):**
- Ships after: T-1 (task export endpoint). Each one must be live in production before this
  task is released; the release is refused otherwise.
- Built after: T-1 (task export endpoint). Work on this task does not start until those are done.
<!-- /tt:order -->
```

The same text, with the markers stripped, is what gets posted as a comment
when the release is actually dispatched.

## Why two ordering relations, not one

`blocked_by` and `deploy_depends_on` look similar — both mean "after those"
— but they answer different questions, and a real project needs both
answered separately: two tasks can be written in parallel with neither
blocking the other's code (no `blocked_by` needed) while still having to
ship in a fixed order (an API before the client that calls it, via
`deploy_depends_on`). Collapsing them into one relation would force every
task with a shipping order to also wait to start, even when there's no
reason it couldn't be built alongside its dependency.

## Setting relations from a task

All four relations are reachable wherever a task's fields are: the create
dialog's deploy section for `deploy_depends_on`, and an agent's own tools
(`blocked_by`, `deploy_depends_on`, `derived_from` arguments on the task-
creation and update tools) for the rest. A relation always resolves either a
task's UUID or its board key (`T-12`), so you never need the UUID on hand to
reference one.

## The ready queue

`list_ready_tasks` is the queue agents check for unblocked work: every task
in Backlog or Todo with no unfinished `blocked_by` blocker, sorted critical →
high → medium → low and then oldest first. A task parked on Blocked never
appears in it — it's excluded before the blocker check even runs.
