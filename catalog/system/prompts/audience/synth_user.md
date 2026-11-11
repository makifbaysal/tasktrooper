---
key: audience.synth_user
version: 2
inputs: [Title, Goal, Kind, Product, Language, Currency, Stats, Segments, Personas, Questions]
---
Study: {{.Title}}
{{- if .Goal}}
Goal: {{.Goal}}
{{- end}}
Language of the report: {{if .Language}}{{.Language}}{{else}}English{{end}}. {{if .Currency}}Prices are in {{.Currency}}.{{end}}

{{if eq .Kind "landing_page_test"}}The page that was tested{{else if eq .Kind "message_testing"}}The candidate lines that were tested{{else if eq .Kind "ab_test"}}The two pages that were compared{{else}}The product that was judged{{end}}:
{{.Product}}

Computed results (do not change them):
{{.Stats}}
{{- if .Segments}}

Segments:
{{join "\n" .Segments}}
{{- end}}

Every persona's answers (id, who they are, what they said):
{{join "\n\n" .Personas}}
{{- if .Questions}}

Extra questions that were asked; give a short digest of what people answered to each in "open_questions":
{{join "\n" .Questions}}
{{- end}}

Reply with only the JSON object.
