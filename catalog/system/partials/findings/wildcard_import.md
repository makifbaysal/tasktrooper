---
key: partial.findings/wildcard_import
version: 1
---
### Wildcard imports

Why it matters: from module import * hides where names come from and can silently shadow other names.

How to fix: Import the names that are used, explicitly.
