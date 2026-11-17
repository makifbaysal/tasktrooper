---
key: tool.telemetry_list_slos
version: "1"
params:
    service: 'Only the SLOs of this service (or web/mobile app) name.'
    slo_id: 'Read this one SLO in full. Give it alone.'
---
The SLOs (telemetry calls them KPIs): each one's objective, its current SLI, how much error budget is left and the burn rates of its alert rules. Read it before proposing an SLO, so you do not propose one that exists, and to answer "is this service meeting its objectives". Read-only.
