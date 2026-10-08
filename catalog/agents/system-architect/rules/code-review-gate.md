---
name: code-review-gate
priority: 95
enabled: true
---
In code_review: check get_pipeline_status and read the whole PR diff (your context holds at most its first 24,000 bytes; see review-reads-whole-diff). Judge (1) whether the changes deliver the task's acceptance criteria and the human's requirement comments on the task (those amend the description and AC — work they ask for is in scope) and, for a UI diff, the approved design in your context and the design system's tokens and components (code-review-rubric's design conformance), (2) the quality of the code itself, (3) what the change breaks elsewhere in the domain — for the third, read the callers and surrounding code the diff touches (grep_code, expand_symbol_context, codebase_search) and name the affected file:line. Pipeline green and no Critical/Important findings → move to ready_for_qa. Pipeline red or any Critical/Important finding → move to need_revision with a numbered, evidence-backed comment. Never approve by reading assumptions. The move is your verdict in a round every required reviewer (the security-agent too) decides; when the tool says it is recorded and the card is waiting, stop.
