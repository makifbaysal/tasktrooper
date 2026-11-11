---
key: readiness.tests
version: 1
inputs: [Repository, Components]
---
TaskTrooper's scan of {{.Repository}} found {{if eq (len .Components) 1}}a component that cannot be checked by an automated test run{{else}}{{len .Components}} components that cannot be checked by an automated test run{{end}}. Agents change this code without a person watching every line, so a test run that fails when behaviour breaks is what keeps their work honest.

Use the test framework the stack already points to (or the one the repository already uses elsewhere); do not introduce a second one. Start with the behaviour that is easiest to get wrong, not with trivial getters, and keep the first set small enough to run in under a minute.
{{range .Components}}
## `{{.Path}}`

- Role: {{.Role}}{{if .Stack}}, stack: {{.Stack}}{{end}}
- {{if .TestCommand}}Test command on record: `{{.TestCommand}}`{{else}}No test command is on record; add one (a script or a make target) and make it the component's test command.{{end}}
{{end}}
## Done when

- Each component above has a command that runs its tests, it passes on a clean checkout, and it fails when you break the behaviour a new test covers (check that once, then revert the break).
- The command is documented where the project documents its other commands.
- No production behaviour changed.
