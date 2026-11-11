---
key: readiness.guide
version: 1
inputs: [Repository, Kind, Missing, Components]
---
{{.Repository}} was just imported into TaskTrooper and is missing {{if eq (len .Missing) 2}}both a README and a guide for coding agents{{else}}{{range .Missing}}{{if eq . "readme"}}a README{{else}}a guide for coding agents (`AGENTS.md`){{end}}{{end}}{{end}}. Write {{if eq (len .Missing) 2}}them{{else}}it{{end}} from what the code actually does; read the code, the build files and the CI configuration first, and do not describe anything you did not verify.
{{if .Components}}
What TaskTrooper's scan found:
{{range .Components}}
- `{{.Path}}`: {{.Role}}{{if .Stack}}, {{.Stack}}{{end}}{{range .Commands}}
  - {{.}}{{end}}{{end}}
{{end}}
{{range .Missing}}{{if eq . "readme"}}
## README.md

- What the project is and who it is for, in the first paragraph.
- How to install, run, test and build it, using the commands above that you confirmed work.
- Where the code lives (one line per top-level directory) and where to read more.
{{else}}
## AGENTS.md

`AGENTS.md` is the single guide for every coding agent (Claude Code, OpenCode, Codex, Cursor, Gemini and the rest), so write the whole guide there. Do not create `CLAUDE.md` or any other per-tool copy.

- The same facts a new engineer needs on day one, written for a coding agent: the layout, the exact commands to build, test and lint, and the conventions the existing code follows (naming, error handling, where tests live).
- The rules a change must not break (generated files not to edit, migrations that cannot be rewritten, secrets that must stay out of the tree).
- Keep it short: {{if eq (len $.Missing) 2}}link to the README you are writing{{else}}link to the existing README{{end}} and to deeper docs instead of repeating them.
{{end}}{{end}}
## Done when

- {{if eq (len .Missing) 2}}`README.md` and `AGENTS.md` exist at the repository root{{else}}{{range .Missing}}{{if eq . "readme"}}`README.md`{{else}}`AGENTS.md`{{end}}{{end}} exists at the repository root{{end}}, and every command {{if eq (len .Missing) 2}}in them{{else}}in it{{end}} was run by you and works.
- {{if eq (len .Missing) 2}}Nothing other than those two files changed.{{else}}{{range .Missing}}{{if eq . "readme"}}Nothing other than `README.md` changed.{{else}}Nothing other than `AGENTS.md` changed; the README is left as it is.{{end}}{{end}}{{end}}
