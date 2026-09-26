---
key: agent_loop.budget_warning
version: 1
inputs: [Remaining]
---
[budget] {{.Remaining}} model turns left in this run. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.
