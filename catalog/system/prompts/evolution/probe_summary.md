---
key: evolution.probe_summary
version: 1
inputs: [Cases, Unmeasured, Adopted, Wins, Losses, Ties, NotAdopted]
---
Probe: {{.Cases}} rehearsal {{plural .Cases "case" "cases"}}.{{if .Unmeasured}} Nothing could be measured, so no skill or rule change was written.{{else if .Adopted}} Adopted "{{.Adopted}}": {{.Wins}} better, {{.Losses}} worse, {{.Ties}} even against the current setup.{{else}} No variant did better than the current setup, so no skill or rule change was written.{{end}}{{if .NotAdopted}} {{.NotAdopted}} {{plural .NotAdopted "change was" "changes were"}} not adopted.{{end}}
