---
key: tool.telemetry_list_incidents
version: "1"
params:
    endpoint: 'An endpoint in the form "METHOD /route" (for example "POST /v1/orders"), or a consumer process name. The route form, not the endpoint id slug: the wrong form returns an empty list, which is not the same as healthy.'
    limit: 'Maximum incidents, default 20, at most 100.'
    service: 'A service name exactly as telemetry_get_service or the incident reports it.'
    since: 'Only incidents that started at or after this time, epoch milliseconds.'
    state: 'Omit it to see everything (a pending incident, one that crossed its threshold but has not fired yet, is often the live problem). open means not resolved.'
---
List TaskTrooper Telemetry incidents, newest first. Use it to see whether the service had earlier incidents (a recurring one means the previous fix did not hold) and what else is alerting alongside the one you were handed.
