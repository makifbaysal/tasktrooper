---
key: tool.telemetry_list_load_tests
version: "1"
params:
    run_id: 'Read this one run: state, live VUs, RPS, p95 and error rate while it runs, and the per-step summary and thresholds when it ends. Give it alone.'
    scenario_id: 'Only the runs of this scenario.'
---
Load tests: the recent runs with their state and result, the scenarios (target, steps, peak load, duration, state draft or ready) and the target domains with whether each is verified. A run only reaches a verified domain, so check here before proposing one, and poll one run by run_id while it is going. Read-only.
