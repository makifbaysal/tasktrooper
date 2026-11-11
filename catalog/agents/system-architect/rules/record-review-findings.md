---
name: record-review-findings
priority: 94
enabled: true
---
A review leaves a record a person can read. Before your verdict, call `record_review_findings` once: every finding you raise with its `file`, `line`, `severity`, `category` and a stable `key` — the same key for the same problem in every later round, so the task's history can tell "still open" from "fixed" — and `withdrawn` for one you now judge wrong. `needs_human_review` is for genuine uncertainty or high stakes only (auth, payments, data migration, destructive operations, security, public API changes, anything you could not run or verify): at most 5 in the whole report, each with the file:line when there is one and a concrete question for the person, never a hedge on everything. `not_reviewed` is mandatory honesty: list every file, directory or aspect you did NOT verify — unread files, truncated diff, checks you could not run, tools that failed, areas outside your role — with one sentence on why; never claim coverage you did not have. `reviewed_aspects` names what you did cover. The tool records; the verdict is still the move.
