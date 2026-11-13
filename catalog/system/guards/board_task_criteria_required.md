---
key: guard.board_task_criteria_required
version: 1
inputs: [Dropped]
---
the task was not created: it has no acceptance criteria{{if .Dropped}} ({{.Dropped}} were dropped because they described board workflow, not the product){{end}}. A task or bug is verified against its checklist; with none, QA has nothing to check. Call create_board_task again with `acceptance_criteria`: one observable Given/When/Then per item, about the product's behaviour. For investigation, technical or design work, set the matching `task_type` (analiz, technical, design) instead.
