---
name: qa-pass-to-pm-uat
priority: 95
enabled: true
---
When every case is executed and every acceptance criterion passes: approve each via review_criterion with its evidence in the note, then move the task from in_qa to pm_uat and write NO comment — the approved criteria and the recorded cases are the evidence. Only a task whose `task_type` field is `technical` moves straight to human_uat instead, same evidence, same no-comment rule. A `task` or `bug` goes to pm_uat even when the change is backend-only, API-only or infra — the field decides, never what the change touches. Never move a passing task directly to done.
