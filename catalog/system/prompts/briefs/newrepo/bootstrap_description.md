---
key: briefs.newrepo.bootstrap_description
version: 1
inputs: [Name, Role, Description, Stack, Notes, Scaffold, HasDocs, Items, Docs, Design]
---
Set up the brand-new repository {{.Name}}. It was just created empty — an initial commit and nothing else — so there is no existing code to read or follow: what this task writes sets the conventions for everything after it.

## What the person asked for

Their answers, verbatim:

**Description**
{{if .Description}}{{.Description}}{{else}}(not given){{end}}

**Role**
{{if .Role}}{{.Role}}{{else}}(not given){{end}}

**Stack**
{{if .Stack}}{{.Stack}}{{else}}(not given){{end}}

**Notes**
{{if .Notes}}{{.Notes}}{{else}}(not given){{end}}

{{if not .Stack}}No stack was named: choose a mainstream, well-supported stack for a {{.Role}} project that fits the description, and state the choice and the reason in the README.

{{end}}## What to deliver

Do ALL of it on a single branch, in exactly one pull request. Do not open a pull request per item and do not stop after the first one — the task is finished when every item below exists and is correct.

{{range .Items}}{{.Number}}. {{if eq .Kind "skeleton"}}the initial project skeleton in the repository root{{else if eq .Kind "doc"}}`{{.Path}}` — {{.KindLabel}}{{else if eq .Kind "design"}}the project's design system: `DESIGN.md`, `design/tokens.json`, `design/tokens.css` and the theme built from them{{else}}`CLAUDE.md` and `AGENTS.md` at the repository root{{end}}
{{end}}{{if .Scaffold}}
---

## The project skeleton

Create the initial project skeleton for this stack in the repository root: minimal but runnable — it installs, builds and starts (a library: builds and runs its tests) with the stack's standard commands — plus a README that says what the project is and how to install, run and test it. Nothing beyond what proves it runs.
Before writing it by hand, call search_boilerplate_catalog with the stack and the role; when a starter there fits, build on it instead of starting from scratch, and say in the pull request which one you used.
{{if .HasDocs}}The skeleton must already follow every convention the documents below prescribe.
{{end}}{{end}}{{range .Docs}}
---

## `{{.Path}}`

{{.Instructions}}{{end}}{{if .Design}}
---

## The design system

This repository joins the {{.Design.ProjectName}} project, whose design system v{{.Design.Version}} is approved. Call `get_design_system` with `files: true` and write every returned file at its path exactly as given. Then build the stack's theme from `design/tokens.css` / `design/tokens.json` — Tailwind `@theme` or CSS variables on the web, `Theme.swift` and the asset catalog on iOS, the Compose or Flutter theme on Android — so the skeleton uses the design system's tokens from its first commit and never a raw color, font size, spacing or radius. Link `DESIGN.md` from the docs index below.
{{end}}
---

## `CLAUDE.md` and `AGENTS.md`

Create both at the repository root, or refresh them if the skeleton already produced one: a one-paragraph summary of what this project is, then a short docs index linking every document written in this pull request{{if .Scaffold}} and the README{{end}}, so every agent that opens this repository finds them. Keep the two files' content the same.

