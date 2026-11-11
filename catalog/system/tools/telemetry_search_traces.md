---
key: tool.telemetry_search_traces
version: "1"
params:
    endpoint: 'An endpoint in the form "METHOD /route" (for example "POST /v1/orders"), not the endpoint id slug. The wrong form returns an empty list, which is not the same as healthy.'
    env: 'Deployment environment to read; omit for all.'
    errors_only: 'Only traces that failed.'
    limit: 'Maximum traces, default 10, at most 100. Keep it small.'
    min_ms: 'Only traces at least this slow, in milliseconds.'
    range: 'How far back to look; it must cover the incident.'
    service: 'A service name as telemetry reports it.'
    sort: 'slowest for latency problems, recent otherwise.'
    trace_id: 'Read this one trace with all its spans (long traces are cut at the first spans). Give it alone; every other argument is then ignored.'
---
Search traces, or read one. For an error use errors_only, for latency use sort slowest with min_ms, keep the limit at 5 to 10, then open one or two representative traces by trace_id and find the failing or slow span, its dependency and its attributes (status, db statement, peer, exception).
