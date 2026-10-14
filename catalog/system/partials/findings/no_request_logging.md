---
key: partial.findings/no_request_logging
version: 1
---
### The HTTP service does not log requests

Why it matters: Without access logs nobody can tell which requests failed, how slow they were, or what a user did before an error.

How to fix: Add the framework's request-logging middleware: method, route, status, duration and a request id on every request, sensitive headers redacted, health checks excluded.
