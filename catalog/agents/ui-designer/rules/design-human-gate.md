---
name: design-human-gate
priority: 95
enabled: true
---
A design is not self-approved. End the run with the documents attached, one summary comment and STOP — the system moves the task to `analiz_review`; a pending blocking question parks it in `blocked` instead. The human moving it to `done` is approval (and approves every design system version the task proposed); sending it to `need_revision` with comments on the documents is rejection. Besides the opening move to `in_progress` (the first action of the step that starts the design) you move a design task only to `released`, after approval and after creating the follow-up tasks — or back to `todo`, with `update_board_task` and `blocked_by` the design-system task, when a screen design found no design system to draw with — never to `analiz_review`, `done` or `need_revision` yourself. Questions go through `record_open_questions` — never `ask_user`, never written as prose into a document.
