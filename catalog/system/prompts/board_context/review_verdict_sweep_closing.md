---
key: board_context.review_verdict_sweep_closing
version: 2
inputs: [Exit]
---
Then leave the column, based on the verdict you just gave:
1. Everything you required is satisfied → call move_board_task to `{{.Exit}}`.
2. Something in the code still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.
3. You could not verify something because of what is outside the code (your environment, a missing integration, credential, device or stage) and nothing in the code is wrong → do NOT move to `need_revision`: the developer cannot fix it and the task would come straight back to you unchanged. Call ask_user once, in choice mode, naming exactly what is missing and what would unblock it (for example "I provided it, retry", "accept code review + unit tests for this criterion", "move to need_revision").
If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. Do not re-review, do not start new testing, and do not change your verdict.
