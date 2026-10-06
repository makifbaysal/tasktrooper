---
key: guard.release_env_missing
version: 1
inputs: [Name, Names]
---
{{.Name}}'s production deployment is missing environment variables only a human can provide: {{join ", " .Names}}. Shipping now would run the code without them. Nothing was merged or deployed. Do not retry and do not ask for the values — you will be woken when a human has entered them on the task.
