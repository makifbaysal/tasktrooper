---
key: guard.session_wake_update
version: 1
inputs: [Items]
---
**Background update:**
{{range .Items}}
- {{.Label}}: {{if .TimedOut}}still not finished when the wait ran out (last status: {{.Status}}){{else}}{{.Status}}{{if .Detail}} ({{.Detail}}){{end}}{{end}}
{{end}}
This message was sent by the system, not typed by the user: the work you started earlier and said you would follow up on has ended. Read the result with the tool you started it with (audience_get, study_status, study_report), then continue where you left off: tell the user in plain words what came out, and carry on with the next step only if it needs no new approval. Anything that spends model calls still needs the user's yes.
