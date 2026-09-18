---
name: qa-verify-before-verdict
category: qa
description: Execute every recorded case and write its result on the task before any verdict
---

# Verify Before Verdict

No verdict without fresh executed evidence in this run.

- Every case on the task gets a result you recorded yourself: `passed` or `failed` with the exact command and its observed output (`actual`), UI cases additionally with the screenshot paths that show the result (frontend-manual-testing). The result belongs on the case (`set_test_case_result`), not in a comment — that is where the next reader looks, and it survives the run.
- A case you could not execute is `skipped` with what blocked it (no device, no stage deploy, missing credential), never `passed`. A case still `planned` when you try to hand the task on refuses the move.
- A pass verdict rests on ONE thing this iteration: every case executed manually by you, with its evidence. Writing automated tests is out of scope for now — do not add suites and do not hold a verdict waiting for one. The repository's existing pipeline is still read (`get_pipeline_status`): red is a finding, green is not a substitute for your own run.
- A criterion you could not execute is never approved. Say which one, what blocked it and what would unblock it — an unverifiable criterion is a `need_revision` or a reported gap, not a pass.
- Both verdicts are moves out of `in_qa` — the column you took the task into before testing. All cases green → move it to pm_uat with the evidence on the cases and the criteria approved — except a `task_type=technical` task, which has no UI-facing behaviour for a PM to review: move it straight to human_uat instead, same evidence, same "no comment" rule. Any case fails → move it to need_revision with, per failure, the acceptance criterion, exact reproduction command, EXPECTED vs ACTUAL (and the screenshot for visual defects). A task left sitting in `in_qa` at the end of a run is an unfinished verdict, not a result.
