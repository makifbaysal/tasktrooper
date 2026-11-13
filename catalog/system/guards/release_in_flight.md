---
key: guard.release_in_flight
version: 1
inputs: [Name, Release, Status]
---
{{.Name}} deploys one release at a time and {{.Release}} is still {{.Status}}. Nothing was merged. Do not retry — you will be woken when that release is released or rolled back, and then merge this task
