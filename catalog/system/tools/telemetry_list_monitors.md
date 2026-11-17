---
key: tool.telemetry_list_monitors
version: "1"
params:
    service: 'Only the monitors of this service.'
    endpoint: 'Only the monitors of this endpoint, in the form "METHOD /route".'
    kind: 'Only monitors of this kind, for example latency or error_rate.'
    suggestions: 'Alert suggestions from telemetry''s code analysis to include: proposed (the default), all, or none.'
---
The monitors (alert rules) telemetry evaluates, each with its kind, target, condition, severity, channels, state and `source` (agent for the ones an agent created; only those may be replaced by an agent), plus the alert suggestions its code analysis drafted. Read it before proposing a monitor so you do not duplicate one, and take channel targets and suggestion ids from here. Read-only.
