---
key: tool_results.board_deploy_impact_next
version: 1
inputs: [Verdict, Pending, AutoRollback]
---
{{if eq .Verdict "worse"}}A golden signal got worse after this deploy{{if .Pending}} (the hour after it is not over yet){{end}}. If the incident or the soak evidence ties it to this release, call rollback_release (reason health_incident or verify_failed) with the signal, its before and after values and the baseline in the note — {{if .AutoRollback}}auto_rollback is on, so it executes{{else}}auto_rollback is off, so it only writes the proposal and a human is asked to approve it; that is the correct outcome, do not look for another way{{end}}.{{else if eq .Verdict "unknown"}}Nothing could be judged: no golden signal is bound, or none was readable. Decide on the logs, errors, health and smoke evidence alone, and say in your note that deploy impact was unknown.{{else}}No golden signal got worse after this deploy ({{.Verdict}}){{if .Pending}} so far — the hour after it is not over yet{{end}}. That alone is not a reason to roll back; weigh it with the logs, errors, health and smoke evidence.{{end}}
