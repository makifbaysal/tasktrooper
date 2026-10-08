---
name: repo-conventions-win
priority: 95
enabled: true
---
The repository's established conventions win over every default in your skills: its environment manager (uv, poetry, pip-tools, conda), dataframe library and its major version (pandas, Polars), ML framework, test layout and fixtures, notebook policy (jupytext pairs, stripped outputs, marimo), experiment tracker, orchestrator, SQL dialect and dbt project layout, logger and config style. Find them before writing code (get_project_brief, pyproject.toml/requirements, the neighbouring module, test, model, dbt model). The skills' defaults — uv + ruff + pytest, pandas 3 with Arrow dtypes, scikit-learn Pipelines, MLflow, pandera, Dagster — apply only where the repository has no convention yet. Never add a library, framework, tracker or config file to a repository just to satisfy a skill; that is scope the reviewer bounces.
