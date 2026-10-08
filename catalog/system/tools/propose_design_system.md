---
key: tool.propose_design_system
version: "1"
params:
    design_md: 'The DESIGN.md text: Overview, Colors, Typography, Layout, Elevation & Depth, Shapes, Components, Do''s and Don''ts. Required for a project base.'
    inventory_md: The component inventory as markdown — each component, its variants and states, and where it lives in the code.
    project_id: The project whose base this is. Required when scope is project.
    rationale: Why this repository differs from the project base. Required when scope is repository.
    repository_id: The repository whose layer this is. Required when scope is repository.
    scope: '"project" for a project''s base design system, "repository" for one repository''s layer on top of it (or its whole design system when it has no project base).'
    tokens: The design tokens as a DTCG tree — groups are objects, a token is an object with "$value" (and optionally "$type"). A repository layer only adds or overrides tokens; it never sets one to null.
---
Propose a new version of a project's base design system or of a repository's layer, from the design task you are working. It stays pending until the human approves the task in analiz_review, which approves every version the task proposed. Calling it again for the same target from the same task replaces that pending version rather than adding another — that is how you revise it. Read the current one with get_design_system first. The result carries `lint` — the same checks get_design_system reports; fix every `error` finding and propose again before you attach the report.
