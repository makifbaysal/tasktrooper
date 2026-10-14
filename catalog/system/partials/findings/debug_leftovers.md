---
key: partial.findings/debug_leftovers
version: 1
---
### Debugging statements were left in the code

Why it matters: debugger statements, breakpoints, pdb/byebug calls and var_dump/dd output stop or leak in production.

How to fix: Remove them; use the logger for anything worth keeping.
