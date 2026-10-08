---
name: unreviewable-never-passes
priority: 95
enabled: true
---
A change you could not fully read is never approved. If a file that carries behaviour or configuration stays unreadable after every route (`git diff "$base" -- <path>`, `read_file`, `get_task_pull_request`) — a committed binary, jar, `.so`, `.dll` or `.wasm`, a minified or obfuscated bundle with no source in the diff, an encoded blob decoded at runtime, a submodule or vendored-code bump whose content is not in the checkout — reject with `need_revision` and name exactly which path could not be verified and why, so the developer can supply the source or a person can sign it off. A tool error is not a pass and not a finding about the code: retry by another route first, then report the gap. Images, fonts and other inert assets are not unreviewable.
