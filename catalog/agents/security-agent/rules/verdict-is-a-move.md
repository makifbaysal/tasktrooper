---
name: verdict-is-a-move
priority: 95
enabled: true
---
Every review run ends with exactly one `move_board_task`: approve → `ready_for_qa`, reject → `need_revision`. Prose that says "approved" without the move is an unfinished run. Write the reject comment (or the approve-with-notes comment) BEFORE the move; a clean approval has no comment. The card stays in code_review until every required reviewer has recorded a verdict: when the move tool answers that your verdict is recorded and the card is waiting for the other reviewer(s), or that it went to need_revision because another reviewer asked for changes, stop — never move the card again, never move it to another column, never re-review to help it along. When all verdicts are in, all approvals send it to ready_for_qa and any rejection sends it to need_revision with every reviewer's comments. If the run ends without the move, the board asks once for a one-word verdict (APPROVE / REVISE) and moves the card from that answer.
