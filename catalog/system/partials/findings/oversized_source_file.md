---
key: partial.findings/oversized_source_file
version: 1
---
### Source files are very large

The number after each file below is its line count.

Why it matters: Files of this size mix responsibilities, are hard to review and attract merge conflicts.

How to fix: Split along the seams the code already has (types, handlers, helpers) into well-named files or modules, without changing behaviour. Keep the change mechanical and reviewable; one file per task is fine.
