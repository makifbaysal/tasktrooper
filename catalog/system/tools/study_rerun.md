---
key: tool.study_rerun
version: "1"
params:
    study_id: 'The id of a finished study to run again.'
    changes: 'What changed in the product since that study, in a few plain sentences (what the person did in answer to its findings). It is shown to the people as "what changed".'
---
Plan the same study again after the product changed, so the new report can be compared with the old one: same audience and people, same kind, goal, prices and interview script, plus the note of what changed. Only a study with a finished report can be run again. This only plans it (nothing is spent): the script waits for the person's OK again, then study_script with approve and study_start with `user_approved: true` run it like any study, after they said yes and you stated the number of model calls. When the new study is done, call study_compare to show what moved. Rebuild nothing in between: the same people must answer both, so do not run audience_build on the audience first.
