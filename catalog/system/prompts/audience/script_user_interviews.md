---
key: audience.script_user_interviews
version: 1
inputs: [Goal, Product, Language, Segments]
---
Kind of study: user interviews. Each person is asked whether they would buy this product as it exists today.
Language: {{if .Language}}{{.Language}}{{else}}English{{end}}.

Research goal: {{if .Goal}}{{.Goal}}{{else}}Find out whether these people would pay for the product and what stands in the way.{{end}}

The product, as it stands today:
{{.Product}}
{{- if .Segments}}

The people who will be interviewed:
{{join "\n" .Segments}}
{{- end}}

Write the interview script: how they handle this problem today, what they think of the product, what would make them buy or not.
