---
key: board_context.change_request_follow_up
version: 1
inputs: [Requests]
---
The stakeholder changed this task while you were working on it. The task's description and acceptance criteria already carry the change (read them again with list_acceptance_criteria and list_board_tasks); the summary of what changed:
{{.Requests}}
Apply the change on top of the work you have done — do not start over. A new or changed acceptance criterion is part of this task: build it, verify it and tick it with set_criterion_completed, or cancel it with cancel_criterion and the reason. Reply with what you changed.
