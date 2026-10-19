---
key: evolution.evidence_trajectories
version: 1
inputs: [Runs, Files]
---
## How recent runs went
{{if .Files}}The full step-by-step record of each run is in runs/ in your working directory.
{{end}}{{range .Runs}}
### {{.Title}} ({{.Status}})
{{if .Errors}}Tool errors:
{{bullets .Errors}}
{{end}}{{if .Final}}Last message: {{.Final}}
{{end}}{{end}}
