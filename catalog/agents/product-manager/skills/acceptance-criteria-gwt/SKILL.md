---
name: acceptance-criteria-gwt
category: pm
description: Use when writing acceptance criteria for a task - express each as an observable Given/When/Then that QA can execute, including negative cases
source: cucumber/docs better-gherkin.md (MIT), adapted
---
# Acceptance Criteria (Given/When/Then)

## Overview

Acceptance criteria are the contract QA tests the product against and the PM verifies at UAT. If a criterion isn't executable, it can't be verified — and unverifiable criteria are where defects hide.

**Core principle:** Every criterion is an observable behavior QA can execute with a command or a click path. No aspirational adjectives.

## The shape

`Given [precondition], When [action], Then [measurable result].`

## Where they go

Criteria live in the board task's own `acceptance_criteria` field — an array of strings, one criterion per item, passed to `create_board_task` (or `update_board_task` to replace the whole list). Each item becomes a checkbox the implementer ticks with `set_criterion_completed`, and that QA (in ready_for_qa/in_qa) then rules on independently with `review_criterion`. The tick is the implementer's claim — QA never calls `set_criterion_completed`; its verdict is `review_criterion`.

Never write them into `description`. A criteria block pasted as markdown leaves the task's checklist empty: nothing to tick, nothing the move guard can check, and the prose copy goes stale the moment the real criteria are edited.

## Testability gate

For each criterion, before you save it, write down:
- **who executes it** — QA in in_qa, with browser or mobile tools (or `http_request` against the task's own preview when the surface is an API, not a screen);
- **the action** — a click path, a URL, or a request;
- **the exact observable** — text, a count, an element state, or a file.

If the behaviour is user-facing, the Then must be visible in the UI — not a status code standing in for what the user sees. If nobody outside the code can observe it, it belongs in `technical_description`, not in a criterion.

## Declarative When, concrete Then

Write the When as what happens, not the click-by-click path — the cucumber test: "Will this wording need to change if the implementation changes?" If yes, it's too imperative. One behaviour per criterion. Put exact copy strings in quotes in the Then; "an error appears" is not testable, "'Title must be at most 200 characters' appears under the field" is.

## Never a criterion: board actions

A criterion states what the finished WORK is, never what the board does around
it. These are not criteria and the tools drop them:

| Not a criterion | Why | Write instead |
|---|---|---|
| "Task moved to ready_for_qa / analiz_review / done" | a column move is workflow; the agent's role already tells it where the card goes | the behaviour that makes the task ready |
| "Implementation tasks are created" | they are opened AFTER this task is approved — a criterion nobody can tick before the hand-off keeps the card out of done forever | what the plan those tasks come from must contain |
| "Spec attached with add_task_document" / "questions asked with add_task_comment" | naming the tool describes the mechanics, and an attached document is not a good one | what the document must say |
| "Presented for human approval" / "assigned to the developer" | hand-offs are the flow's job | nothing — the flow does it |

The test: could a reviewer check it by looking only at the product and the
documents, without opening the board's history? If not, it is not a criterion.

## Rules

- **Observable + measurable.** "Then a 422 with message 'title too long'" — not "Then it handles it gracefully."
- **At least one negative criterion where the feature accepts input or has permissions:** invalid input, unauthorized access, empty state. Not required for a pure copy or styling change.
- **3–7 criteria per task.** More means the task should be split.
- **No 'fast'/'user-friendly'/'robust'** — replace with a number or a concrete behavior.

## Mid-flight edits

`update_board_task(acceptance_criteria=…)` replaces the whole array and resets every tick on it — use it only before work starts, or when the human explicitly asked for the change. To drop one criterion from a task already in progress (descoped, superseded, covered elsewhere), use `cancel_criterion` with a reason instead of rewriting the list.

## Worked Example

Feature: reject task titles over 200 chars, in the New Task dialog.

```
❌ Given a create-task form, When I submit a 201-char title, Then I get 422.
   (status code stands in for what the user sees — fine for an API-only task, not for a dialog)

✅ AC1 (happy)    Given the New Task dialog,
                  When I enter a 200-character title and press Create,
                  Then the dialog closes and a card with that title appears on the board.

✅ AC2 (boundary/negative)
                  Given the New Task dialog,
                  When I enter a 201-character title and press Create,
                  Then the dialog stays open, shows "Title must be at most 200 characters"
                  under the field, and no card is added to the board.

✅ AC3 (auth/negative)
                  Given a user without write access to the project,
                  When they open the board,
                  Then the "New Task" button is not shown.
```

Keep one API-surface example only when the API itself is the product (usually a `task_type: "technical"` task):

```
Given a member of the project, When they GET /api/v1/projects/:id/tasks/export,
Then they get 200 + text/csv with header "id,title,status,created_at"
```

## Common Mistakes

- A board action as a criterion (see above) — the most common one is "the task is moved to X".
- An HTTP status as the only Then on a user-facing task — say what the user sees, not the wire protocol underneath it.
- Aspirational wording ("intuitive", "fast").
- Only happy-path criteria, no negative, on a task with input or permissions.
- A "Then" with no exact text, count or state — "an error appears" instead of the exact copy.
- 12 criteria on one task → split it.
- Criteria pasted into `description` as a markdown list instead of passed as `acceptance_criteria`.
- Rewriting `acceptance_criteria` on a task already in progress instead of `cancel_criterion`.

## Red Flags

- QA asks "how do I test this?" → the AC isn't executable; run it through the testability gate again.
- No negative/auth criterion on a task that takes input or checks permission.
- An adjective in a "Then".
- A criterion that names a board tool or a column the card must reach.
