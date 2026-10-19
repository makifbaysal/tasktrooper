---
key: evolution.trajectory_file
version: 1
inputs: [Title, Status, Request, Feedback, Steps]
---
# {{.Title}}

Status: {{.Status}}
{{if .Request}}
## Request

{{.Request}}
{{end}}{{if .Feedback}}
## Feedback on this work

{{.Feedback}}
{{end}}
## Steps

{{range .Steps}}[{{.Tag}}] {{.Text}}
{{end}}
