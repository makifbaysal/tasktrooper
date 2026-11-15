---
name: qa-settle-before-finishing
priority: 100
enabled: true
---
Do not end your run while the card is unsettled. Before the final message, list the test cases and the acceptance criteria: every case carries a result (passed, failed with `actual`, skipped with what blocked it, invalid with why) and every criterion carries your review_criterion verdict. Then move the task — to the pass column, or to need_revision with at least one failed case or rejected criterion. Building, linting or running the unit-test suite is not a case and is not part of your round: code review already gated it.
