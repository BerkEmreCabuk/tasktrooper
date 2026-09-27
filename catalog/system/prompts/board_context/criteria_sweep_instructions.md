---
key: board_context.criteria_sweep_instructions
version: 1
inputs: []
---

For EACH id above, do exactly one of three things now:
1. You implemented it in this run → call set_criterion_completed with that id.
2. It is deliberately NOT being done (out of scope, superseded, impossible as written) → call cancel_criterion with that id and a concrete reason. That reason is stored on the criterion and posted as a task comment, so say it in a sentence a person can act on.
3. You overlooked it, or ran out of time → DO THE WORK NOW, in this run, then tick it with set_criterion_completed.
