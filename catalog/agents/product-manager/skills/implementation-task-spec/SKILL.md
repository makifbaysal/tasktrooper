---
name: implementation-task-spec
category: pm
description: Use when you create an implementation board task directly - the required fields, one-role-one-deliverable rule, and dependency ordering
---
# Implementation Task Spec

## Overview

An implementation task is a self-contained unit of work for one developer role. The recurring failures are bundling two layers into one task and vague, untestable acceptance criteria.

**Core principle:** One task = one role = one deliverable.

## create_board_task fields

| Field | Value |
|-------|-------|
| task_type | `task` (user-visible feature or change) · `bug` (existing behaviour is wrong) · `technical` (no user-facing behaviour: refactor, infra, CI, backend-internal — QA sends it straight to human_uat, skipping pm_uat) · `analiz` (investigation → system-architect, see analiz-task-spec) · `design` (a screen designed before it is built — the type assigns the ui-designer itself, so pass no `assignee`; the human approves it in analiz_review). A missing design system is never a hand-written design task: `request_design_system` opens it (Design brief below). Spell the argument `task_type` — `type` is silently ignored and the task is created as the default type. |
| column | `backlog` by default (stakeholder reviews before start); `todo` only if told to start immediately |
| assignee | one responsible developer role — REQUIRED: pass `assignee: "<role>"` (e.g. `backend-developer`) so that agent is dispatched. An unassigned task sits idle. Use `list_team` for valid names. |
| title | action-object ("Add task export endpoint", "Fix checkout price rounding") |
| description | PRODUCT only: user story ("As [persona], I want [capability], so that [outcome]") + context + out-of-scope + analiz reference if any |
| technical_description | TECHNICAL only: endpoints/files/schema touched, approach, constraints, interfaces consumed and produced. BEFORE filling it, verify every file and endpoint name against the actual repository: `get_repo_tree` for the layout, `codebase_search`/`grep_code` for the handler/route/symbol you are naming. Never write a path or endpoint you did not see — an invented path sends the developer to a file that does not exist. |
| acceptance_criteria | array of strings, one observable Given/When/Then per item, incl. error cases (see acceptance-criteria-gwt). About the PRODUCT only — never a board step ("moved to ready_for_qa", "PR opened", "QA notified"); those are dropped when the task is created |
| repository | which codebase the work touches — plain name (`"acme-web"`) or UUID. Resolve with `list_repositories`. Omitting it silently files the task against the default repository. |
| project | which initiative it belongs to — plain name (`"Acme"`) or UUID. Resolve with `list_projects`; `create_project` first if the initiative is new. |
| derived_from | the analiz task this work came out of, e.g. `["A-12"]`. Set it whenever an analysis exists. The spec and plan are DOCUMENTS on that analiz task — never a `docs/` commit — and this reference is what feeds them into the developer's run and lets them re-read the plan with `list_task_documents A-12`. |
| blocked_by | task keys that must be FINISHED before anyone starts this one, e.g. `["T-1"]`. Enforced: the board parks this task in `blocked` while any of them is open and picks it up automatically when the last one lands. A design task counts as finished only once RELEASED — the designer releases it after approval, once its hand-off and follow-up tasks exist — and blocking on it is also what hands this task's runs its approved documents. |
| deploy_depends_on | task keys that must be LIVE IN PRODUCTION before this one may be released. Enforced at release time, and written into this task's `before_deploy` runbook automatically. |
| before_deploy / after_deploy / rollback_plan | what has to happen around the deploy — a migration to run first, a flag to flip after, how to undo it. Posted on the card automatically when the release is dispatched and when it lands. Put them here, not in a comment: a comment is not read at deploy time. |

## Rules

- **Verify names before writing them.** Everything in `technical_description` that names a file, endpoint, table or symbol comes from `get_repo_tree` + `codebase_search`/`grep_code` output, not from memory or plausibility. If you cannot find it, say what you looked for and leave the naming to the developer instead of guessing.
- **One field, one home.** `description`, `technical_description` and `acceptance_criteria` are three separate board fields, each rendered in its own section of the task. Never write acceptance criteria or technical detail into `description` — a pasted copy is what QA and pm_uat then read instead of the real checklist, and it silently goes stale when the real field is edited.
- **Criteria describe the product, not the board.** A column move, a PR, a comment, a hand-off, an assignment, opening the next task — none of these is a criterion, and the create/update tools drop them. Every criterion must be checkable from the running product (or the delivered document) alone, without reading the card's history.
- **Criteria are structured, not prose.** Pass `acceptance_criteria: ["Given …, When …, Then …", …]` — each item becomes a checkbox the implementer ticks with `set_criterion_completed` and that QA and you then rule on with `review_criterion`. A criteria block written as markdown text produces a task with an EMPTY checklist, so the task can never be verified complete.
- **Tag repository and project.** Both accept names, so there is no reason to skip them and no reason to ask the stakeholder — look them up. To file a task that already exists, use `update_board_task` with `project`.
- **One role, one deliverable.** Never bundle backend + frontend in one task.
- **Creation-only fields.** `assignee` and `derived_from` can only be set by `create_board_task` — `update_board_task` has no such fields and silently ignores them; `task_type` too, unless `update_board_task`'s schema lists it. Fix a wrong one the update tool cannot change by `delete_board_task` + `create_board_task` (no `force` needed in backlog/todo).
- **Secrets.** Never ask the stakeholder for a third-party API key in chat. If a task needs one configured, say so in `before_deploy` ("needs `X_API_KEY` configured for <env>") and tell the human where to set it — not a comment, which nobody reads at deploy time.
- **Duplicate guard.** `create_board_task` returns `created:false` with the existing task when an open one has a near-identical title. Report "already on the board as T-n", not "created" — pass `allow_duplicate: true` only for genuinely different work that happens to share a title.

### Design brief (UI work)

Replaces a one-line "follow the existing design system" guess. Put this block in the `description` of any task that touches a screen a user sees:

```
Design brief
- Surface: marketing page | app screen | component change
- Audience & tone: <who>; <3 adjectives>          e.g. "freelance designers; calm, precise, premium"
- Content language: <the product audience's language, e.g. English> — not TaskTrooper's UI locale
- Visual direction: follow the existing design system (v<N> from get_design_system) | design system comes first: D-<n> (request_design_system) | new: colours <names/hex>, fonts <names>, logo <attached> — "new" or unclear means a screen design task comes first: D-<n>
- References: <URL> — borrow <layout | palette | tone>, not the whole look
- Content: real copy per section (headline, subhead, CTA labels, empty/error messages). Copy you drafted is marked "draft copy".
- Primary action per view: <one>
- States: loading / empty / error / success for <which data views>
- Responsive notes: <anything non-obvious at 360px>
- Attachments: <files added with attach_task_file>
```

Procedure:
- **Detect an existing design system before asking.** `get_design_system` for the repository first — an approved design system there means "follow the existing design system (v<N>)", no question. Nothing there (no `base`, no `layer`) and the work touches UI: `request_design_system` — `scope: "project"` with the `project.id` `get_design_system` returned, or `scope: "repository"` for a repository in no project — with the brief's audience, tone, visual direction and references in `notes`. It opens the design-system task the project's Design System tab opens, in `todo` like an analiz — the ui-designer derives it from the code that exists, or creates it from the brief when there is none, and the human approves it in analiz_review before anything is built on it — or returns the one already open (`created: false`). Every UI task you create is `blocked_by` its returned key, and the brief's Visual direction line reads "design system comes first: D-<n>". Never hand-write a design task for a design system.
- **Design first when the look is new or unclear.** When Visual direction is "new", the brief cannot say what the screen should look like, or the screen is new and no existing screen shows the pattern, the design comes before the build: create a screen design task first — `task_type: "design"`, no `assignee`, `repository`, `project`, the design brief and the screens it covers in `description`, and `blocked_by` the design-system task when you just requested one — then the implementation task(s) with `blocked_by` that design task. That alone hands the developer the approved mockups and the hand-off spec, and holds the task until the designer has written the hand-off and released the design; `derived_from` stays for an analiz task. Write "screen design task comes first: D-<n>" on the brief's Visual direction line. A change that follows an existing screen in the existing design system needs no design task.
- **Draft real copy yourself**, in the content language. Never leave lorem ipsum, and never block the task on copy — the human corrects it at backlog approval or human_uat.
- **Files the human uploads in chat** (mockup, logo, screenshot) go on the task with `attach_task_file` and get one descriptive line in the brief, since the text survives even where an attachment doesn't render.
- **UI acceptance-criteria templates:**
  - "Given the page at 360, 768, 1024 and 1440 px wide, When it renders, Then there is no horizontal scroll and nothing overflows (the browser_set_viewport report lists no overflowing elements)"
  - "Given <list> has no items, When the page loads, Then it shows '<exact empty-state copy>' and a '<CTA label>' button"
  - "Given saving fails, When I press Save, Then '<exact error copy>' appears beside the form and my input is kept"
- **Ordering is an argument, not a sentence.** analiz (if needed) → design (if needed) → backend → frontend/mobile → qa. Declare each cross-task dependency with `blocked_by` (nobody starts this until those are done) and, where the order also applies to shipping, `deploy_depends_on` (this is not released until those are live). Both point the same way: this task comes after the ones you list. A "Depends on: …" line in the description is worth writing for the human, but it enforces nothing on its own, and a cycle is refused at creation.
- **Ordered tasks still all go on the board.** A blocked task parks itself and is picked up automatically when its blocker lands, so there is no reason to hold work in `backlog` to fake an order.
- **Point at the analysis.** If the work came out of an analiz task, set `derived_from`. The architect normally does this; when you create the task yourself, you do.
- **Architect-created tasks:** most implementation tasks are created by the system-architect after the human approves an analiz — you write tasks directly only for clear, small, no-analiz work.

## Worked Example

```
create_board_task(
  title:       "Add task export endpoint",
  task_type:   "task",
  column:      "backlog",
  assignee:    "backend-developer",
  repository:  "tasktrooper",
  project:     "Task export",
  description: "As a project member, I want to export my project's tasks to CSV, so that I can share them.
                Out of scope: the web download button (separate frontend task). Depends on: none.",
  derived_from: ["A-12"],
  technical_description: "New GET /api/v1/projects/:id/tasks/export in internal/adapter/http/handler_task.go.
                Streams text/csv, reuses repository.Service.ListTasks. No schema change.
                Ref: the implementation plan attached to analiz task A-12.",
  acceptance_criteria: [
    "Given a member of the project, When they GET /api/v1/projects/:id/tasks/export, Then they get 200 + text/csv with header id,title,status,created_at",
    "Given a user without access to the project, When they GET the same URL, Then they get 403 and no data is returned",
    "Given an invalid project id, When they GET the endpoint, Then they get 400"
  ]
)
```

The web download-button task is a SEPARATE frontend task, and its order is declared rather than described:

```
create_board_task(
  title:             "Add export button to the project board",
  assignee:          "frontend-developer",
  repository:        "tasktrooper-web",
  derived_from:      ["A-12"],
  blocked_by:        ["T-1"],   # the endpoint task — nobody starts this until it is done
  deploy_depends_on: ["T-1"],   # and it must not ship before the endpoint is live
  ...
)
```

## Common Mistakes

- "Add export API and button" → two tasks.
- Aspirational AC ("works well").
- No assignee, or a role that can't do the work.
- An "Acceptance Criteria" heading inside `description` → the checklist is empty; pass `acceptance_criteria` instead.
- Technical detail written into both `description` and `technical_description` → keep it in `technical_description` only.
- An order stated only as "Depends on: the backend task" → nothing enforces it; pass `blocked_by` / `deploy_depends_on`.
- A task created out of an analysis with no `derived_from` → the developer has no route to the spec, because the spec is a document on the analiz task and not a file in the repo.

## Red Flags

- AC mentions two layers/repos.
- No error/auth criterion.
- The created task comes back with an empty `acceptance_criteria` array → you wrote them as prose. Fix it with `update_board_task`.
