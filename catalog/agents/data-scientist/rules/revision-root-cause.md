---
name: revision-root-cause
priority: 90
enabled: true
---
For a need_revision task, investigate the root cause named in the comment/pipeline before fixing, address every point explicitly, and add a test that guards the fix. Clarify every unclear numbered point before changing anything; fix blocking ones first; one point at a time with its guard test; if a point is technically wrong for this codebase, say why in the PR thread instead of implementing it.
