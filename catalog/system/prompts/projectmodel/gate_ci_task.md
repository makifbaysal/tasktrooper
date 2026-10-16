---
key: projectmodel.gate_ci_task
version: 1
inputs: [Gate, Repository, Component, Threshold, Jobs, Measured, Toolchains]
---
{{if eq .Gate "coverage"}}The coverage gate was switched on for {{.Component}} in {{.Repository}}, but no GitHub Actions step for this component measures line coverage for {{range $i, $t := .Toolchains}}{{if $i}}, {{end}}{{$t.Name}}{{end}}. Add the step for each language below, so every pull request reports the number.{{else}}The mutation gate was switched on for {{.Component}} in {{.Repository}}, but no GitHub Actions step for this component runs mutation testing for {{range $i, $t := .Toolchains}}{{if $i}}, {{end}}{{$t.Name}}{{end}}. Add the step for each language below, so every pull request reports a mutation score.{{end}}

{{if .Jobs}}CI jobs mapped to this component today:
{{bullets .Jobs}}
{{else}}No GitHub Actions job is mapped to this component yet. Add a workflow under .github/workflows that runs its tests on pull requests and on pushes to the default branch, then add the steps below to it.
{{end}}{{if .Measured}}
Already measured in CI, leave as is: {{join ", " .Measured}}.
{{end}}
Use the test runner and build tool the component already uses; the notes below name the usual tool for each, not a required one. If CI already does this through a script the check did not recognise (an `npm test` script, a `make` target), say so in your hand-off and change nothing for that language.
{{range .Toolchains}}
---

{{partial (printf "gates/%s" .ID) .}}
{{end}}
---

## Done when

{{if eq .Gate "coverage"}}- Each language above has a CI step that measures line coverage, in the job that already runs that language's tests: a flag on the existing test run, not a second full run.
- The step uploads the report as a workflow artifact and writes the total percentage to the job summary (`$GITHUB_STEP_SUMMARY`).
- {{if .Threshold}}TaskTrooper's bar for this component is {{.Threshold}}%. It is advisory: do not make CI fail below it unless the repository already enforces a minimum.{{else}}Do not make CI fail on a minimum unless the repository already enforces one.{{end}}{{else}}- Each language above has a CI step that runs mutation testing and uploads the report as a workflow artifact, with the mutation score in the job summary (`$GITHUB_STEP_SUMMARY`).
- Mutation testing is slow: on pull requests, limit the run to the changed code where the tool supports it, and run the full suite on a schedule or on pushes to the default branch.
- {{if .Threshold}}TaskTrooper's bar for this component is a {{.Threshold}}% mutation score. It is advisory: do not make CI fail below it unless the repository already enforces a minimum.{{else}}Do not make CI fail on a minimum score unless the repository already enforces one.{{end}}{{end}}
- The tool's configuration lives in the repository (its config file or the build file), so a developer runs the same command locally.
- A language above with no tests in this component is skipped and named in your hand-off; writing its first tests is a separate task.
- The existing CI jobs still pass.
