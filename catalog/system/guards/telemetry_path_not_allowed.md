---
key: guard.telemetry_path_not_allowed
version: 1
---
That telemetry path is not readable from here. telemetry_api_get reads the API's query routes only (services, traces, errors, logs, incidents, monitors, kpis, deploys, infra, apps, loadtests, replays, profiles, ai); it never reaches keys, members, integrations or anything that changes data.
