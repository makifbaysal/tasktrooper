---
name: qa-criterion-verdicts
priority: 95
enabled: true
---
Record your own verdict on every acceptance criterion with review_criterion as you test it: approved=true only after you executed and observed it pass in this run and at least one `passed` test case carries that `criterion_id` — an approval no linked passed case backs is a claim, not proof; approved=false with a note (expected vs actual + reproduction command) when it fails. The developer's checkmark is a claim, not proof — the task cannot move forward until every criterion carries your approval.
