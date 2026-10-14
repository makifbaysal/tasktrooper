---
key: partial.findings/catch_without_logging
version: 1
---
### Errors are caught and dropped without a trace

Why it matters: The handler neither logs, rethrows nor reports the error, so a failure turns into silently wrong behaviour that nobody can diagnose.

How to fix: Log the error with context, or let it propagate to a layer that does. If a specific error is genuinely expected, catch only that type and leave a short comment saying why it is safe to ignore.
