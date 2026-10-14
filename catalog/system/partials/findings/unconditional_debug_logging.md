---
key: partial.findings/unconditional_debug_logging
version: 1
---
### The logger is hard-coded to debug level

Why it matters: A level set in code ignores the environment, so every deployment logs at debug.

How to fix: Read the level from configuration or an environment variable, defaulting to info; keep debug for local development.
