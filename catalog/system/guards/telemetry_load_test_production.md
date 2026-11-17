---
key: guard.telemetry_load_test_production
version: 1
inputs: [Host]
---
{{.Host}} is where this repository's production answers, and load tests target staging unless the user explicitly asked for production. Target the staging environment instead; only if the user asked to load production, call again with `environment: "production"`, still after their yes to the exact load.
