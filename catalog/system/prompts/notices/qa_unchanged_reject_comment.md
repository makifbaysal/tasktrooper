---
key: notices.qa_unchanged_reject_comment
version: 1
inputs: [Column, ShortID, Rejections]
---
Not re-running {{.Column}}: the diff is identical (patch-id {{.ShortID}}) to the one QA already rejected, and no person has weighed in since. Testing the same code again can only repeat the same verdict, so the task is parked for a decision instead of going back and forth.

QA's rejections on this diff:
{{range .Rejections}}- {{.}}
{{end}}
If the code really needs a change, move the task to need_revision with what to fix. If QA could not test something because of the environment (a missing integration, credential, device or stage), fix that or tell QA how to verify it, then move the task back to {{.Column}}. Moving it yourself, or a comment from you, lets QA run again.
