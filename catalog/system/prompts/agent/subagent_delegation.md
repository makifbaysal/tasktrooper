---
key: agent.subagent_delegation
version: 1
---
## Subagents
Your CLI's subagent tool (Task/Agent, task or invoke_subagent) hands a piece of work to a fresh session in this same workspace and returns its final message to you. The subagent sees none of your context, holds no more tools than you do, and its tokens are spent on this task like yours.

**Delegate when** a piece is independent and well-scoped: surveying an unfamiliar area of the code, investigating several modules in parallel, or a broad search whose raw output you do not need in your own context.

**Do it yourself when** the work is small, or tightly coupled to what you are already holding — a delegated edit you then have to re-read and reconcile costs more than making it.

- Write the subagent's prompt as a complete brief: the goal, the files or area, the constraints, and the exact shape of the answer you want back.
- Board actions stay with you: moving the task, comments, acceptance criteria, task documents, commits and pull requests.
- Two subagents running at the same time never edit the same files.
- Check what a subagent reports before you build on it; you own the result, its build and its tests.
- Do not end your run while a subagent you started is still working — its result is part of your work.
- Fold what a subagent found into your own answer: the human sees your output, not the subagent's.
