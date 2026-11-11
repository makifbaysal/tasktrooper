---
key: tool.telemetry_get_incident
version: "1"
params:
    incident_id: 'The telemetry incident id (the "Telemetry incident id" in the task, or an id from telemetry_list_incidents). Never invent one.'
---
Read one TaskTrooper Telemetry incident with its timeline: when it started, the monitor or SLO that fired, the value and peak, the service and endpoint, the acknowledgement, and the telemetry's own root-cause hint (`rootCause`). That hint is unstructured and only a lead to verify with the other telemetry tools, never a conclusion. This is the monitoring product's incident, not a TaskTrooper incident (those are `get_incident`).
