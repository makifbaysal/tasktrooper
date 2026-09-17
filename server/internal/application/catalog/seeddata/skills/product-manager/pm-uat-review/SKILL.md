---
name: pm-uat-review
category: pm
description: How to conduct PM UAT review
---

# PM UAT Review

When a task reaches pm_uat:

1. Read the ORIGINAL task description and every acceptance criterion.
2. Read QA's evidence: the `review_criterion` note on each criterion (commands run, observed outputs, screenshot paths) — that is where a passing QA round records what it executed, and it no longer duplicates it in a comment. A comment from QA exists only when something failed.
3. **Coverage-gap check.** Call `list_test_cases` and, criterion by criterion, check whether it has a `passed` case linked to it (matching `criterion_id`). QA's `review_criterion=approved` note is a claim, not proof — trust the case list, not the note. Any open criterion with no passed case backing it goes on the list you must produce your own evidence for in step 4 before you can approve it; QA's note alone is not enough.
4. Map each AC to a piece of executed evidence. Reading source code is NOT verification — only executed evidence counts. You have no code-reading tools in this column anyway.
5. Walk the critical flows YOURSELF on stage: resolve the stage base_url with `get_deploy_target` (env="stage"), then `browser_navigate` → `browser_wait_for` → `browser_fill`/`browser_click` through each user-facing AC and `browser_screenshot` the outcome. QA's evidence tells you it was tested; your own walk-through tells you it works as the stakeholder asked. Put the screenshot path in the `review_criterion` note of the criterion it proves. never-test-in-prod applies to you too: stage only, never the production environment or production data. Every criterion flagged uncovered in step 3 MUST get this walk-through before you approve it — there is no backend exception today: if the repository has nothing to click, say so in the gap list instead of approving on QA's word alone.
6. Every AC covered by evidence (QA's passed case, or your own walk-through from step 5): approve each criterion with its evidence in the note and move the task to human_uat. Write NO comment — "PM UAT passed" says nothing the approved criteria and the column do not already say.
7. Any AC without evidence, with failing evidence, or failing your own walk-through: move to need_revision. add_task_comment with a numbered gap list — quote the unmet criterion, expected vs actual, with the screenshot for visual gaps.

Never reject with vague feedback, never approve without evidence.
