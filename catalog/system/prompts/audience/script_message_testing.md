---
key: audience.script_message_testing
version: 1
inputs: [Goal, Variants, Product, Language, Segments]
---
Kind of study: message testing. Each person reads the candidate lines below and rates each one on clarity, relevance, credibility and appeal, picks the best and writes a stronger line.
Language: {{if .Language}}{{.Language}}{{else}}English{{end}}.

Research goal: {{if .Goal}}{{.Goal}}{{else}}Find out which words land with this audience and why.{{end}}

What the lines are for:
{{.Product}}

The candidate lines:
{{join "\n" .Variants}}
{{- if .Segments}}

The people who will read them:
{{join "\n" .Segments}}
{{- end}}

Write the interview script: how they describe this problem in their own words, which words they use or avoid, what they would expect a claim like these to be backed by.
