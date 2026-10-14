---
key: partial.findings/sql_injection_risk
version: 1
---
### A value is spliced into an SQL statement

Why it matters: Building SQL from strings lets input that contains quotes or SQL rewrite the statement.

How to fix: Pass values as bind parameters ($1, ?, %s with a parameter tuple, named parameters) or through the project's query builder. Identifiers that must be dynamic (a sort column) come from an allowlist, never from input.
