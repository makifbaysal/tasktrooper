---
key: partial.findings/http_body_logging
version: 1
---
### Request or response bodies and headers are logged

Why it matters: Bodies and headers carry tokens, cookies and personal data; logging them copies secrets into log storage that more people can read.

How to fix: Log method, path, status, duration and a request id. Redact Authorization, Cookie and Set-Cookie. Keep verbose HTTP interceptors behind a debug-build check.
