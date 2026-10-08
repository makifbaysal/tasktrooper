---
key: briefs.designsystem.project_task
version: 2
inputs: [ProjectName, ProjectID, ProjectDescription, Repositories, CurrentVersion, Notes]
---
{{if .CurrentVersion}}Update the design system of the **{{.ProjectName}}** project. Its approved base is v{{.CurrentVersion}}: read it with `get_design_system` (project_id `{{.ProjectID}}`) before anything else and propose the change as the next version.{{else}}Derive the design system of the **{{.ProjectName}}** project from the code that already exists. The project has no design system yet: what the repositories below already do IS the starting point — read it, do not invent a new look.{{end}}
{{if .ProjectDescription}}
Project: {{.ProjectDescription}}
{{end}}
Repositories in this project:
{{range .Repositories}}- **{{.Name}}** ({{.Kind}}) — repository_id `{{.ID}}`{{if .RootPath}}, `{{.RootPath}}`{{end}}{{if .LayerVersion}}, has its own approved layer v{{.LayerVersion}}{{end}}
{{end}}
Work in this order:

1. **Read every repository above** that has a UI: theme and token files (Tailwind config or `@theme`, CSS variables, `globals.css`), the component library, fonts, and on mobile `Theme.swift`, `colors.xml`/`themes.xml`, Compose or Flutter `ThemeData`. Note each value with the file it came from.
2. **Find what they share.** That is the project base: semantic color tokens for both themes, the type scale and font families, spacing, radius, elevation, motion. Name tokens by role (`color.primary`, `color.surface`), never by hue.
3. **Sort every difference** into one of two kinds:
   - *deliberate* — the repository is meant to look different (a marketing site, a platform convention such as 44pt touch targets on iOS). Propose it as that repository's layer, with the reason.
   - *drift* — the same thing hand-copied and then changed. Put the base value in the base, and list the drift per repository in your report so implementation tasks can fix it.
4. **Propose** with `propose_design_system`: once with `scope: "project"` for the base (tokens as a DTCG tree, `DESIGN.md`, component inventory), then once per repository that needs a layer with `scope: "repository"` and a `rationale`. A layer only adds or overrides tokens; it never removes one. Every result carries `lint`: fix each `error` finding and propose again.
5. **Attach one self-contained HTML report** with `add_task_document` (`format: "html"`), titled `design system: {{.ProjectName}} v<version>`: color swatches for both themes, the type scale, spacing and radius samples, the main components, and a table per repository of what it uses today, what the base says and whether the difference is a layer or drift. Use inline CSS and SVG only — no scripts, no form controls, no external fonts. Serve it on loopback and save a screenshot of it with `browser_screenshot` `attach_to_task: true`, so the human sees the palette on the task.

Then stop. The task moves to `analiz_review` on its own; the human approves the design system there, and approving the task approves every version it proposed.
{{if .Notes}}
Notes from the human:
{{.Notes}}
{{end}}
