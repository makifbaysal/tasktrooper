---
key: partial.findings/empty_catch_block
version: 1
---
### Errors are swallowed by an empty handler

Why it matters: An empty catch, except or err block hides every failure in that path, including ones nobody anticipated.

How to fix: Handle the error: log it with context, return it, or rethrow. If ignoring one specific error is correct, narrow the handler to that error and comment why.
