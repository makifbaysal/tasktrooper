---
key: partial.gates/dotnet
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Make sure each test project references `coverlet.collector`, then run `dotnet test --collect:"XPlat Code Coverage"`; turn the Cobertura files into one summary with ReportGenerator. TaskTrooper's own hand-off runs the same collector.
- On Microsoft.Testing.Platform, use its `--coverage --coverage-output-format cobertura` instead.
- Unity: use the Code Coverage package with `-enableCodeCoverage` on the batch-mode test run.{{else}}- Run Stryker.NET (`dotnet tool install dotnet-stryker` as a local tool, then `dotnet stryker`), configured in `stryker-config.json` at the repository root; TaskTrooper's own hand-off runs Stryker.NET when that file exists.
- On pull requests use `--since` with the base branch so only changed code is mutated.{{end}}
