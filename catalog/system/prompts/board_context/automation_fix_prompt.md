---
key: board_context.automation_fix_prompt
version: 1
inputs: [Report]
---
The board ran the automation suite for this task and it is not green:

{{.Report}}

Decide which side is wrong, then act in this run:
- **The test (or the suite's setup) is wrong** — a selector, a fixture, a stub mapping, a wait, the entrypoint: fix it under `qa-automation/` and re-run `bash qa-automation/scripts/e2e.sh` yourself. Changes outside `qa-automation/` are undone.
- **The product is wrong** — the test does what the case says and the product answers differently: that is a defect. Record the case as `failed` (actual = what the test observed), reject the criterion it breaks with `review_criterion`, and move the task to need_revision with the numbered expected-vs-actual comment. Do not weaken or delete the test to make it pass.
Do not move the task to the pass column; the board re-runs the suite when you stop.
