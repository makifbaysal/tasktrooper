---
key: tool.telemetry_get_service
version: "2"
params:
    env: 'Deployment environment to read; omit for all.'
    range: 'How far back to look. It must cover the time the problem started or an empty result looks like health.'
    service: 'A service name as telemetry reports it (it is in the incident). Never guess one.'
---
One service's health in a single call: its error rate, latency and throughput, its endpoints ranked with their ids, its recent deploys and its dependencies. Start here for any service, then go narrower: telemetry_explain_endpoint for why one endpoint got worse, then traces, logs and errors.
