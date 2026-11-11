---
key: tool.study_start
version: "1"
params:
    study_id: 'The id of a planned study.'
    user_approved: 'true only after the user said yes to running this study in this conversation.'
---
Start a planned study: every person of the audience is asked the same questions in parallel (a few at a time), then a report is written. It returns at once. This spends the study's model calls on the user's own agent runtime, so show the user the plan and the number of calls and ask first; call it with `user_approved: true` only after they said yes. Then tell them it started and check study_status later; never wait in a loop.
