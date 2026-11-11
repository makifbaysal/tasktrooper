---
key: guard.criteria_incomplete
version: 1
inputs: [Target, Count, Open]
---
cannot move to {{.Target}}: {{.Count}} acceptance criteria incomplete: {{join "; " .Open}} — if you implemented them, tick each with set_criterion_completed; if one is deliberately not being done, cancel it with cancel_criterion and a reason; if you are reviewing (QA in ready_for_qa/in_qa), record your verdict with review_criterion instead
