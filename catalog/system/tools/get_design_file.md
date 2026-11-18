---
key: tool.get_design_file
version: "1"
params:
    task_id: The design task's id. Defaults to the run's own task.
    repository_id: The task's repository. Defaults to the run's own repository.
---
The FigCraft file this design task draws in, ready to open with the figcraft MCP tools. `task_file` is the task's own file: the first call copies the design system file it builds on into it (`seeded_from` — the repository's layer file when there is one, else the project's base file), so the Foundations page, styles, variables and components are already there; when neither exists the file does not exist yet (`task_file_exists: false`) and you create it at exactly that path. Draw only in `task_file` and save it there: the human reviews it on the task, opens it in FigCraft, and when a design-system task is approved it becomes `project_design_system_file` or `repository_design_system_file` automatically — never write those two yourself. `figcraft_installed: false` means FigCraft is not on this computer: fall back to the HTML document, and say in your summary comment that FigCraft installs in one click from Settings → Development environment (Design).
