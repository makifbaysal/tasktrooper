---
key: tool.telemetry_propose_slo
version: "1"
params:
    slo: 'The SLO (a telemetry KPI) as telemetry takes it.'
    slo.properties.name: 'The user-facing outcome, for example "Checkout succeeds".'
    slo.properties.description: 'One sentence: what a good event is and why it matters.'
    slo.properties.owner: 'The team or person who owns it, free text.'
    slo.properties.target: 'kind endpoint, page, journey or app_start; serviceId the service or app name; ref "METHOD /route" for an endpoint, the path for a page.'
    slo.properties.sli: 'latency (fast and not a 5xx), availability, or success_rate (not a 5xx).'
    slo.properties.good: 'The good-event rule; thresholdMs is required for a latency SLO.'
    slo.properties.objective: 'The share of good events, between 0 and 1 exclusive, for example 0.99.'
    slo.properties.window: '7d or 28d.'
    slo.properties.alerts: 'Usually two burn-rate rules: fast (longWindow 1h, shortWindow 5m, threshold 14.4, critical) and slow (6h / 30m, 6, warning).'
    replace_id: 'Replace this SLO instead of creating one. Only an SLO an agent created (source agent) can be replaced.'
    user_approved: 'true only after the user said yes to this exact SLO in this conversation.'
---
Propose an SLO with burn-rate alerts in the connected telemetry. Without user_approved nothing is written and the answer shows exactly what would be sent. Read telemetry_list_slos first, pick an objective the service meets today (telemetry_get_service), show the user the SLO, ask, and call again with `user_approved: true` once they said yes. It is created with source agent.
