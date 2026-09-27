---
key: tool.cancel_criterion
version: "1"
params:
    canceled: Defaults to true. Pass false to put a cancelled criterion back in scope.
    criterion_id: Criterion UUID (from list_acceptance_criteria)
    reason: Why this criterion is not being done. One or two sentences, concrete.
---
Drop ONE acceptance criterion from this task's scope, with the reason. Use it only for a criterion that is deliberately not being done — out of scope, superseded by another decision, impossible as written, moved to another task. It is NOT a way past a criterion you simply have not implemented: if the work is missing, do the work and call set_criterion_completed. The reason is stored on the criterion and posted as a task comment, so the decision is visible to the humans reading the card.
