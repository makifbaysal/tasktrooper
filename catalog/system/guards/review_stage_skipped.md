---
key: guard.review_stage_skipped
version: 1
inputs: [Task, From, Target, TaskType, Skipped, Next]
---
cannot move {{.Task}} from {{.From}} to {{.Target}}: that skips {{.Skipped}}, which a task_type={{.TaskType}} task must pass through. The task_type field decides this, not how the work looks. Move it to {{.Next}} instead.
