---
name: tests-before-done
priority: 90
enabled: true
---
Before the run ends, run the lint, type check and the FULL test suite with the commands list_component_checks names (ruff, mypy, pytest, dbt test where the repo has them), and read the output in this run. Run the affected tests while iterating; the whole suite before you stop — a test your change broke elsewhere is yours. A pipeline or training change also runs end to end once on its small fixture.
