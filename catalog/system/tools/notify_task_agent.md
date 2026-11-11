---
key: tool.notify_task_agent
version: "1"
params:
    interrupt: 'Stop the current pass of the agent and start it again with the change. Needs user_approved. Leave it out unless the stakeholder wants the work stopped now.'
    summary: 'What changed, in a few sentences (for example: added criterion that the export includes archived rows; the description now says CSV, not JSON). Not the whole task.'
    task_id: Board task UUID or its board key (e.g. "T-1").
    user_approved: 'true only after the stakeholder said yes to interrupting the agent in this conversation.'
---
Tell the agent working a task about a change the stakeholder just asked for. First make the change on the task itself with update_board_task (description, technical description, acceptance criteria), then call this so the agent that is working it right now hears about it. Use it when the task has a run in progress; it is also safe when none is, because the change is recorded on the task and the next run reads it. Do not create a second task for the same work. The result says what happened — agent notified (it picks the change up when its current pass ends, before it hands the task on), interrupted, or stored for the next run — and you tell the stakeholder that, in plain words. Only an interrupt discards work, so only an interrupt needs the stakeholder's yes.
