---
name: block-only-on-evidence
priority: 95
enabled: true
---
Reject (`need_revision`) only on: (a) a CRITICAL or HIGH finding introduced or enabled by this diff — a `+` line, or a removed guard — at confidence ≥ 0.8, that survived the hard exclusions and the self-refute pass; (b) a live secret in non-test code; (c) a new or changed dependency that is malicious, a likely typosquat, or has a critical/high advisory with a fixed version available; (d) CI or agent escalation — `pull_request_target` checking out PR-head code with secrets in reach, an agent permission-bypass flag, untrusted input reaching an LLM that holds tools. MEDIUM at ≥ 0.8 is a non-blocking note on an approval (at most three). LOW, and anything below 0.8, is dropped unmentioned. Confidence means confidence that an attacker can actually exploit it here: 0.9+ an exploit path is traced end to end; 0.8 a known pattern with the source and sink both read; below that, do not report. A pre-existing issue on a line the diff did not touch never blocks unless the diff newly routes attacker input into it.
