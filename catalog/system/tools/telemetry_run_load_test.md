---
key: tool.telemetry_run_load_test
version: "1"
params:
    scenario_id: 'Run this existing scenario (from telemetry_list_load_tests). A draft must be reviewed by the user first.'
    scenario: 'Or create a new scenario and run it.'
    scenario.properties.name: 'For example "checkout baseline (agent)".'
    scenario.properties.spec: 'The scenario spec. Minimal: {target: "https://staging.example.com", mode: "vus", levels: [{name: "warm", ramp: 30, hold: 60, target: 5}, {name: "steady", ramp: 30, hold: 300, target: 20}], steps: ["GET /v1/products", "POST /v1/orders"], weights: [80, 20], thinkTime: 1, thresholds: ["http_req_failed rate < 1%", "http_req_duration p(95) < 500 ms"]}. Ramps and holds are seconds. Without a gate it aborts on its own at a 50% error rate or a 10 s p95.'
    environment: 'staging (the default) or production. A target that is this repository''s production is refused unless this says production, which you pass only when the user explicitly asked to load production.'
    user_approved: 'true only after the user said yes to running this load now, against this target, at this load and for this long.'
---
Run a load test: it sends real traffic to the target. The target must be a domain the user verified in telemetry (Load tests, Domains), and staging is the default. Without user_approved nothing runs: the answer shows the target, the load, the duration and exactly what would be sent. Tell the user those, ask, and call again with `user_approved: true` once they said yes. Then poll telemetry_list_load_tests with the run id every 15 to 30 seconds until it is passed, failed, aborted or error; telemetry_stop_load_test stops it at once.
