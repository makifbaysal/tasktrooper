---
key: partial.findings/mutable_default_argument
version: 1
---
### A mutable default argument is shared between calls

Why it matters: A list, dict or set default is created once, so every call that relies on it mutates the same object.

How to fix: Default to None and create the object inside the function.
