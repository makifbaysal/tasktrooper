---
key: partial.gates/scala
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Add sbt-scoverage and run `sbt clean coverage test coverageReport`; upload the XML report.{{else}}- Add stryker4s (the sbt plugin) and run `sbt stryker`, configured in `stryker4s.conf`; upload its report.{{end}}
