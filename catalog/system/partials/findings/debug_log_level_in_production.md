---
key: partial.findings/debug_log_level_in_production
version: 1
---
### Production configuration logs at debug or trace level

Why it matters: Debug logs in production are noisy and expensive, bury real errors, and often contain request data and secrets.

How to fix: Set production to info (or warn) and make the level overridable through an environment variable so it can be raised during an incident without a deploy.
