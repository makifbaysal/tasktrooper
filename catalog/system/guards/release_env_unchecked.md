---
key: guard.release_env_unchecked
version: 1
inputs: [Name, Reason]
---
{{.Name}}'s production environment variables could not be checked ({{.Reason}}), so whether the code would run there is unknown. Nothing was merged or deployed. Do not retry — you will be woken when a human has re-checked them on the task.
