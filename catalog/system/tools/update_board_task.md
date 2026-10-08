---
key: tool.update_board_task
version: "1"
params:
    acceptance_criteria: Replaces the task's acceptance-criteria checklist with this list, one observable Given/When/Then per item. Send the FULL list — anything omitted is deleted and completion state is reset. Omit the argument entirely to leave criteria untouched.
    after_deploy: 'Post-deploy steps (markdown): cache warms, flag flips, smoke checks. Posted on the task automatically when the production deploy succeeds.'
    before_deploy: 'Pre-deploy checklist (markdown): what must be true or done before this ships. Posted on the task automatically when the release is dispatched — do not write it as a comment.'
    blocked_by: 'Work ordering: task UUIDs or board keys that must be FINISHED (done or released; a design task only once released, after its designer wrote the hand-off) before anyone starts this task — THIS task waits for them, and an approved design task among them hands this task its documents. ADDS to the task''s existing blockers rather than replacing them, so send only the ones you are adding. Enforced: while any blocker is open the task is parked in `blocked` instead of being dispatched, and it is picked up automatically when the last one lands. A cycle is refused.'
    column: 'Put the task in this board column. For workflow moves prefer move_board_task; this is for sending your own task back to todo in the same call that adds the blocker it now waits for.'
    component: 'For a monorepo: re-scope this task to a different component, by its repository-relative path (e.g. "services/api"); "." means the repository root. Omit to leave the task''s current component alone.'
    deploy_depends_on: 'Deploy ordering: task UUIDs or board keys (e.g. ["T-1"]) that must be LIVE IN PRODUCTION before this task may be released — THIS task ships AFTER them. Replaces the task''s existing deploy dependencies — send the FULL list; [] clears them, omitting the argument leaves them untouched. This is enforced: releasing this task is refused while any of them is unreleased, and the ordering is regenerated into this task''s before_deploy runbook. Use it for real shipping order (the API before the client that calls it), not for who codes first — that is blocked_by. A cycle is refused.'
    description: 'Product-level description (markdown): user story, context, out-of-scope. Acceptance criteria and technical detail have their own fields — do not paste them here.'
    project: 'File this task under an initiative: project name or UUID. Use list_projects for valid names.'
    rollback_plan: How to undo this change if production breaks (markdown). Posted alongside the pre-deploy checklist when the release is dispatched.
    task_id: Board task UUID or its board key (e.g. "T-1" for a task, "B-1" for a bug, "A-1" for an analysis).
    technical_description: 'Technical detail (markdown): affected endpoints/files/schema, approach, constraints. The only place technical detail belongs.'
---
Update a board task. Deploy ordering goes in deploy_depends_on (this task ships AFTER those), and anything that has to happen around the deploy goes in before_deploy / after_deploy / rollback_plan — not in a comment. Those fields are read back and posted automatically when the release is dispatched and when it lands, so a checklist written as a comment is one nobody will see at deploy time.
