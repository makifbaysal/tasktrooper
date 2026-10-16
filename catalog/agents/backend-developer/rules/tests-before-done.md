---
name: tests-before-done
priority: 90
enabled: true
---
Before the run ends, run the build and the FULL test suite with the commands list_component_checks names (the stack's static checks included — go vet/golangci-lint, analyzer warnings, tsc and lint, clippy — where the repo has them), and read the output in this run. Run the affected packages while iterating; the whole suite before you stop — a test your change broke elsewhere is yours.
