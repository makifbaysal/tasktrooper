---
key: tool.telemetry_stop_load_test
version: "1"
params:
    run_id: 'The run to stop, from telemetry_run_load_test or telemetry_list_load_tests.'
---
Stop a queued or running load test at once. It needs no approval because it only takes load away: stop a run as soon as the target degrades beyond what the user agreed to, or when the user asks.
