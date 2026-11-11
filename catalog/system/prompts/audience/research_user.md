---
key: audience.research_user
version: 1
inputs: [Title, Report, Personas, Question, Language]
---
Study: {{.Title}}

Report:
{{.Report}}

Persona answers:
{{join "\n\n" .Personas}}

Question: {{.Question}}

Answer in {{if .Language}}{{.Language}}{{else}}English{{end}}.
