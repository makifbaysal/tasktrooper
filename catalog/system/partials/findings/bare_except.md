---
key: partial.findings/bare_except
version: 1
---
### A bare except catches everything

Why it matters: except: also catches KeyboardInterrupt and SystemExit and hides programming errors such as NameError.

How to fix: Catch the specific exceptions the block expects (or Exception at most) and log or re-raise the rest.
