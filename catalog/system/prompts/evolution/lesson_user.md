---
key: evolution.lesson_user
version: 1
inputs: [AgentName, Changes, Variants, Adopted, Cases]
---
Agent: {{.AgentName}}

## Edits
{{range .Changes}}- {{.Kind}} {{.Action}} "{{.Name}}" ({{.Dimension}}): verdict {{.Verdict}}{{if .Adopted}}, adopted{{end}}
  why it was proposed: {{.Reason}}
{{end}}
## Variants rehearsed against the current setup
{{range .Variants}}- {{.Label}} ({{.Changes}} edits): {{.Wins}} better, {{.Losses}} worse, {{.Ties}} even{{if .GoldenLosses}}, {{.GoldenLosses}} golden task(s) that passed before and failed with it{{end}}
{{end}}
Adopted: {{if .Adopted}}{{.Adopted}}{{else}}nothing, the current setup stays{{end}}

## Cases
{{range .Cases}}- [{{.Kind}}] {{.Title}}{{if not .Measured}}: not measured{{end}}
{{range .Notes}}  - {{.Label}}: {{if .Golden}}{{if .Passed}}passed{{else}}failed{{end}}{{else}}{{.Score}}/10, {{.Reason}}{{end}}
{{end}}{{end -}}
