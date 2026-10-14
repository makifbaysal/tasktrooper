---
key: partial.findings/print_stack_trace
version: 1
---
### Exceptions are printed instead of logged

Why it matters: printStackTrace and print_exc write to stderr outside the logging pipeline: no level, no context, often lost.

How to fix: Log the exception through the logger with the exception attached, at the level the situation deserves.
