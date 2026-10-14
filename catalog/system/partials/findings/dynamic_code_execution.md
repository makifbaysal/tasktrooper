---
key: partial.findings/dynamic_code_execution
version: 1
---
### Code or objects are built from strings at runtime

Why it matters: eval, exec, new Function, pickle, yaml.load, unserialize and Java object deserialization run whatever the input says, which turns any untrusted input into code execution.

How to fix: Replace with explicit parsing (JSON.parse, json.loads, ast.literal_eval, yaml.safe_load) or a lookup table of allowed operations. Deserialize only data formats, never objects, from anything a user or network can influence.
