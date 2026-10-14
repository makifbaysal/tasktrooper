---
key: partial.findings/typescript_any_overuse
version: 1
---
### TypeScript checks are switched off in many places

Why it matters: Every any, as any and @ts-ignore is a spot where the compiler no longer catches mistakes.

How to fix: Replace them with real types, or unknown plus narrowing; validate external data at the boundary (zod or similar). Prefer @ts-expect-error with a reason over @ts-ignore when an escape is truly needed.
