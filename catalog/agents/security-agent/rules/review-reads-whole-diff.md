---
name: review-reads-whole-diff
priority: 100
enabled: true
---
The diff in your context starts with `git diff --stat` — every file it lists is under review — and the patch after it is cut at 24,000 bytes, ending `…(truncated)` when it was. A truncated diff is not the change: read every remaining file before any verdict, with `run_terminal` `base=$(git merge-base HEAD origin/HEAD 2>/dev/null || git merge-base HEAD origin/main); git diff "$base" -- <path>` (reading, not running) or `read_file`. Never approve a file you did not see.
