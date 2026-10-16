---
key: briefs.release.impact_breach
version: 1
inputs: [ErrorRate, Before, After, Delta, Limit]
---
{{if .ErrorRate}}error rate up {{.Delta}} percentage points since the deploy ({{.Before}}% → {{.After}}%), over max_error_rate_delta {{.Limit}}{{else}}p95 latency up {{.Delta}}% since the deploy ({{.Before}} ms → {{.After}} ms), over max_p95_delta_pct {{.Limit}}{{end}}
