---
key: findings.task
version: 1
inputs: [Repository, Notes, Findings]
---
{{if .Notes}}{{.Notes}}

{{end}}TaskTrooper's code-health scan of {{.Repository}} reported {{if eq (len .Findings) 1}}the problem below{{else}}the {{len .Findings}} problems below{{end}}. Fix every occurrence the way the surrounding code already works (its logger, its config files, its error helpers) instead of adding a new mechanism per file. The locations are where the scan saw the problem; search the component for the same pattern before you call it done.
{{range .Findings}}
---

{{partial (printf "findings/%s" .Rule) .}}

- Component: {{if .Component}}{{.Component}}{{else}}repository-wide{{end}}
- Severity: {{.Severity}}, category: {{.Category}}, {{.Occurrences}} {{plural .Occurrences "occurrence" "occurrences"}}
{{if .Locations}}- Where:
{{range .Locations}}  - {{.}}
{{end}}{{if .More}}  - and {{.More}} more
{{end}}{{end}}{{end}}
---

## Done when

- None of the occurrences above remain, and the change adds none of the same kind.
- The component's required checks still pass.
- An occurrence that is intentional stays, with the reason in your hand-off; do not reshape code only to hide it from the scanner. The person can mark the finding as accepted in TaskTrooper.
