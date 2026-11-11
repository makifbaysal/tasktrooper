---
key: tool.telemetry_list_deploys
version: "1"
params:
    env: 'Deployment environment to read; omit for all.'
    range: 'How far back to look; widen it beyond the incident start.'
    service: 'A service name as telemetry reports it.'
---
List the deploys telemetry saw (a version first seen in spans, or a marker written by CI or the host), newest first. A deploy shortly before an incident started is a correlation worth checking, not proof; say so when you cite it.
