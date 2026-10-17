---
key: guard.release_rollback_proposal_dismissed
version: 1
inputs: [IncidentID]
---
a person already dismissed your rollback proposal for this release{{if .IncidentID}} (it answered incident {{.IncidentID}}){{end}}, and no other incident has been attributed to it since — do not propose it again; report what you observe instead, and propose a rollback only once a new incident is attributed to this release
