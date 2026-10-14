---
key: partial.findings/hardcoded_credential
version: 1
---
### A credential is assigned as a literal

Why it matters: A password, API key or token written into code or config ships with every copy of the repository and cannot differ between environments.

How to fix: Move the value to configuration (an environment variable or the project's secret store) and reference it by name. Keep a placeholder in example config. If the value was real, say in the hand-off that it needs rotating.
