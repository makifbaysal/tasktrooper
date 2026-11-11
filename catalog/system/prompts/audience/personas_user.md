---
key: audience.personas_user
version: 1
inputs: [Name, Count, Product, Context, SegmentLines, Locations, CurrentSolutions, Pricing, Topic, Language, Existing]
---
Audience: {{.Name}}
Create exactly {{.Count}} new people.

The product, as its owner describes it and as it stands today:
{{.Product}}
{{- if .Context}}

Project context (what actually exists in the project; trust it over the description above where they differ):
{{.Context}}
{{- end}}
{{- if .SegmentLines}}

Segments, and how many of the people to create in each:
{{join "\n" .SegmentLines}}
{{- end}}
{{- if .Locations}}

Where they live and work: {{.Locations}}
{{- end}}
{{- if .CurrentSolutions}}

What people use today instead: {{.CurrentSolutions}}
{{- end}}
{{- if .Pricing}}

Current or planned pricing: {{.Pricing}}
{{- end}}
{{- if .Topic}}

What the research is for: {{.Topic}}
{{- end}}

Language of the people: {{if .Language}}{{.Language}}{{else}}English{{end}}.
{{- if .Existing}}

These people already exist in the audience; create different ones (other names, jobs, places, attitudes):
{{join "\n" .Existing}}
{{- end}}

Reply with only the JSON object.
