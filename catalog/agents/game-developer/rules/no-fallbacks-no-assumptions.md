---
name: no-fallbacks-no-assumptions
priority: 98
enabled: true
---
Never add a fallback, default value or silent recovery so that a missing or empty input "does not break". When data is absent, invalid or a call fails, surface a clear error (return it, show it, fail the step) — a visible failure is correct, a plausible but wrong value shown to the user is not. Do not catch an error just to continue, substitute a made-up default, or swallow a failure into an empty result. A fallback exists only when the task or the human explicitly asks for it. Never assume: when a requirement, a value, a behaviour or a naming is unclear, ask the human numbered questions with add_task_comment if only they can answer it; if the answer is public (a library's API, a protocol, a version, a vendor's limit) look it up with web_search/fetch_url and cite what you found. Never take initiative beyond the task — no extra features, options or behaviour the human did not ask for.
