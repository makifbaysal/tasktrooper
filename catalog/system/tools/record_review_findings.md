---
key: tool.record_review_findings
version: "1"
params:
    task_id: Board task UUID or its board key. Optional in a task run — omit it there and the task in context is used.
    findings.items.properties.key: 'A short stable name for this problem, e.g. "ARCH-2" or "SEC-1". Use the SAME key for the same problem in a later round so the history can tell "still open" from "fixed".'
    findings.items.properties.file: Repository-relative path the finding is about.
    findings.items.properties.line: Line number in that file, when there is one.
    findings.items.properties.severity: How bad it is.
    findings.items.properties.category: 'What kind of problem: e.g. correctness, security, performance, tests, api, migration, style.'
    findings.items.properties.message: The finding itself, one or two sentences.
    findings.items.properties.confidence: How sure you are. Say "low" when you are not.
    findings.items.properties.human_review_reason: 'Only when a person should look at THIS finding because you are genuinely unsure or the area is high-stakes: one sentence on why.'
    findings.items.properties.question: The concrete question for the person, when you set human_review_reason.
    findings.items.properties.status: '"withdrawn" takes back a finding from an earlier round that you now judge to be wrong (use its key). Omit otherwise.'
    needs_human_review.items.properties.file: Repository-relative path the person should look at.
    needs_human_review.items.properties.line: Line number, when there is one.
    needs_human_review.items.properties.reason: 'One sentence on why a person should look: "touches payment logic I could not run", "business rule not in the criteria", "security-sensitive change", "could not verify at runtime".'
    needs_human_review.items.properties.question: The concrete question for the person.
    not_reviewed.items.properties.scope: A file, a directory or an aspect you could not verify.
    not_reviewed.items.properties.reason: One sentence on why you could not.
    reviewed_aspects.items: 'An aspect you did cover, e.g. "correctness", "security", "tests", "performance", "api_compatibility", "migrations".'
---
Record what your review found, in a form the task's review history and the person deciding can read. Call it once per review run, BEFORE the verdict move. `findings` are the problems you are rejecting or noting (up to 40); the same problem keeps the same `key` in every round. `needs_human_review` is for places you could not settle yourself — genuine uncertainty or high stakes (auth, payments, data migration, destructive operations, security, public API changes, anything you could not run) — at most 5 flags in the whole report, each with a concrete question for the person; flagging everything flags nothing. `not_reviewed` is mandatory honesty: every file, directory or aspect you did not verify, and why; never claim coverage you did not have. `reviewed_aspects` names what you did cover. This tool records; the verdict is still the move. Returns how many items were recorded.
