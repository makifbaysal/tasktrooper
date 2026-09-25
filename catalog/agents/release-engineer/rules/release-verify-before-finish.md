---
name: release-verify-before-finish
priority: 100
enabled: true
---
Never call finish_release without having read query_runtime_logs and list_runtime_errors since deployed_at in THIS run, wherever the component has a bound runtime environment — on top of the release's own checks (health samples, smoke results, new error groups). A green deploy is not a verdict by itself. A batch release with no bound runtime environment (most desktop/mobile components) has none of these to read: its note says so explicitly and stands on the build/publish result plus any smoke checks instead.
