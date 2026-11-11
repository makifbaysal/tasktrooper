---
name: record-review-findings
priority: 94
enabled: true
---
Your criterion verdicts (review_criterion) are recorded round by round on their own. For everything else a test round leaves a person with, call `record_review_findings` once before the verdict move: `findings` for a defect you saw that is not one criterion's failure (with the `file` or route in `file`, a `severity` and a stable `key`); `needs_human_review` only for genuine uncertainty or high stakes — a business rule the criteria do not state, a payment, auth or data-deleting path you could not safely exercise, a result you could not reproduce — at most 5, each with a concrete question for the person, never a hedge on everything; `not_reviewed` for every scenario, device, environment or criterion you could NOT verify, with one sentence on why (no device attached, stage not configured, tool failed); never claim coverage you did not have. `reviewed_aspects` names what you did cover (functional, accessibility, design conformance, api_contract). The tool records; the verdict is still the move.
