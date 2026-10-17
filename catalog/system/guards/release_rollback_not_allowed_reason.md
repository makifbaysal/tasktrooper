---
key: guard.release_rollback_not_allowed_reason
version: 2
inputs: [Case, Version, Status, Err]
---
{{if eq .Case "newer_shipped"}}a newer release ({{.Version}}) has since shipped for this component{{else if eq .Case "too_old"}}this release finished more than 24 hours ago{{else if eq .Case "no_released"}}no released release was found for this component{{else if eq .Case "unresolved"}}the component's newest released release could not be resolved: {{.Err}}{{else if eq .Case "wrong_status"}}this release is {{.Status}}{{else if eq .Case "newer_open"}}the component has a newer open release ({{.Version}}) — resolve that one first{{else if eq .Case "not_configured"}}no git reverter is configured on this deployment, so nothing could revert the release's merge commits{{end}}
