---
key: tool.study_script
version: "1"
params:
    approve: 'true to record the user''s OK to the script version you showed them (needs version and user_approved).'
    regenerate: 'true to have the script written again (one model call; needs user_approved). It voids any earlier approval.'
    sections: 'Replace the script with these grouped questions: [{title, questions[]}], at most 8 sections and 12 questions. Free; use it for the stakeholder''s own edits.'
    study_id: 'The id of a planned study that has not started.'
    user_approved: 'true only after the user said yes in this conversation: to approving this script version (with approve) or to writing it again (with regenerate).'
    version: 'The script_version you showed the user, for approve. An approval of an older version is refused.'
---
Read, edit or rewrite the interview script of a planned study. With only `study_id` it returns `script_state` (generating, ready or failed) and the script: sections of open questions every person will be asked on top of the kind's built-in scored questions. With `sections` it replaces the script with the stakeholder's edits (no model call). With `regenerate: true` and `user_approved: true` it has the script written again (one model call). Every script waits for the user's approval: `script_state` is `awaiting_approval` until they say OK, and nothing ever runs on its own after the script is written. Show the stakeholder the script (the questions, and the kind's inputs) and ask whether it is OK. Only after they say yes, call this with `approve: true`, the `version` you showed (`script_version`) and `user_approved: true`; the state becomes `approved` and `study_start` can run it. Any edit (`sections`), rewrite or change of the study's inputs makes an earlier approval void and a new version needs a new OK. If the script failed, regenerate it or pass your own `sections`.
