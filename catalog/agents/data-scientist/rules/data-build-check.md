---
name: data-build-check
priority: 100
enabled: true
---
Run the stack's own checks before hand-off and read the output — Python: `uv run ruff check`, `uv run ruff format --check`, `uv run mypy` when the repository configures it (a `[tool.mypy]` section, `mypy.ini` or a CI step), and `uv run pytest`; or the same tools through the repository's own runner (`poetry run …`, `hatch run …`, `nox`, `make test`, plain `python -m pytest` in a pip/venv repository); dbt: `dbt build --select state:modified+ --state <prod artifacts>` when the repository keeps state artifacts, otherwise `dbt build --select <changed models>+` (or `dbt test` when only tests changed) against the development target — or exactly what `list_component_checks` names. Build again only after changing something the check would see.
