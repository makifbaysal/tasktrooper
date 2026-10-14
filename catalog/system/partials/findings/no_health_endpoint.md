---
key: partial.findings/no_health_endpoint
version: 1
---
### The HTTP service has no health endpoint

Why it matters: Load balancers, orchestrators and deploy checks cannot tell whether the service is alive or ready.

How to fix: Add a cheap liveness endpoint (for example /healthz) and a readiness endpoint that checks the dependencies the service cannot work without, and point deploy probes at them.
