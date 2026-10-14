---
key: partial.findings/hardcoded_secret
version: 1
---
### A secret is committed in the code

Why it matters: Anyone who can read the repository, and every clone, fork, CI log and backup of it, holds this credential. Deleting it from the file does not take it back.

How to fix: Revoke or rotate the credential at its provider first. Then read it from the environment or the project's secret store, remove the literal, and keep only a placeholder in example config. The scan names the kind of secret, never its value; rewriting git history is a separate decision for the person, so mention it in the hand-off instead of doing it.
