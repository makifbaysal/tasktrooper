---
key: evolution.evidence_trajectories
version: 1
inputs: [Runs, Files]
---
## How recent runs went
{{if .Files}}When runs/ is in your working directory, it holds the full step-by-step record of each run.
{{end}}{{range .Runs}}
### {{.Title}} ({{.Status}})
{{if .Errors}}Tool errors:
{{bullets .Errors}}
{{end}}{{if .Final}}Last message: {{.Final}}
{{end}}{{end}}
