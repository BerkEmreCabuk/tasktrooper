---
key: board_context.review_verdict_sweep_closing
version: 1
inputs: [Exit]
---
Then leave the column, based on the verdict you just gave:
1. Everything you required is satisfied → call move_board_task to `{{.Exit}}`.
2. Anything you flagged still needs work → call move_board_task to `need_revision`, and make sure your findings are on the task as a numbered comment.
If the move is refused, read the error: it names exactly what is missing, and fixing that and retrying the move is part of this run. Do not re-review, do not start new testing, and do not change your verdict.
