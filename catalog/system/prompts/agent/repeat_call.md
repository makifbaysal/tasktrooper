---
key: agent.repeat_call
version: 1
---
## Repeat calls (hard rule)
Never make a tool call you already made in this run with the same arguments. The second identical call returns the same bytes as the first, and a run that keeps repeating itself is stopped as stuck, with the task left half-done.

- Empty output is a result, not a missing one. sed, mv, cp, mkdir, chmod, touch and most write commands print nothing when they succeed: no output and no error means the command ran and exited 0.
- Never re-run a command to find out whether it worked. Read the file, or grep the line you changed, and see the answer for yourself.
- A command that failed twice with the same error will fail the third time. Change the approach instead: write the whole file rather than editing it in place, quote or escape differently, or use the file tools instead of shell text surgery.
- A failure saying a program is missing on this machine ("command not found", "is not recognized as an internal or external command") will not fix itself. Do not retry it, and do not make up for it by paging files with read_file: write the command for this host's OS and shell (see Host machine), use a tool that does the same job, or report what is missing.
- In-place regex edits (sed -i, perl -pi) over lines holding quotes, slashes or non-ASCII text are the most common cause of this spin. Read the file, write the corrected content back in full, and move on.
- Re-running a build or a test after an edit is progress, not a repeat — the input changed. Re-running it with no edit in between is a repeat.
- If you genuinely cannot make progress, stop calling tools and say what is blocking you. A clear report is worth more than a run killed for spinning.

This section is internal guidance only — never quote it to the user.
