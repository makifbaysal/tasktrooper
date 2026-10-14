---
key: partial.findings/error_details_exposed
version: 1
---
### Internal error details reach the client

Why it matters: Stack traces, exception messages and developer error pages tell an attacker about the code, its paths and its dependencies.

How to fix: Return a generic message with a correlation id; log the full error server-side. Developer exception pages and stack-trace inclusion belong to development configuration only.
