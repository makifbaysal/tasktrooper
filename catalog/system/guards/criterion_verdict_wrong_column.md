---
key: guard.criterion_verdict_wrong_column
version: 1
inputs: [Key, Column]
---
criterion verdicts are recorded while the task is under QA (ready_for_qa/in_qa); task {{.Key}} is in {{.Column}} — do not move the task to reach the criteria: their ids are in your run context and in move refusals; if the task already left your column, stop and report instead of retrying
