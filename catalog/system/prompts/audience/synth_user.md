---
key: audience.synth_user
version: 1
inputs: [Title, Goal, Product, Language, Currency, Stats, Segments, Personas, Questions]
---
Study: {{.Title}}
{{- if .Goal}}
Goal: {{.Goal}}
{{- end}}
Language of the report: {{if .Language}}{{.Language}}{{else}}English{{end}}. Prices are in {{.Currency}}.

The product that was judged:
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
