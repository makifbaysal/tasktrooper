---
key: notices.release_env_created_comment
version: 2
inputs: [Name, Names, OverwrittenOnDeploy]
---
Set on {{.Name}} before shipping: {{join ", " .Names}}. Values the code needs but nobody has to know were generated; non-secret values came from their declaration. None of them is stored or shown anywhere.{{if .OverwrittenOnDeploy}}

{{.Name}}'s configuration is usually rendered from a file in this repository on every deploy (for ECS, the task definition), and a deploy from that file drops variables it does not list. Add these names to it — secrets as references to the secret store entries TaskTrooper created, never as values — in a follow-up change, or the next deploy removes them.{{end}}
