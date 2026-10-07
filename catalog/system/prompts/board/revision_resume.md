---
key: board.revision_resume
version: 1
inputs: [Task, Trigger, Feedback]
---
{{.Task}} came back to you for revision. This is the same session and the same workspace you worked in before: your branch, your changes and your earlier reasoning are all still here, so build on them instead of starting over. Address every point in the feedback below, then reply with a short summary of what you changed for each.

{{.Trigger}}
{{- range .Feedback}}

{{.}}
{{- end}}
