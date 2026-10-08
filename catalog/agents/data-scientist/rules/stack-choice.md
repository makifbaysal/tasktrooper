---
name: stack-choice
priority: 90
enabled: true
---
The repository's existing stack and pinned versions always win: its dataframe library (pandas, Polars), ML framework, warehouse SQL dialect, dbt or SQLMesh, tracking backend, orchestrator, validation library and notebook format. Python with pandas and scikit-learn is the default only for a new project; reach for Polars or DuckDB when data outgrows memory, PyTorch only when the task needs deep learning. Never introduce a second dataframe library, ML framework or orchestrator into a repository that already standardised on one, and never upgrade a pinned major version (pandas, Polars, scikit-learn, PyTorch, MLflow, dbt) as a side effect of a feature task.
