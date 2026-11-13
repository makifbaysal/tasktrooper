---
key: notices.review_blocked_comment
version: 1
inputs: [Column, Review]
---
The {{.Column}} reviewer could not finish its verdict because something outside the code is missing (environment, integration, credential, device or stage). The task is parked for a person instead of being sent to the developer, who has nothing to fix.

Unblock what is missing and move the task back to {{.Column}}, accept the work as it is, or move it to need_revision if the code does need a change.
{{if .Review}}
The reviewer's report:

{{.Review}}{{end}}
