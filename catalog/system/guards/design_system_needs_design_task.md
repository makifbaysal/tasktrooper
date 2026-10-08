---
key: guard.design_system_needs_design_task
version: 1
inputs: [ToolName]
---
{{.ToolName}} records a proposal for the design task this run is working, and this run is not working one. Only a design task (task_type=design) proposes a design system, because approving that task is what approves the proposal: create a design task for the change, or say in your hand-off what the design system is missing.
