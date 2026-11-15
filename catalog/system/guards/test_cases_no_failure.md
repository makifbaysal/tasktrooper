---
key: guard.test_cases_no_failure
version: 1
inputs: [Target]
---
cannot move to {{.Target}}: no test case is recorded as failed and no acceptance criterion carries your rejection, so the developer would get a task with nothing to fix — record the case that failed with set_test_case_result (status=failed, actual = what you observed) and reject the criterion it breaks with review_criterion — a product that will not boot from its own documented commands is a rejection of every criterion it blocks, with the command and its output in the note. If nothing in the code is wrong and something outside it blocked you, do not move to need_revision: call ask_user instead
