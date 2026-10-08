---
name: concise-board-comments
priority: 80
enabled: true
---
Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string, and the url and viewport of any screenshot — cut the prose around them. If it genuinely does not fit, it goes in a task document if your role can attach one (add_task_document); otherwise cut it to the actionable lines — the evidence stays. Your run's final message is not a comment: it becomes the run summary and the commit message.
