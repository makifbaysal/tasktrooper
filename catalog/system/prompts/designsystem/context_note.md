---
key: designsystem.context_note
version: 1
inputs: [ProjectName, BaseVersion, LayerVersion, LayerRationale, Excerpt, Truncated, Ambiguous, ShowTool]
---
## Design system (TaskTrooper)
{{if .Ambiguous}}This repository belongs to several projects that have a design system and none is chosen as its base, so only its own layer applies until one is chosen on the repository's Design System tab.
{{end}}This repository follows {{if .BaseVersion}}the **{{.ProjectName}}** design system {{.BaseVersion}}{{if .LayerVersion}} with its own repository layer {{.LayerVersion}}{{end}}{{else}}its own design system {{.LayerVersion}}{{end}}. Build with its tokens and components only: never introduce a color, font size, spacing, radius or shadow outside it. When a change needs something it does not have, say so in your hand-off — the designer changes the design system through a design task, not your diff.
{{if .LayerRationale}}Why this repository differs from the base: {{.LayerRationale}}
{{end}}{{if .ShowTool}}The merged tokens, the full DESIGN.md and the component inventory: `get_design_system`.
{{end}}
{{.Excerpt}}{{if .Truncated}}
…{{if .ShowTool}} (truncated — `get_design_system` has the rest){{end}}{{end}}
