---
key: board_context.automation_failed_comment
version: 1
inputs: [Rounds, Report]
---
The manual QA round passed, but the automation suite (`qa-automation/scripts/e2e.sh`) is still red after {{.Rounds}} fix rounds, so the task cannot leave QA. Last output:

```
{{.Report}}
```

If the failure is in the product, fix it on this branch; if it is in a test, the next QA round fixes the test.
