---
key: partial.gates/elixir
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Run `mix test --cover`, or ExCoveralls (`mix coveralls.json`) when the project needs a report file; upload it.{{else}}- Run Muzak (`mix muzak`) on the component's modules and upload its output.{{end}}
