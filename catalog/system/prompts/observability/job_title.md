---
key: observability.job_title
version: 1
inputs: [Kind, Scope, Environment]
---
{{if eq .Kind "error_catalog"}}Error catalog from code{{else if eq .Kind "map_signals"}}Map signals{{else}}Define errors{{end}} · {{.Scope}} · {{.Environment}}
