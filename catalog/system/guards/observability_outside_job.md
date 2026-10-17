---
key: guard.observability_outside_job
version: 1
inputs: [ToolName, Case, RepositoryID, Component]
---
{{.ToolName}} runs inside an AI job scoped to repository {{.RepositoryID}}{{if .Component}}, component `{{.Component}}`{{end}}; {{if eq .Case "repository"}}another repository{{else if eq .Case "component"}}another component{{else}}a data source only other repositories use{{end}} is outside that scope and refused — work within the job's own scope
