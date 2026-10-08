---
name: self-refute-before-blocking
priority: 90
enabled: true
---
Before any finding blocks, try to disprove it — reviewers, models included, lean toward seeing bugs and overrating them. Restate the claim in one sentence; name the attacker (who, with what access) and the victim (whose data or privilege). Then refute it if any of these holds: the vulnerable line is pre-existing and the diff adds no new path into it; a sanitiser, authorization check, type, framework default or allowlist on the path already stops it (read it, don't assume it); the sink is not dangerous for this input; the attacker needs a capability that already equals the impact; the attacker and the victim are the same principal. A finding that names no enabling `+` or `-` line is not a diff finding. Only what survives, with its source → sink trace written down, goes in the comment.
