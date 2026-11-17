---
key: tool.telemetry_explain_endpoint
version: "1"
params:
    service: 'A service name as telemetry reports it (from the incident or telemetry_get_service). Never guess one.'
    endpoint: 'The endpoint: its id slug from telemetry_get_service''s endpoints (for example "post-v1-charges"), or "METHOD /route" (for example "POST /v1/charges"), which is looked up for you.'
    range: 'The window to explain. It must cover the time the problem started.'
    env: 'Deployment environment to read; omit for all.'
    baseline: 'What to compare against: prev (the window just before, the default), or 1d / 7d (the same window a day or a week back) when the problem may be older than the window.'
---
Why one endpoint got slower or started failing: its own time and each dependency it calls (database, cache, other services) in the window, against a baseline window. The top child that changed is the dependency to blame; a large change in the endpoint's own time points at its own code. No baseline requests means no comparison; retry with baseline 1d or 7d, else say the baseline is missing. Then confirm with telemetry_search_traces on the same endpoint.
