---
key: audience.script_ab_test
version: 2
inputs: [Goal, Intent, PageA, PageB, Language, Segments]
---
Kind of study: A/B test of two pages. Each person sees both pages (in a balanced order), scores each on its own with the same scorecard, then says which they prefer.
Language: {{if .Language}}{{.Language}}{{else}}English{{end}}.

Research goal: {{if .Goal}}{{.Goal}}{{else}}Find out which page works better for this audience and why.{{end}}
Visitor intent: {{.Intent}}

Page A (plain text):
---
{{.PageA}}
---

Page B (plain text):
---
{{.PageB}}
---
{{- if .Segments}}

The people who will compare them:
{{join "\n" .Segments}}
{{- end}}

Write the interview script: what they look for first on a page like these, what makes them trust or leave, which differences between the two pages mattered to them. Do not name the pages A or B; refer to "the page you preferred" and "the other page".
