---
key: guard.observability_outside_run_repository
version: 1
inputs: [ToolName, RepositoryID]
---
{{.ToolName}}: repository_id names another repository than {{.RepositoryID}}, which this run is bound to; cross-repository reads and proposals are refused
