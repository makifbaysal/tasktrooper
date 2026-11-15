---
key: board_context.automation_no_entrypoint
version: 1
inputs: []
---
`qa-automation/scripts/e2e.sh` does not exist, so the suite cannot run. Create it as the project's single entrypoint: provision what the tests need (Testcontainers, WireMock), run every suite, tear down, and exit non-zero on any failure.
