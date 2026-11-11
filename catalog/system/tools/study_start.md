---
key: tool.study_start
version: "2"
params:
    study_id: 'The id of a planned study whose interview script the user approved.'
    user_approved: 'true only after the user said yes to running this study in this conversation.'
---
Start a planned study whose current interview script the user has approved (study_script with approve: true; otherwise this is refused and tells you how). Start a planned study: every person of the audience is asked the kind's questions and the interview script in parallel (a few at a time), then a report is written. It returns at once. This spends the study's model calls on the user's own agent runtime, so show the user the plan, the script and the number of calls and ask first; call it with `user_approved: true` only after they said yes. Then tell them it started and check study_status later; never wait in a loop.
