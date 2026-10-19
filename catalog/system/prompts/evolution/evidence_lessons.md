---
key: evolution.evidence_lessons
version: 1
inputs: [Lessons]
---
## Lessons from earlier tested changes
Each was distilled from a change set that was rehearsed against this agent's setup. Build on them, and do not repeat an edit a lesson already shows did not help.
{{range .Lessons}}- {{.Cue}} → {{.Lesson}} (confidence {{.Confidence}}, {{.Date}}; evidence: {{.Evidence}})
{{end}}
