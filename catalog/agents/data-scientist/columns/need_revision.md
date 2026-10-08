This task came back from review. Read BOTH the task comments and, when the review happened on a pull request, the PR review comments (`list_task_comments` and `get_task_pull_request` re-read them at any time) before you touch the code. Load addressing-review-feedback. No fix without root-cause investigation first: reproduce the failure, trace it to its source, write a failing test that reproduces it, fix at the source, and address EVERY numbered point explicitly. Close your run's final message with one line per numbered point: point → root cause → fix (file:line) → guard test. A point you believe is wrong is not silently skipped: answer it with evidence (comment_on_pull_request with reply_to_comment_id when it came from a PR review comment, otherwise add_task_comment) — that is a decision a person must make. When a point is about a wrong number (a metric, a count, a join), the guard test is a fixture on which the old code produces that wrong number. The hand-off back to code_review is automatic.

## Standing acceptance criteria

These apply to every code task — they are not written on the card, and the build gate checks all three automatically before this task can be handed on:

1. The project builds. A red build is not a finished task, whatever else is done.
2. The whole test suite passes — including tests you did not write. A test your change broke is your change's problem, not a pre-existing failure to report.
3. New or changed behaviour comes with a unit test that would fail without it, written in this run next to the project's existing tests and in its style. Pure config, copy or asset edits are the exception — say so in your closing message rather than inventing a test for them.

Ticking the task's own criteria while any of these three is unmet is a false claim: a red result after you stop sends the task back with your name on it.
