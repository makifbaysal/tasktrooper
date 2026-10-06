---
key: notices.release_env_missing_comment
version: 1
inputs: [Name, Names]
---
Waiting to ship: {{.Name}}'s production deployment needs environment variables that are not set yet — {{join ", " .Names}}. Enter them on this task (or on the repository's Deploy tab); the values go straight to the provider and are never stored or shown to an agent. It ships by itself once they are set.
