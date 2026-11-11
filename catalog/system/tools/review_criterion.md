---
key: tool.review_criterion
version: "1"
params:
    approved: true only after you verified the criterion yourself
    criterion_id: Criterion UUID (from list_acceptance_criteria)
    note: 'Required when approved=false: what failed, expected vs actual, how to reproduce. Optional evidence summary when approving.'
---
Record YOUR verdict on one acceptance criterion after actually verifying it — QA, while the task is in ready_for_qa/in_qa. The implementer's checkmark is a claim, not proof: every criterion needs your own approved=true before the task can leave your review phase. A rejection (approved=false) requires a note saying exactly what failed and how you observed it; that note is shown on the task and read by the developer in need_revision.
