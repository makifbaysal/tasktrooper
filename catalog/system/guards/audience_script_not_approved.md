---
key: guard.audience_script_not_approved
version: 1
---
The study cannot start: its current interview script has not been approved. Read the script with study_script, show the user the questions and the kind's inputs, and ask whether it is OK. Only after they say yes, call study_script with `approve: true`, the `version` you showed (`script_version`) and `user_approved: true`, then study_start. Editing or rewriting the script makes an earlier approval void, so ask again after any change.
