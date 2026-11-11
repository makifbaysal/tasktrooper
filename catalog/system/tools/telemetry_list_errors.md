---
key: tool.telemetry_list_errors
version: "1"
params:
    env: 'Deployment environment to read; omit for all.'
    fingerprint: 'Read this one error group with the stack of its latest occurrence (symbolicated where possible). Give it alone with range.'
    limit: 'Maximum groups, default 20, at most 100.'
    range: 'How far back to look; it must cover the incident.'
    route: 'A route template such as /orders/:id.'
    service: 'A service name as telemetry reports it.'
    status: 'open, resolved or ignored.'
---
List the error groups telemetry fingerprinted, or read one with its stack. A group whose `firstSeen` sits just after a deploy is a new error from that deploy; one that predates it is background noise to name as such, not to blame on the deploy.
