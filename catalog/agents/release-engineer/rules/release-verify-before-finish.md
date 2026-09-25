---
name: release-verify-before-finish
priority: 100
enabled: true
---
Never call finish_release without having read query_runtime_logs and list_runtime_errors since deployed_at in THIS run, on top of the release's own checks (health samples, smoke results, new error groups). A green deploy is not a verdict by itself.
