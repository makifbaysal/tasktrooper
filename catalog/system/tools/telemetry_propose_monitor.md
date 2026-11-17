---
key: tool.telemetry_propose_monitor
version: "1"
params:
    monitor: 'The monitor as telemetry takes it.'
    monitor.properties.name: 'A short human name, for example "orders-api p95 latency".'
    monitor.properties.kind: 'What it watches. latency, error_rate, throughput and apdex need target.service (and target.endpoint for one endpoint); the others are rarer, copy their query shape from an existing monitor.'
    monitor.properties.target: 'service from telemetry, endpoint as "METHOD /route" (omit for the whole service), process for job_missed.'
    monitor.properties.query: 'Kind specific: latency {percentile: p50|p95|p99}; error_rate {statusMin: 500}; throughput {direction: drop|spike|change}; apdex {tMs: 500}; anomaly {metric: error_rate|p95|rpm, direction: up|down|both}; log_count {minSeverity: error, contains?}.'
    monitor.properties.condition: 'When it fires. Rates are fractions (2% = 0.02), latency is in ms.'
    monitor.properties.condition.properties.forMin: 'Minutes the condition must hold before it fires; 5 is a good default.'
    monitor.properties.condition.properties.minRequests: 'Skip windows with fewer requests, so two requests cannot page anyone.'
    monitor.properties.condition.properties.window: 'The window the value is measured over (default 5m, anomaly 15m).'
    monitor.properties.severity: 'critical pages someone; warning and info do not.'
    monitor.properties.channels: 'Notification targets copied from an existing monitor; an empty list opens incidents without notifying.'
    monitor.properties.status: 'active (the default) or draft, saved but not evaluated.'
    preview_range: 'How much history the preview replays the monitor over; 24h by default, 7d for a rare signal.'
    replace_id: 'Replace this monitor instead of creating one. Only a monitor an agent created (source agent) can be replaced.'
    user_approved: 'true only after the user said yes to this exact monitor in this conversation.'
---
Propose a monitor (an alert rule) in the connected telemetry. Without user_approved nothing is written: the answer replays the monitor over past data (`preview`: how often it would have fired and whether it fires now) and shows exactly what would be sent. Check telemetry_list_monitors first so you do not duplicate one; a monitor that would have fired 0 times on a noisy metric, or many times a day, has the wrong threshold, so fix it and preview again. Then show the user the monitor and its preview, ask, and call again with `user_approved: true` once they said yes. It is created with source agent.
