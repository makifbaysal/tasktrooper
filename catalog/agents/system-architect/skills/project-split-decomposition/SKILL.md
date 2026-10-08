---
name: project-split-decomposition
category: architecture
description: Use when an approved analiz spans more than one repository - split the work per repository and open one or more tasks per repository, each carrying its own plan slice
---
# Project Split Decomposition

## Overview

Real features cross repository boundaries: an API in the backend repo, a screen in the web repo, a screen in the mobile repo. After the human approves the analysis, you split the work along repository lines and open **one or more implementation tasks per repository, never one task spanning two**, each carrying the slice of the plan that repository needs. A developer working the web task should never have to read the backend repo's plan to do their job.

**Core principle:** One repository (and, in a monorepo, one `component`), one self-contained plan slice per task. The split follows the deployable unit, not the feature.

**REQUIRED SUB-SKILL:** task-decomposition (per-task structure, AC, assignees). This skill decides the BOUNDARIES; task-decomposition fills each task in.

## When to Use

Use after the human approves an analiz (see analiz-human-review-gate) whenever the plan touches more than one repository. A single-repository change skips straight to task-decomposition.

## How to Split

1. **List the repositories the plan touches**, by name from `list_repositories`: backend-api, web, mobile, an automation/test repo, etc. Note which `project` (initiative, from `list_projects`) each belongs to, if any.
2. **One or more implementation tasks per repository, never one task spanning two.** They build, test, and deploy independently.
3. **Within a repository, split by layer only if independently deliverable**, and by `component` when it is a monorepo. A backend task may cover the whole vertical slice for that repo; split further only where a reviewer could accept one part and reject another.
4. **Write each repository's plan slice into its task.** Copy the relevant plan sections, file paths, and the Interfaces blocks that task consumes/produces — verbatim, not "see the main plan." The task is self-contained.
5. **Order by cross-repository dependency, in the arguments.** The repository that produces an interface goes before the repository that consumes it. On the consuming task set `blocked_by: ["<producer key>"]` (nobody starts it until the producer is done) and `deploy_depends_on: ["<producer key>"]` (it may not be released until the producer is live). Both are enforced — a prose "depends on the backend task" is not. See task-decomposition, "Ordering Is an Argument, Not a Sentence".
6. **Point every task back at the analysis.** `derived_from: ["A-N"]` on each one. The spec and the plan live as documents on that analiz task, so this is the only route each developer has to them.
7. **Size and shared files.** A task should land at an estimated ≤ ~400 changed lines including tests, so a reviewer sees all of it inside the 24,000-byte diff cut; over ~1,000 is a split, and refactors are separate from feature work. Two tasks in the same repository that both add a migration (sequence numbers collide), or edit the same router table, locale bundle, DI container, `INVENTORY.md` or OpenAPI file, are not independent — chain them with `blocked_by` rather than leaving the collision to be discovered at review time.

## The Cross-Repository Contract

The one thing that MUST be identical across the split is the interface between repositories — the API contract. Define it once in the producing task and copy the exact request/response shape into the consuming task's plan slice. A field named `taskId` in the backend task but `task_id` in the web task is a guaranteed integration bug.

## Schema Tasks

A repository task that adds or changes a migration carries `before_deploy` (what must be true before this ships — e.g. "migration N applied on stage") and `rollback_plan` (a code rollback is a `git revert`; it does not undo the schema, so state what the schema looks like afterward and that the previous release's code must still run against it). A breaking schema change (a drop or rename still read by deployed code) is a separate, later task with `deploy_depends_on` every consumer update — see migration-and-contract-review.

## Worked Example

Approved analiz: "Users can export a project's tasks to CSV from web and mobile."

Split into three tasks:

| Task | Repository | Assignee | Ordering arguments | Scope slice |
|------|---------|----------|-----------|-------------|
| Add task export endpoint (T-1) | backend-api | backend-developer | `derived_from: ["A-12"]` | `GET /api/v1/projects/:id/tasks/export` → CSV; plan slice with handler, service, test |
| Add export button to web board | web | frontend-developer | `derived_from: ["A-12"]`, `blocked_by: ["T-1"]`, `deploy_depends_on: ["T-1"]` | Calls the endpoint, downloads the file; plan slice with component + api client |
| Add export action to mobile board | mobile | mobile-developer | `derived_from: ["A-12"]`, `blocked_by: ["T-1"]`, `deploy_depends_on: ["T-1"]` | Same endpoint, native share sheet; plan slice with screen + api client |

A data or game slice follows the same rule: a churn model's training pipeline (data-scientist, in the analytics repository) and the endpoint that serves its score (backend-developer, in the API) are two tasks, the second `blocked_by` the first and consuming the model artifact's name and version from it.

The exact endpoint path and CSV column order are defined in the backend task and copied into both consumer tasks' plan slices. All three go to `todo` together with `repository` set on each (`backend-api`, `web`, `mobile`): the two consumers park themselves behind T-1 and are picked up automatically when it lands, and neither can be released ahead of it.

## Common Mistakes

- One task titled "add export to web and mobile" — two repositories, must be two tasks.
- A consumer task that says "see the backend plan for the contract" — copy the contract in; the developer only sees their own task.
- Splitting a single-repo change across three tasks that can't be reviewed independently — over-decomposition.
- Mismatched field names between the producing and consuming task's plan slices.
- Leaving `repository` unset and trusting the fallback — it lands in the active repository context or the default one, which is often wrong for a consumer task.

## Red Flags

- A consumer task with no `blocked_by` / `deploy_depends_on` on its producer → the order exists only in your head, and the frontend can start (and ship) before the API it calls.
- A task with no `derived_from` → the developer cannot reach the plan slice you wrote for them.
- A created task whose `repository` differs from its row in the `split` table.
- A task's plan slice references files in a repository the task doesn't own.
- Two tasks would edit the same repository's same files → they are really one task, or `blocked_by` if they must stay separate.
- The interface shape differs between the producer and consumer tasks.
