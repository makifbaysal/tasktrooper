---
key: tool.get_design_system
version: "1"
params:
    files: true also returns `files` — DESIGN.md, design/tokens.json, design/tokens.css and design/INVENTORY.md rendered from the effective design system, each with its path and full content, to write into the repository exactly as given.
    project_id: Project UUID. Returns that project's approved base, its pending proposals and the layer of each of its repositories instead of one repository's view.
    repository_id: Repository UUID. Defaults to the run's own repository.
---
Read the design system a repository follows: the project base it builds on, the repository's own layer, and the merged DTCG token tree (`tokens`) — the values to build with. `base` and `layer` carry `design_md` (principles, do's and don'ts), `inventory_md` (the components) and their own tokens. Both null means the repository has no design system yet. Pass `project_id` to read a project's base and its repositories' layers instead. Each version carries `lint`: problems found in it — `unresolved_alias`, `invalid_color`, `contrast_below_aa` (a color and its `-foreground`/`on-` pair below 4.5:1, with the `ratio`), `empty_group`, `missing_section` (a DESIGN.md section the base lacks).
