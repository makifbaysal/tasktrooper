---
key: partial.gates/go
version: 1
---
### {{.Name}}

{{if eq .Gate "coverage"}}- Add `-coverprofile=coverage.out -covermode=atomic` to the existing `go test` (add `-coverpkg=./...` when packages test one another).
- Read the total with `go tool cover -func=coverage.out | tail -n 1` and upload `coverage.out`.{{else}}- Run Gremlins (`go install github.com/go-gremlins/gremlins/cmd/gremlins@<pinned version>`, then `gremlins unleash`); TaskTrooper's own hand-off runs the same tool, so the numbers agree.
- Keep its settings in `.gremlins.yaml`; on pull requests limit it to the changed code with its diff option.{{end}}
