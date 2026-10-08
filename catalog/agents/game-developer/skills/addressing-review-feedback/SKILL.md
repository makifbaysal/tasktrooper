---
name: addressing-review-feedback
category: workflow
description: How to work a task returned with review, QA or UAT findings. Use when a task is in need_revision or PR review comments are in your context.
source: obra/superpowers (MIT), adapted
---
# Addressing Review Feedback

## Overview

A `need_revision` task is somebody else's evidence that something is wrong, not a request to improvise. Treat it the way you would a bug report: verify, fix at the root, prove it with a test, and answer every point — silence on one is how a task bounces a second time for something you could have agreed or disagreed with up front.

## The Process

1. **Read everything before touching code.** Every numbered point on the task, plus the PR review comments when the round happened there (`list_task_comments`, `get_task_pull_request` with `include_diff: false` when you don't need the diff itself). If a point is unclear, re-read the code it's about before acting on a guess. Points can be related — a root cause can explain two of them at once.
2. **Verify each claim** against the code and, where you can, the running product — a reviewer can be wrong about where a line lives or what a function does. Classify each point: **fix** (the claim holds), **disagree** (with evidence — a file:line or a run that shows the current behavior is correct), or **needs a human decision** (a product call neither of you can settle from the code).
3. **Fix in order: blocking → simple → complex.** Blocking = crash, security hole, or an acceptance criterion left unmet. Each fix gets its own guard test that fails before the fix and passes after (root-cause-debugging) — a fix with no failing-first test is not verified, it's hoped.
4. **Push back technically, never performatively.** A point you disagree with gets a reasoned answer with evidence, not silent compliance and not silent skipping. Reply in the PR thread (`comment_on_pull_request` with `reply_to_comment_id`) when it came from a review comment — a new top-level comment leaves the reviewer's thread looking unanswered; otherwise `add_task_comment` for a task-comment point. No "You're absolutely right," no thanks, no preamble — state the finding.
5. **A YAGNI check** when a reviewer asks you to "implement it properly" or "handle the general case": `grep_code` for actual callers first. Build the general version only if more than one caller needs it; otherwise say so and keep the fix scoped to what broke.
6. **Closing message: a point map.** One line per numbered point — point → root cause → fix (file:line) → guard test, or point → why you disagree, with evidence. The architect's review of your revision reads this map against the diff; a revision with no map is a second bounce waiting to happen.

## Red Flags

- A partial fix — three of four points addressed and the fourth left unmentioned.
- A fix with no guard test that failed first.
- Weakening or deleting the test that caught the problem instead of fixing the code.
- Silently agreeing with a wrong claim because arguing felt slower — a wrong "fix" based on a wrong claim bounces again, further down the line.
- A closing message that says "addressed all feedback" without the per-point map.
