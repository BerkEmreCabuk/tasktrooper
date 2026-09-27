---
key: board_context.review_verdict_ask
version: 1
inputs: [Exit]
---
Answer with ONE word and nothing else — no explanation, no tool call.
Based on the review you just completed: `APPROVE` if everything you required is satisfied and the work should move on to `{{.Exit}}`, `REVISE` if anything you flagged still needs work.
This answer is recorded as your verdict and the board move is made from it, so it must match the review you wrote above.
