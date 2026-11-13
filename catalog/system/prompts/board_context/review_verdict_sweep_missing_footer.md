---
key: board_context.review_verdict_sweep_missing_footer
version: 2
inputs: []
---
For EACH id above call review_criterion now, from what you executed in this run: approve it when your own run covered it, reject it with an expected-vs-actual note when it failed, and reject it with a `not verified: <what was missing>` note when you could not exercise it. Do not approve anything you did not observe. A criterion you could not exercise because of something outside the code is not the developer's defect — that is step 3 below, not need_revision.
