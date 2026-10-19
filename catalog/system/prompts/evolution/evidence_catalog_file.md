---
key: evolution.evidence_catalog_file
version: 1
inputs: [Kind, ID, Name, Description, Enabled, Priority, Content]
---
# {{.Kind}}: {{.Name}}

id: {{.ID}}
enabled: {{.Enabled}}
{{if .Description}}description: {{.Description}}
{{end}}{{if .Priority}}priority: {{.Priority}}
{{end}}
{{.Content}}
