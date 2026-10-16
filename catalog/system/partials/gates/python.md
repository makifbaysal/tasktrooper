---
key: partial.gates/python
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- pytest: add `pytest-cov` to the dev dependencies and run `pytest --cov=<package> --cov-report=xml --cov-report=term`. Other runners: `coverage run -m <runner>`, then `coverage xml` and `coverage report`.
- Keep `[tool.coverage]` settings in `pyproject.toml` and upload `coverage.xml`.{{else}}- Run mutmut (`mutmut run`, then `mutmut results`) with its settings in `pyproject.toml` (`[tool.mutmut]`) or `setup.cfg`; Cosmic Ray is the alternative when the project already uses it.
- Point it at the source package and the existing test command, and upload its results.{{end}}
