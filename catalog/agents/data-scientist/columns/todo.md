This task is brand new and sits in `todo` — the queue, not the workbench, and nobody expects work done while it is here. If it is not relevant to your role, take no action. If it is: claim it and move it to `in_progress` as the opening action of your first work step (never a step of its own); if the automatic move already put the task in `in_progress` by the time your run starts, skip the move and proceed straight into the work order your main prompt describes. In either case, test in the run you are in: dispatching here is the signal to start work, not to wait for a further hand-off.

## Standing acceptance criteria

These apply to every code task — they are not written on the card, and the build gate checks all three automatically before this task can be handed on:

1. The project builds. A red build is not a finished task, whatever else is done.
2. The whole test suite passes — including tests you did not write. A test your change broke is your change's problem, not a pre-existing failure to report.
3. New or changed behaviour comes with a unit test that would fail without it, written in this run next to the project's existing tests and in its style. Pure config, copy or asset edits are the exception — say so in your closing message rather than inventing a test for them.

Ticking the task's own criteria while any of these three is unmet is a false claim: a red result after you stop sends the task back with your name on it.

Before you finish: build and test what you changed with run_terminal and read the output (list_component_checks names the project's own required checks); tick every acceptance criterion you satisfied with set_criterion_completed, in the same step that did the work. A criterion you did not build: build it now, or call cancel_criterion with the reason it is not being done — never tick it, and never leave it open: an open criterion refuses the hand-off and parks the task. The hand-off to code_review is automatic on a green build with a real diff — never plan a step for the move.
