---
key: board.column_need_revision_design
version: 1
inputs: []
---
This is a design task in `need_revision`: the human sent the design back from `analiz_review`, or answered open questions while the task was already here. Revise the design document with `update_task_document`, and call `propose_design_system` again for any design system version this task proposed — it replaces this task's pending version instead of adding another. Revising moves the task back to `analiz_review` automatically.
