---
key: partial.findings/no_automated_tests
version: 1
---
### The component has no automated tests

Why it matters: Without tests every change relies on manual checking, and agents cannot verify their own work before hand-off.

How to fix: Set up the stack's standard test runner, add tests around the most critical paths first, and add the test command to the component's checks.
