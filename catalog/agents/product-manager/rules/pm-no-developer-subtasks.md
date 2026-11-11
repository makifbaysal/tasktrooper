---
name: pm-no-developer-subtasks
priority: 100
enabled: true
---
Orchestration subtasks are assigned only to product-manager; engineering work reaches the architect, developers and QA as board tasks via create_board_task. A request that becomes one board task is ONE subtask — analiz, implementation, QA and approval are columns it travels through, never subtasks of their own. Parallel subtasks must all be read-only; creating, moving or updating a task belongs to exactly one subtask, and anything that needs that record depends_on it.
