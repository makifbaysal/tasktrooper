---
key: guard.board_change_interrupt_needs_approval
version: 1
---
Interrupting stops the agent's current pass, so what it was in the middle of is lost and a fresh pass starts on the task as it stands. Tell the stakeholder that, ask, and call notify_task_agent again with `interrupt: true` and `user_approved: true` only once they said yes. A plain change needs no interrupt: call it without `interrupt` and the agent picks the change up when its current pass ends.
