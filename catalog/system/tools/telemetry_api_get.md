---
key: tool.telemetry_api_get
version: "1"
params:
    path: 'The path under the telemetry API root, with no leading /api/v1, for example "infra/hosts", "infra/databases/3/queries", "kpis", "monitors" or "apps/storefront/summary".'
    query: 'Query parameters as strings, for example {"range": "6h", "env": "production"}.'
---
Read any other query route of the TaskTrooper Telemetry API when the dedicated telemetry tools do not cover it: hosts, Kubernetes, databases, queues, SLOs (kpis), monitors, web and mobile apps, load tests, profiling. Read-only; it refuses anything that is not a data route. The result is JSON as telemetry returns it.
