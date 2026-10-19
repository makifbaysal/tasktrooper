---
key: evolution.probe_skill_bodies
version: 1
inputs: [Skills]
---
These skills are already loaded for this work:
{{range .Skills}}
## Skill: {{.Name}}
{{.Content}}
{{end -}}
