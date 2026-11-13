---
key: notices.release_in_flight_comment
version: 1
inputs: [Name, Release, Status]
---
Waiting to merge: {{.Name}} ships one release at a time, and {{.Release}} is still {{.Status}}. This task merges and deploys as soon as that release is released or rolled back, so if something breaks it is clear which change did it.
