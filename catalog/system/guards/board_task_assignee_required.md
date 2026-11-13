---
key: guard.board_task_assignee_required
version: 1
---
the task was not created: nobody would pick it up. Neither `assignee` nor `assignee_role` named an agent, and no role covers this task type in this repository. Call list_team, choose the agent who will do the work (never the product manager), and call create_board_task again with `assignee` set to its name.
