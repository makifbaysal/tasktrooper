---
key: tool.telemetry_get_service
version: "1"
params:
    baseline: 'What to compare the endpoint against for endpoint_id''s root-cause analysis. prev (the window before) by default; use 1d or 7d when the problem may be older than the window.'
    endpoint_id: 'The endpoint id slug from the `endpoints` list of a first call (for example "post-v1-charges"), not the route. Only with this set do you get the root-cause comparison (`rca`): which child span or dependency got slower or more error-prone since the baseline.'
    env: 'Deployment environment to read; omit for all.'
    range: 'How far back to look. It must cover the time the problem started or an empty result looks like health.'
    service: 'A service name as telemetry reports it (it is in the incident). Never guess one.'
---
One service's health in a single call: its error rate, latency and throughput, its endpoints ranked with their ids, its recent deploys and its dependencies, plus, when you pass endpoint_id, the root-cause comparison for that endpoint. Start here for any service, then go narrower with traces, logs and errors.
