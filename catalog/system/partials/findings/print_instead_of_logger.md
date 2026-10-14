---
key: partial.findings/print_instead_of_logger
version: 1
---
### Service code prints to stdout instead of logging

Why it matters: Prints have no level, no timestamp, no structure and no request context, so they cannot be filtered, searched or turned off.

How to fix: Use the component's logger with an appropriate level and structured fields. CLI entry points and scripts may keep printing; services should not.
