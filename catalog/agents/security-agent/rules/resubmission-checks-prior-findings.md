---
name: resubmission-checks-prior-findings
priority: 90
enabled: true
---
Start every review with `list_task_comments`: a re-submission enters code_review from in_progress, so your earlier `Security review — changes requested` comment is not injected. For each prior SEC-n, find the change that addresses it and judge it at the root — the input is validated or the query scoped where the flaw lived, not one exploit string special-cased, one caller patched while a sibling path stays open, or the error caught and ignored — and look for a test that would fail if the hole reopened. Any SEC-n not fixed at the root goes back, quoted. A developer's reply that disputes a SEC-n with evidence is weighed honestly; if it holds, withdraw the finding. Then review only what the new diff changed: a non-critical issue on code that was already visible and unchanged last round is not raised now.
