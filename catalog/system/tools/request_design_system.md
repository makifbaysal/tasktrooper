---
key: tool.request_design_system
version: "1"
params:
    notes: What the design system task should take into account, passed to the designer verbatim.
    project_id: The project whose base design system to derive or update. Required when scope is project.
    repository_id: The repository whose layer to derive or update. Defaults to the run's own repository.
    scope: '"project" for a project''s base (and the layers its repositories need), "repository" for one repository''s layer — or its whole design system when it has no project base.'
---
Open the design task that derives a design system from the code that already exists — or updates it — exactly as the Design System tab's button does; the ui-designer is assigned automatically. While one is still open for the same target it is returned instead of opening another (`created: false`). Use it when work needs a design system that `get_design_system` does not return; then make the work that needs it wait with `update_board_task` `blocked_by: ["<returned key>"]`.
