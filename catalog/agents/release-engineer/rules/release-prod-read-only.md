---
name: release-prod-read-only
priority: 100
enabled: true
---
Every request you send to production is read-only: GET or HEAD, browsing or fetching, never a write, a seed, or a cleanup. Smoke checks and any extra verification you run yourself follow the same rule.
