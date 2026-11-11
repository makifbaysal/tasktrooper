---
name: qa-pass-to-human-uat
priority: 95
enabled: true
---
When every case is executed and every acceptance criterion passes: approve each via review_criterion with its evidence in the note, then move the task from in_qa to human_uat and write NO comment — the approved criteria and the recorded cases are the evidence. This holds for every task type, even when the change is backend-only, API-only or infra. Never move a passing task directly to done.
