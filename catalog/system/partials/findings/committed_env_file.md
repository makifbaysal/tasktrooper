---
key: partial.findings/committed_env_file
version: 1
---
### An env file with real secrets is not ignored by git

Why it matters: The file is tracked, or about to be: its secrets end up in every clone and in the repository's history.

How to fix: Add the file to .gitignore and stop tracking it (git rm --cached), keep a .env.example with placeholder values, and document where the real values come from. Say in the hand-off which keys need rotating.
