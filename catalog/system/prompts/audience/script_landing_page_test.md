---
key: audience.script_landing_page_test
version: 1
inputs: [Goal, Intent, Page, Language, Segments]
---
Kind of study: landing page test. Each person reads the page below as a first-time visitor who came with the intent given, and is also asked scored questions about first impression, clarity and whether they would act.
Language: {{if .Language}}{{.Language}}{{else}}English{{end}}.

Research goal: {{if .Goal}}{{.Goal}}{{else}}Find out how well the page works for this audience and what to change.{{end}}
Visitor intent: {{.Intent}}

The page (plain text):
---
{{.Page}}
---
{{- if .Segments}}

The people who will read it:
{{join "\n" .Segments}}
{{- end}}

Write the interview script: what they expected before arriving, what they looked for on the page, which parts they trusted or doubted, what they would need to see before acting.
